package provenance

import (
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

// stashPullRepo returns an annotated repository on main with forge.txt and
// local.txt, and the tip of branch forge, which appends a line to forge.txt
// without a note, like a commit pulled from the forge.
func stashPullRepo(t *testing.T) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, "forge.txt", "one\ntwo\n")
	write(t, root, "local.txt", "local\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "-b", "forge")
	write(t, root, "forge.txt", "one\ntwo\nforge\n")
	tip := commit(t, root, "forge")
	git(t, root, "checkout", "-q", "main")
	return root, repo, base, tip
}

// agentEditsBeforePull edits a path the pull changes and one it does not.
func agentEditsBeforePull(t *testing.T, root string, repo *gitcmd.Repo) {
	t.Helper()
	write(t, root, "forge.txt", "agent\none\ntwo\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	write(t, root, "local.txt", "local\nagent\n")
	captureAI(t, repo, "stash-session", "local.txt")
}

// refTransaction runs the reference-transaction hook for one update of ref.
// An empty value stands for the zero object ID.
func refTransaction(t *testing.T, repo *gitcmd.Repo, ref, old, newValue string) RewriteResult {
	t.Helper()
	zero := strings.Repeat("0", 40)
	if old == "" {
		old = zero
	}
	if newValue == "" {
		newValue = zero
	}
	input := old + " " + newValue + " " + ref + "\n"
	result, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed")
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// autostashFastForward does to main what git pull --ff-only --autostash
// does before it applies the autostash, and runs the hooks Git runs on the
// way. git reset moves an existing MERGE_AUTOSTASH into the stash list, and
// git merge applies one it did not create itself, so the ref is hidden from
// both and set again for the fast-forward hooks.
func autostashFastForward(t *testing.T, root string, repo *gitcmd.Repo, base, tip string) (string, RewriteResult) {
	t.Helper()
	stash := strings.TrimSpace(git(t, root, "stash", "create", "autostash"))
	git(t, root, "update-ref", "MERGE_AUTOSTASH", stash)
	refTransaction(t, repo, "MERGE_AUTOSTASH", "", stash)
	git(t, root, "update-ref", "-d", "MERGE_AUTOSTASH")
	git(t, root, "reset", "-q", "--hard")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	git(t, root, "update-ref", "MERGE_AUTOSTASH", stash)
	return stash, runFastForwardHooks(t, repo, base, tip)
}

func readStashNote(t *testing.T, repo *gitcmd.Repo, stash string) model.Note {
	t.Helper()
	data, found, err := repo.ReadNoteRef(stashNotesRef, stash)
	if err != nil || !found {
		t.Fatalf("stash note on %s: found=%t err=%v", stash, found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func assertNoStashNotes(t *testing.T, repo *gitcmd.Repo) {
	t.Helper()
	for _, ref := range []string{stashNotesRef, stashOwnershipRef} {
		commits, err := repo.NoteCommits(ref)
		if err != nil || len(commits) != 0 {
			t.Fatalf("%s notes = %v, %v; want none", ref, commits, err)
		}
	}
}

// stashPopAfterFastForwardPull runs the issue-55 flow: agent edits, a
// stash, a fast-forward pull, and a pop. When tipNote is set it first
// writes a note for the pulled tip, as a synced note would deliver.
func stashPopAfterFastForwardPull(t *testing.T, tipNote bool, forge ...model.Author) {
	t.Helper()
	root, repo, base, tip := stashPullRepo(t)
	agentEditsBeforePull(t, root, repo)
	git(t, root, "stash", "-q")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	if result := refTransaction(t, repo, "refs/stash", "", stash); result.Written != 0 {
		t.Fatalf("stash push without pending ranges = %+v, want no note", result)
	}
	git(t, root, "merge", "-q", "--ff-only", "forge")
	transaction := runFastForwardHooks(t, repo, base, tip)
	if transaction.Written != 1 {
		t.Fatalf("fast-forward = %+v, want the consumed checkpoints saved in the stash note", transaction)
	}
	// Checkpoints on local.txt stay live, so only forge.txt is saved.
	if note := readStashNote(t, repo, stash); len(note.Files) != 1 || note.Files["forge.txt"].Blob == "" {
		t.Fatalf("stash note after fast-forward = %+v, want only forge.txt", note)
	}
	if tipNote {
		fetched := makeCoverageNoteForContent(mustBlob(t, repo, tip, "forge.txt"), "forge.txt", 3)
		if err := repo.WriteNote(tip, encodeCoverageNote(t, fetched)); err != nil {
			t.Fatal(err)
		}
	}

	git(t, root, "stash", "pop", "-q")
	if result := refTransaction(t, repo, "refs/stash", stash, ""); result.Mapped != 1 {
		t.Fatalf("stash pop = %+v, want forge.txt restored", result)
	}
	assertNoStashNotes(t, repo)
	assertNoParkedCheckpoints(t, repo, commitAndAnnotate(t, root, repo, tip, "stashed work"))
	assertBlameAuthors(t, repo, "forge.txt", forge...)
	assertBlameAuthors(t, repo, "local.txt", model.AuthorHuman, model.AuthorAI)
}

// The pulled line has no evidence until the forge note arrives.
func TestStashPopAfterFastForwardPullWithoutTipNote(t *testing.T) {
	t.Parallel()
	stashPopAfterFastForwardPull(t, false,
		model.AuthorAI, model.AuthorHuman, model.AuthorHuman, model.AuthorUntracked)
}

// The forge note fetched before the pop turns the pulled line human.
func TestStashPopAfterFastForwardPullWithFetchedTipNote(t *testing.T) {
	t.Parallel()
	stashPopAfterFastForwardPull(t, true,
		model.AuthorAI, model.AuthorHuman, model.AuthorHuman, model.AuthorHuman)
}

func TestStashPopAfterFastForwardPullWithAddedAndDeletedPaths(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := stashPullRepo(t)
	write(t, root, "forge.txt", "agent\none\ntwo\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	write(t, root, "new.txt", "agent new\n")
	captureAI(t, repo, "stash-session", "new.txt")
	git(t, root, "add", "new.txt")
	git(t, root, "rm", "-q", "local.txt")
	git(t, root, "stash", "-q")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	refTransaction(t, repo, "refs/stash", "", stash)
	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)

	// The added path is missing from the stash parent and the deleted path
	// from both sides; neither may hide that forge.txt kept the agent line.
	git(t, root, "stash", "pop", "-q")
	if result := refTransaction(t, repo, "refs/stash", stash, ""); result.Mapped != 1 {
		t.Fatalf("stash pop = %+v, want forge.txt restored", result)
	}
	assertNoStashNotes(t, repo)
	assertNoParkedCheckpoints(t, repo, commitAndAnnotate(t, root, repo, tip, "stashed work"))
	assertBlameAuthors(t, repo, "forge.txt",
		model.AuthorAI, model.AuthorHuman, model.AuthorHuman, model.AuthorUntracked)
	assertBlameAuthors(t, repo, "new.txt", model.AuthorAI)
}

func TestStashDropAfterFastForwardPullRestoresNothing(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := stashPullRepo(t)
	agentEditsBeforePull(t, root, repo)
	git(t, root, "stash", "-q")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	refTransaction(t, repo, "refs/stash", "", stash)
	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)
	readStashNote(t, repo, stash)

	git(t, root, "stash", "drop", "-q")
	if result := refTransaction(t, repo, "refs/stash", stash, ""); result.Mapped != 0 || result.Written != 1 {
		t.Fatalf("stash drop = %+v, want the note removed without a restore", result)
	}
	assertNoStashNotes(t, repo)
	_, state := readCheckpointsAndState(t, repo)
	if len(state.Pending.Files) != 0 {
		t.Fatalf("dropped stash restored pending files: %+v", state.Pending.Files)
	}
	write(t, root, "forge.txt", "one\ntwo\nforge\nhuman\n")
	commitAndAnnotate(t, root, repo, tip, "human")
	assertBlameAuthors(t, repo, "forge.txt",
		model.AuthorUntracked, model.AuthorUntracked, model.AuthorUntracked, model.AuthorHuman)
}

// TestStashPushOverEntryWithoutNote covers a push onto a list whose old top
// has no note: an ordinary push, not a pop of the entry below.
func TestStashPushOverEntryWithoutNote(t *testing.T) {
	t.Parallel()
	root, repo, _, _ := stashPullRepo(t)
	write(t, root, "local.txt", "local\nfirst\n")
	git(t, root, "stash", "-q")
	entry := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	refTransaction(t, repo, "refs/stash", "", entry)
	write(t, root, "forge.txt", "one\ntwo\nsecond\n")
	git(t, root, "stash", "-q")
	top := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	if result := refTransaction(t, repo, "refs/stash", entry, top); result.Written != 0 {
		t.Fatalf("push over an entry without a note = %+v, want an ordinary push", result)
	}
	assertNoStashNotes(t, repo)
}

func TestAutostashFastForwardPullKeepsAgentLines(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := stashPullRepo(t)
	agentEditsBeforePull(t, root, repo)
	stash, transaction := autostashFastForward(t, root, repo, base, tip)
	if transaction.Written != 1 {
		t.Fatalf("fast-forward = %+v, want the consumed checkpoints saved in the autostash note", transaction)
	}
	git(t, root, "stash", "apply", "-q", stash)
	git(t, root, "update-ref", "-d", "MERGE_AUTOSTASH")
	if result := refTransaction(t, repo, "MERGE_AUTOSTASH", stash, ""); result.Mapped != 1 {
		t.Fatalf("applied autostash = %+v, want forge.txt restored", result)
	}
	assertNoStashNotes(t, repo)
	assertNoParkedCheckpoints(t, repo, commitAndAnnotate(t, root, repo, tip, "autostashed work"))
	assertBlameAuthors(t, repo, "forge.txt",
		model.AuthorAI, model.AuthorHuman, model.AuthorHuman, model.AuthorUntracked)
	assertBlameAuthors(t, repo, "local.txt", model.AuthorHuman, model.AuthorAI)
}

func TestConflictingAutostashKeepsAgentLinesUntilDrop(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := stashPullRepo(t)
	// The agent edits the line next to the one the pull appends, so the
	// autostash does not apply cleanly.
	write(t, root, "forge.txt", "one\nagent two\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	stash, _ := autostashFastForward(t, root, repo, base, tip)
	// Git keeps an autostash it cannot apply in the stash list, then
	// deletes MERGE_AUTOSTASH. Both keep the note for the later drop.
	git(t, root, "stash", "store", "-q", "-m", "autostash", stash)
	if result := refTransaction(t, repo, "refs/stash", "", stash); result.Written != 0 {
		t.Fatalf("stored autostash = %+v, want the saved note kept", result)
	}
	git(t, root, "update-ref", "-d", "MERGE_AUTOSTASH")
	if result := refTransaction(t, repo, "MERGE_AUTOSTASH", stash, ""); result.Written != 0 || result.Mapped != 0 {
		t.Fatalf("deleted MERGE_AUTOSTASH = %+v, want the stored autostash untouched", result)
	}
	readStashNote(t, repo, stash)

	// The developer resolves the conflict by hand and drops the entry.
	write(t, root, "forge.txt", "one\nagent two\nforge\n")
	git(t, root, "stash", "drop", "-q")
	if result := refTransaction(t, repo, "refs/stash", stash, ""); result.Mapped != 1 {
		t.Fatalf("drop after resolving = %+v, want forge.txt restored", result)
	}
	assertNoStashNotes(t, repo)
	commitAndAnnotate(t, root, repo, tip, "resolved")
	assertBlameAuthors(t, repo, "forge.txt", model.AuthorHuman, model.AuthorAI, model.AuthorUntracked)
}

// TestConflictingAutostashStoreKeepsOlderStashNote covers git stash store of
// a conflicting autostash: it pushes onto the list with the older entry
// below, while MERGE_AUTOSTASH still names the stored entry. The store must
// not read as a pop of the entry it covers.
func TestConflictingAutostashStoreKeepsOlderStashNote(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := stashPullRepo(t)
	// An older entry with its note stays in the list.
	write(t, root, "local.txt", "local\nstashed\n")
	git(t, root, "stash", "-q")
	entry := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	writeStashFixtureNote(t, repo, entry,
		encodeCoverageNote(t, makeCoverageNoteForContent(mustBlob(t, repo, entry, "local.txt"), "local.txt", 2)))
	// The agent edits the line next to the one the pull appends, so the
	// autostash does not apply cleanly.
	write(t, root, "forge.txt", "one\nagent two\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	stash, _ := autostashFastForward(t, root, repo, base, tip)
	// Git stores the autostash it cannot apply onto the list and deletes
	// MERGE_AUTOSTASH only after the store.
	git(t, root, "stash", "store", "-q", "-m", "autostash", stash)
	if result := refTransaction(t, repo, "refs/stash", entry, stash); result.Written != 0 {
		t.Fatalf("stored autostash over an entry = %+v, want both notes kept", result)
	}
	git(t, root, "update-ref", "-d", "MERGE_AUTOSTASH")
	if result := refTransaction(t, repo, "MERGE_AUTOSTASH", stash, ""); result.Written != 0 || result.Mapped != 0 {
		t.Fatalf("deleted MERGE_AUTOSTASH = %+v, want the stored autostash untouched", result)
	}
	readStashNote(t, repo, entry)
	readStashNote(t, repo, stash)
}

func TestAutostashAbortKeepsPendingRanges(t *testing.T) {
	t.Parallel()
	root, repo, _, _ := stashPullRepo(t)
	// A partial commit leaves the second agent line pending.
	write(t, root, "local.txt", "local\nai one\n")
	captureAI(t, repo, "abort-session", "local.txt")
	git(t, root, "add", "local.txt")
	write(t, root, "local.txt", "local\nai one\nai two\n")
	captureAI(t, repo, "abort-session", "local.txt")
	git(t, root, "commit", "-q", "-m", "partial")
	partial := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	_, before := readCheckpointsAndState(t, repo)
	if _, ok := before.Pending.Files["local.txt"]; !ok {
		t.Fatalf("pending after partial commit = %+v", before.Pending.Files)
	}

	stash := strings.TrimSpace(git(t, root, "stash", "create", "autostash"))
	git(t, root, "update-ref", "MERGE_AUTOSTASH", stash)
	if result := refTransaction(t, repo, "MERGE_AUTOSTASH", "", stash); result.Written != 1 {
		t.Fatalf("autostash push = %+v, want a stash note", result)
	}
	_, state := readCheckpointsAndState(t, repo)
	if len(state.Pending.Files) != len(before.Pending.Files) {
		t.Fatalf("autostash push moved pending ranges: %+v", state.Pending.Files)
	}
	// git merge --abort deletes MERGE_AUTOSTASH before it resets and
	// applies the autostash, so the hook sees a worktree without it. The
	// ref goes first, or git reset would move the autostash into the stash
	// list.
	git(t, root, "update-ref", "-d", "MERGE_AUTOSTASH")
	git(t, root, "reset", "-q", "--hard")
	if result := refTransaction(t, repo, "MERGE_AUTOSTASH", stash, ""); result.Mapped != 0 || result.Written != 1 {
		t.Fatalf("aborted autostash = %+v, want the note removed without a restore", result)
	}
	assertNoStashNotes(t, repo)
	git(t, root, "stash", "apply", "-q", stash)

	commitAndAnnotate(t, root, repo, partial, "after abort")
	assertBlameAuthors(t, repo, "local.txt", model.AuthorHuman, model.AuthorAI, model.AuthorAI)
}

func TestDroppingLastStashKeepsOpenAutostashNote(t *testing.T) {
	t.Parallel()
	root, repo, _, _ := stashPullRepo(t)
	write(t, root, "local.txt", "local\nstashed\n")
	git(t, root, "stash", "-q")
	entry := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	writeStashFixtureNote(t, repo, entry,
		encodeCoverageNote(t, makeCoverageNoteForContent(mustBlob(t, repo, entry, "local.txt"), "local.txt", 2)))
	// A merge with --autostash is still open when the last entry goes. The
	// reset goes before the ref, or it would move the autostash into the
	// stash list.
	write(t, root, "forge.txt", "one\ntwo\nautostashed\n")
	autostash := strings.TrimSpace(git(t, root, "stash", "create", "autostash"))
	git(t, root, "reset", "-q", "--hard")
	git(t, root, "update-ref", "MERGE_AUTOSTASH", autostash)
	writeStashFixtureNote(t, repo, autostash,
		encodeCoverageNote(t, makeCoverageNoteForContent(mustBlob(t, repo, autostash, "forge.txt"), "forge.txt", 3)))

	git(t, root, "stash", "drop", "-q")
	if _, exists, err := repo.RefValue("refs/stash"); err != nil || exists {
		t.Fatalf("stash list after dropping the last entry: exists=%t err=%v", exists, err)
	}
	if result := refTransaction(t, repo, "refs/stash", entry, ""); result.Written != 1 {
		t.Fatalf("last stash drop = %+v, want only the entry note removed", result)
	}
	if _, found, err := repo.ReadNoteRef(stashNotesRef, entry); err != nil || found {
		t.Fatalf("dropped entry note found=%t err=%v", found, err)
	}
	readStashNote(t, repo, autostash)
}

func TestStashNoteWithPendingRangesTakesConsumedCheckpoints(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "forge.txt", "one\ntwo\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	// A partial commit leaves ai2 pending, so stash push moves it into the
	// stash note. The agent adds ai3 afterwards, which only a checkpoint
	// records.
	write(t, root, "forge.txt", "one\ntwo\nai1\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	git(t, root, "add", "forge.txt")
	write(t, root, "forge.txt", "one\ntwo\nai1\nai2\n")
	captureAI(t, repo, "stash-session", "forge.txt")
	git(t, root, "commit", "-q", "-m", "partial")
	partial := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "-b", "forge")
	write(t, root, "forge.txt", "forge\none\ntwo\nai1\n")
	git(t, root, "commit", "-q", "-m", "forge", "forge.txt")
	tip := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	// The checkout dance lost the pending worktree content; the agent edit
	// that follows starts from it.
	git(t, root, "checkout", "-q", "main")
	write(t, root, "forge.txt", "one\ntwo\nai1\nai2\nai3\n")
	captureAI(t, repo, "stash-session", "forge.txt")

	git(t, root, "stash", "-q")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	if result := refTransaction(t, repo, "refs/stash", "", stash); result.Written != 1 {
		t.Fatalf("stash push = %+v, want pending ranges in the stash note", result)
	}
	pushed := readStashNote(t, repo, stash)
	git(t, root, "merge", "-q", "--ff-only", "forge")
	if transaction := runFastForwardHooks(t, repo, partial, tip); transaction.Written != 1 {
		t.Fatalf("fast-forward = %+v, want the stash note updated", transaction)
	}
	saved := readStashNote(t, repo, stash)
	if saved.Files["forge.txt"].Blob == pushed.Files["forge.txt"].Blob {
		t.Fatalf("stash note after fast-forward = %+v, want the stashed forge.txt", saved)
	}

	git(t, root, "stash", "pop", "-q")
	refTransaction(t, repo, "refs/stash", stash, "")
	assertNoStashNotes(t, repo)
	commitAndAnnotate(t, root, repo, tip, "stashed work")
	assertBlameAuthors(t, repo, "forge.txt", model.AuthorUntracked, model.AuthorHuman, model.AuthorHuman,
		model.AuthorAI, model.AuthorAI, model.AuthorAI)
}
