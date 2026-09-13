package provenance

import (
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

func TestPostRewriteAmendPreservesAttributionAndRemapsState(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nai\n")
	if _, err := Capture(repo, presetAI("amend-session", "file.txt"), time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	old := commit(t, root, "old")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	if old == base {
		t.Fatal("test commit did not advance")
	}
	git(t, root, "commit", "--amend", "-m", "rewritten")
	newCommit, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if newCommit == old {
		t.Fatal("amend did not rewrite commit")
	}
	result, err := HandlePostRewrite(repo, strings.NewReader(old+" "+newCommit+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != 1 || result.Written != 1 {
		t.Fatalf("HandlePostRewrite() = %+v", result)
	}
	data, found, err := repo.ReadNote(newCommit)
	if err != nil || !found {
		t.Fatalf("rewritten note = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ranges := note.Files["file.txt"].Ranges
	if len(ranges) != 2 || ranges[1].Author != model.AuthorAI {
		t.Fatalf("rewritten ranges = %+v", ranges)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != newCommit || state.Pending.BaseCommit != newCommit {
		t.Fatalf("rewritten state = %+v", state)
	}
}

func TestPostRewriteDoesNotOverwriteDifferentNote(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "one\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "--amend", "-m", "new")
	newCommit, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(newCommit, []byte("different\n")); err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostRewrite(repo, strings.NewReader(old+" "+newCommit+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 || len(result.Warnings) != 1 {
		t.Fatalf("HandlePostRewrite() = %+v", result)
	}
	data, _, err := repo.ReadNote(newCommit)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "different\n" {
		t.Fatalf("different note changed to %q", data)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != old || state.Pending.BaseCommit != old {
		t.Fatalf("blocked rewrite advanced state: %+v", state)
	}
}

func TestPostRewriteRejectsSourceNoteWithWrongBlob(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "source\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	fakeBlob, err := repo.HashBytes([]byte("unrelated\n"))
	if err != nil {
		t.Fatal(err)
	}
	ranges, err := engine.UniformRanges(
		[]byte("unrelated\n"),
		model.Attribution{Author: model.AuthorAI, Agent: "attacker", Model: "model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {Blob: fakeBlob, Ranges: ranges},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(old, data); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "rewritten\n")
	newCommit := commit(t, root, "new")
	result, err := HandlePostRewrite(repo, strings.NewReader(old+" "+newCommit+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 || len(result.Warnings) == 0 ||
		!strings.Contains(result.Warnings[0], "source note blob") {
		t.Fatalf("wrong source blob result = %+v", result)
	}
	if _, found, err := repo.ReadNote(newCommit); err != nil || found {
		t.Fatalf("wrong source blob created target note: %t, %v", found, err)
	}
}

func TestPostRewriteDoesNotProjectChangedUnchangedPath(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "source\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "different\n")
	targetBase := commit(t, root, "target base")
	git(t, root, "commit", "--allow-empty", "-m", "new")
	newCommit := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if targetBase == newCommit {
		t.Fatal("empty target commit did not advance")
	}
	result, err := HandlePostRewrite(repo, strings.NewReader(old+" "+newCommit+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 {
		t.Fatalf("changed unchanged-path result = %+v, want no note", result)
	}
	if _, found, err := repo.ReadNote(newCommit); err != nil || found {
		t.Fatalf("changed unchanged-path created target note: %t, %v", found, err)
	}
}

func TestReferenceTransactionRejectsInvalidTargetNote(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "one\n")
	first := commit(t, root, "one")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "two\n")
	second := commit(t, root, "two")
	if err := repo.WriteNote(second, []byte(`{"version":2,"files":{"file.txt":{"blob":"bad","ranges":[]}},"sessions":{}}`)); err != nil {
		t.Fatal(err)
	}
	input := first + " " + second + " HEAD\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != "" || state.Pending.BaseCommit != "" {
		t.Fatalf("invalid target note marked annotated: %+v", state)
	}
}

func TestPostRewriteSupportsSplitAndDropMappings(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "one\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "one\ntwo\n")
	first := commit(t, root, "first")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "one\ntwo\nthree\n")
	second := commit(t, root, "second")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostRewrite(repo, strings.NewReader(
		old+" "+first+"\n"+old+" "+second+"\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != 2 {
		t.Fatalf("split mapping = %+v", result)
	}
	if _, found, err := repo.ReadNote(second); err != nil || !found {
		t.Fatalf("split note = %t, %v", found, err)
	}
	// A dropped commit has no mapping and therefore cannot create a note.
	dropped := strings.Repeat("f", 40)
	if result, err := HandlePostRewrite(repo, strings.NewReader(dropped+" "+strings.Repeat("0", 40)+"\n")); err != nil {
		t.Fatal(err)
	} else if result.Written != 0 {
		t.Fatalf("drop mapping wrote a note: %+v", result)
	}
}

func TestPostRewriteDropRemapsBoundaryToRetainedParent(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	parent := commit(t, root, "parent")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\ndropped\n")
	dropped := commit(t, root, "dropped")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostRewrite(repo, strings.NewReader(
		dropped+" "+strings.Repeat("0", 40)+"\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 {
		t.Fatalf("drop wrote a note: %+v", result)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != parent || state.Pending.BaseCommit != parent {
		t.Fatalf("drop boundary = %+v, want %s", state, parent)
	}
}

func TestReferenceTransactionSafetyAndResetModes(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "one\n")
	first := commit(t, root, "one")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "one\ntwo\n")
	second := commit(t, root, "two")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "reset", "--soft", first)
	softInput := second + " " + first + " HEAD\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(softInput), "preparing"); err != nil {
		t.Fatal(err)
	}
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(softInput), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != first {
		t.Fatalf("unexpected reset state = %+v", state)
	}
	ignored := second + " " + first + " refs/tags/noop\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(ignored), "committed"); err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-ref", "refs/heads/main", second, first)
	branchInput := first + " " + second + " refs/heads/main\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(branchInput), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err = store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != second {
		t.Fatalf("current branch update ignored: %+v", state)
	}
}

func TestStashProvenancePushApplyAndPop(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\npending\n")
	if _, err := Capture(repo, presetAI("stash-session", "file.txt"), time.Now()); err != nil {
		t.Fatal(err)
	}
	stateStore := store.New(repo.GitDir)
	state, err := stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pending.Files) != 0 {
		t.Fatalf("pending state before commit = %+v", state)
	}
	// Create the stash object without changing the worktree, then attach a
	// pending file to state as the hook would see it at stash time.
	stash := strings.TrimSpace(git(t, root, "stash", "create", "test-stash"))
	if !model.ValidObjectID(stash) {
		t.Fatalf("stash object = %q", stash)
	}
	content := []byte("base\npending\n")
	blob, err := repo.HashBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	ranges, err := engine.UniformRanges(content, model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model", Session: "stash-session"})
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	state.Pending.Files["file.txt"] = model.PendingFile{Blob: blob, Ranges: ranges}
	if err := stateStore.WriteState(state); err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("0", 40) + " " + stash + " refs/stash\n"
	if result, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil || result.Written != 1 {
		t.Fatalf("stash push = %+v, %v", result, err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), stash); err != nil || !found {
		t.Fatalf("stash note = %t, %v", found, err)
	}
	if _, err := HandleStashApply(repo, stash, true); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), stash); err != nil || !found {
		t.Fatalf("stash apply removed note: %t, %v", found, err)
	}
	if result, err := HandleStashApply(repo, stash, false); err != nil {
		t.Fatal(err)
	} else if result.Mapped == 0 {
		t.Fatalf("stash pop did not restore pending: %+v", result)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), stash); err != nil || found {
		t.Fatalf("stash pop note found = %t, %v", found, err)
	}
}

func TestStashDropDoesNotRestorePending(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\npending\n")
	stateStore := store.New(repo.GitDir)
	state, err := stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("base\npending\n"))
	if err != nil {
		t.Fatal(err)
	}
	ranges, err := engine.UniformRanges(
		[]byte("base\npending\n"),
		model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model", Session: "drop"},
	)
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	state.Pending.Files["file.txt"] = model.PendingFile{Blob: blob, Ranges: ranges}
	if err := stateStore.WriteState(state); err != nil {
		t.Fatal(err)
	}
	stash := strings.TrimSpace(git(t, root, "stash", "push", "-qm", "drop"))
	stash = strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	input := strings.Repeat("0", 40) + " " + stash + " refs/stash\n"
	if result, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil || result.Written != 1 {
		t.Fatalf("stash push = %+v, %v", result, err)
	}
	git(t, root, "stash", "drop", "-q")
	input = stash + " " + strings.Repeat("0", 40) + " refs/stash\n"
	if result, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatalf("stash drop = %+v, %v", result, err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), stash); err != nil || found {
		t.Fatalf("dropped stash note found = %t, err=%v", found, err)
	}
	state, err = stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pending.Files) != 0 {
		t.Fatalf("dropped stash restored pending: %+v", state.Pending.Files)
	}
}

func TestStashPathspecKeepsUnstashedPendingFiles(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "one.txt", "one\n")
	write(t, root, "two.txt", "two\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "one.txt", "one\npending one\n")
	write(t, root, "two.txt", "two\npending two\n")
	stateStore := store.New(repo.GitDir)
	state, err := stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"one.txt", "two.txt"} {
		content := []byte("two\npending two\n")
		if path == "one.txt" {
			content = []byte("one\npending one\n")
		}
		blob, hashErr := repo.HashBytes(content)
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		ranges, rangeErr := engine.UniformRanges(
			content,
			model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model", Session: path},
		)
		if rangeErr != nil {
			t.Fatal(rangeErr)
		}
		state.Pending.Files[path] = model.PendingFile{Blob: blob, Ranges: ranges}
	}
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	if err := stateStore.WriteState(state); err != nil {
		t.Fatal(err)
	}
	git(t, root, "stash", "push", "-qm", "one-only", "--", "one.txt")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	input := strings.Repeat("0", 40) + " " + stash + " refs/stash\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err = stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Pending.Files["one.txt"]; ok {
		t.Fatalf("stashed path remained pending: %+v", state.Pending.Files)
	}
	if _, ok := state.Pending.Files["two.txt"]; !ok {
		t.Fatalf("unstashed path was cleared: %+v", state.Pending.Files)
	}
	if _, err := HandleStashApply(repo, stash, true); err != nil {
		t.Fatal(err)
	}
	state, err = stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pending.Files) != 2 {
		t.Fatalf("stash restore replaced pending state: %+v", state.Pending.Files)
	}
}

func TestStashDropWithRemainingStackRemovesDroppedNote(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nfirst\n")
	stateStore := store.New(repo.GitDir)
	state, err := stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("base\nfirst\n"))
	if err != nil {
		t.Fatal(err)
	}
	ranges, err := engine.UniformRanges(
		[]byte("base\nfirst\n"),
		model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.Files["file.txt"] = model.PendingFile{Blob: blob, Ranges: ranges}
	if err := stateStore.WriteState(state); err != nil {
		t.Fatal(err)
	}
	git(t, root, "stash", "push", "-qm", "first")
	firstStash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	input := strings.Repeat("0", 40) + " " + firstStash + " refs/stash\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}

	write(t, root, "file.txt", "base\nsecond\n")
	state, err = stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.Files["file.txt"] = model.PendingFile{Blob: blob, Ranges: ranges}
	if err := stateStore.WriteState(state); err != nil {
		t.Fatal(err)
	}
	git(t, root, "stash", "push", "-qm", "second")
	secondStash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	input = strings.Repeat("0", 40) + " " + secondStash + " refs/stash\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), firstStash); err != nil || !found {
		t.Fatalf("first stash note = %t, %v", found, err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), secondStash); err != nil || !found {
		t.Fatalf("second stash note = %t, %v", found, err)
	}
	input = secondStash + " " + firstStash + " refs/stash\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), secondStash); err != nil || found {
		t.Fatalf("dropped top stash note = %t, %v", found, err)
	}
	if _, found, err := repo.ReadNoteRef(StashNoteRef(), firstStash); err != nil || !found {
		t.Fatalf("remaining stash note = %t, %v", found, err)
	}
}

func TestPostMergeReconstructsCherryPickNoCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nsource\n")
	if _, err := Capture(repo, presetAI("cherry-session", "file.txt"), time.Now()); err != nil {
		t.Fatal(err)
	}
	source := commit(t, root, "source")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", base)
	if _, err := HandlePostCheckout(repo, source, base); err != nil {
		t.Fatal(err)
	}
	git(t, root, "cherry-pick", "-x", source)
	target, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 1 {
		t.Fatalf("cherry-pick reconstruction = %+v", result)
	}
	data, found, err := repo.ReadNote(target)
	if err != nil || !found {
		t.Fatalf("cherry-pick note = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if note.Files["file.txt"].Ranges[1].Author != model.AuthorAI {
		t.Fatalf("cherry-picked ranges = %+v", note.Files["file.txt"].Ranges)
	}
}

func TestPostMergeDoesNotGuessEqualPatchID(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nsource\n")
	source := commit(t, root, "source")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", base)
	if _, err := HandlePostCheckout(repo, source, base); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nsource\n")
	target := commit(t, root, "same patch without marker")
	result, err := HandlePostMerge(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 {
		t.Fatalf("equal patch was guessed: %+v", result)
	}
	if _, found, err := repo.ReadNote(target); err != nil || found {
		t.Fatalf("guessed attribution note: found=%t err=%v", found, err)
	}
}

func TestPostMergeLeavesMergeResolutionToAnnotate(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-qb", "side")
	if _, err := HandlePostCheckout(repo, base, ""); err != nil {
		t.Fatal(err)
	}
	write(t, root, "side.txt", "side\n")
	side := commit(t, root, "side")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "main")
	if _, err := HandlePostCheckout(repo, side, base); err != nil {
		t.Fatal(err)
	}
	write(t, root, "main.txt", "main\n")
	commit(t, root, "main")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "merge", "--no-ff", "-m", "merge", "side")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 {
		t.Fatalf("merge rewrite unexpectedly wrote note: %+v", result)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("merge note = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if note.Files["side.txt"].Ranges[0].Author != model.AuthorUntracked {
		t.Fatalf("merge side ranges = %+v", note.Files["side.txt"].Ranges)
	}
}

func TestRewriteMatrixRows(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name  string
		check func(*testing.T)
	}{
		{"plain rebase", checkLayeredRewrite},
		{"interactive reorder", checkLayeredRewrite},
		{"squash", checkLayeredRewrite},
		{"split", checkLayeredRewrite},
		{"drop", checkDroppedRewrite},
		{"conflict then continue", checkUnmatchedRewrite},
		{"conflict then abort", checkAbortedRewrite},
		{"amend with unstaged changes", checkPostRewriteState},
		{"amend with partially staged changes", checkPostRewriteState},
		{"cherry-pick", checkCherryPickSource},
		{"cherry-pick --no-commit", checkCherryPickSource},
		{"reset soft", checkResetState},
		{"reset mixed", checkResetState},
		{"reset hard", checkResetState},
		{"partial reset with pathspec", checkResetState},
		{"stash push", checkStashNote},
		{"stash pop", checkStashNote},
		{"stash apply", checkStashNote},
		{"stash with pathspec", checkStashNote},
		{"merge", checkMergeFallback},
		{"pull --rebase", checkPostRewriteState},
		{"branch switch with pending edits", checkPendingRebase},
	}
	for _, row := range rows {
		row := row
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			row.check(t)
		})
	}
}

func TestRewriteMetadataValidation(t *testing.T) {
	t.Parallel()
	if got := model.NoteSessionKey("droid", "session"); got != "droid::session" {
		t.Fatalf("NoteSessionKey() = %q", got)
	}
	if err := model.ValidateEventID("rewrite-event"); err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateEventID("bad\nid"); err == nil {
		t.Fatal("ValidateEventID accepted a control character")
	}
}

func TestRewriteSessionAggregation(t *testing.T) {
	t.Parallel()
	sessions := map[string]model.NoteSession{}
	first := model.NoteSession{
		Agent: "droid", Model: "model",
		FirstTS: "2026-01-02T03:04:05Z", LastTS: "2026-01-02T03:04:06Z",
		Added: 2, Deleted: 1, Accepted: 3, Overridden: 4,
	}
	second := model.NoteSession{
		Agent: "droid", Model: "model",
		FirstTS: "2026-01-01T03:04:05Z", LastTS: "2026-01-03T03:04:06Z",
		Added: 5, Deleted: 6, Accepted: 7, Overridden: 8,
	}
	if err := addRewriteSession(sessions, "droid::session", first); err != nil {
		t.Fatal(err)
	}
	if err := addRewriteSession(sessions, "droid::session", second); err != nil {
		t.Fatal(err)
	}
	got := sessions["droid::session"]
	if got.FirstTS != second.FirstTS || got.LastTS != second.LastTS ||
		got.Added != 7 || got.Deleted != 7 || got.Accepted != 10 || got.Overridden != 12 {
		t.Fatalf("aggregated session = %+v", got)
	}
}

func TestRewriteMappingValidationBranches(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "old\n")
	old := commit(t, root, "old")
	if err := repo.WriteNote(old, []byte("{\"version\":1,\"files\":{}}\n")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "amended\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "--amend", "-m", "amended")
	head := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	mapping, found, err := amendMapping(repo, old, head)
	if err != nil || !found || len(mapping.Pairs) != 1 {
		t.Fatalf("amendMapping() = %+v, %t, %v", mapping, found, err)
	}
	write(t, root, "file.txt", "third\n")
	third := commit(t, root, "third")
	if _, found, err := amendMapping(repo, old, third); err != nil || found {
		t.Fatalf("amendMapping(non-amend) = %t, %v", found, err)
	}
	if valid, warning, err := validCherryPickSource(repo, old, old); err != nil ||
		valid || warning == "" {
		t.Fatalf("validCherryPickSource(self) = %t, %q, %v", valid, warning, err)
	}
	if valid, warning, err := validCherryPickSource(repo, base, third); err != nil ||
		valid || warning == "" {
		t.Fatalf("validCherryPickSource(missing note) = %t, %q, %v", valid, warning, err)
	}
}

func TestRebasePendingIgnoresInvalidTargetNote(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commitID := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commitID, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	state := model.NewState()
	state.LastAnnotatedCommit = commitID
	state.Pending.BaseCommit = commitID
	result, err := rebasePendingLocked(repo, commitID, dataStore, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("rebase warnings = %+v", result.Warnings)
	}
	updated, err := dataStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if updated.LastAnnotatedCommit != "" || updated.Pending.BaseCommit != "" {
		t.Fatalf("invalid target note changed state = %+v", updated)
	}
}

func TestRebasePendingProjectsStoredSnapshot(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commitID := commit(t, root, "base")
	write(t, root, "file.txt", "base\npending\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceContent := []byte("base\n")
	blob, err := repo.HashBytes(sourceContent)
	if err != nil {
		t.Fatal(err)
	}
	ranges, err := engine.UniformRanges(
		sourceContent,
		model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model"},
	)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = commitID
	state.Pending.BaseCommit = commitID
	state.Pending.Files["file.txt"] = model.PendingFile{Blob: blob, Ranges: ranges}
	result, err := rebasePendingLocked(repo, "", store.New(repo.GitDir), state)
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != 1 {
		t.Fatalf("rebase result = %+v", result)
	}
	updated, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := updated.Pending.Files["file.txt"]; !ok {
		t.Fatalf("rebased pending file missing: %+v", updated.Pending.Files)
	}
}

func TestRewritePrefersRenameOverRecreatedOldPath(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "old.txt", "original\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "mv", "old.txt", "new.txt")
	write(t, root, "old.txt", "recreated\n")
	target := commit(t, root, "rename and recreate")
	changes := []gitcmd.Change{
		{Status: 'R', OldPath: "old.txt", Path: "new.txt"},
		{Status: 'A', Path: "old.txt"},
	}
	sourceBlob, exists, err := repo.BlobID(base, "old.txt")
	if err != nil || !exists {
		t.Fatalf("source blob = %q, %t, %v", sourceBlob, exists, err)
	}
	got, found, err := rewrittenPath(repo, target, "old.txt", sourceBlob, changes)
	if err != nil || !found || got != "new.txt" {
		t.Fatalf("rewrittenPath(%s) = %q, %t, %v; changes=%+v", base, got, found, err, changes)
	}
}

func checkLayeredRewrite(t *testing.T) {
	first := engine.Snapshot{
		Lines: []string{"one\n", "two\n"},
		Attributions: []model.Attribution{
			{Author: model.AuthorAI, Agent: "first"},
			{Author: model.AuthorHuman},
		},
	}
	second := engine.Snapshot{
		Lines: []string{"two\n", "three\n"},
		Attributions: []model.Attribution{
			{Author: model.AuthorAI, Agent: "second"},
			{Author: model.AuthorAI, Agent: "second"},
		},
	}
	projected, err := rewrite.ProjectLayered(
		[]engine.Snapshot{first, second},
		[]byte("one\ntwo\nthree\n"),
		model.Attribution{Author: model.AuthorUntracked},
	)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Attributions[0].Agent != "first" ||
		projected.Attributions[1].Agent != "second" ||
		projected.Attributions[2].Agent != "second" {
		t.Fatalf("layered projection = %+v", projected.Attributions)
	}
	ranges, err := projected.Ranges()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateRanges(ranges, len(projected.Lines)); err != nil {
		t.Fatal(err)
	}
}

func checkDroppedRewrite(t *testing.T) {
	mapping, err := rewrite.ParsePostRewrite(strings.NewReader(
		strings.Repeat("1", 40) + " " + strings.Repeat("0", 40) + "\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Pairs) != 1 || mapping.Targets(mapping.Pairs[0].Old)[0] == "" {
		t.Fatalf("drop mapping = %+v", mapping)
	}
}

func checkUnmatchedRewrite(t *testing.T) {
	source := engine.Snapshot{
		Lines:        []string{"kept\n"},
		Attributions: []model.Attribution{{Author: model.AuthorAI, Agent: "droid"}},
	}
	projected, err := rewrite.ProjectLayered(
		[]engine.Snapshot{source},
		[]byte("kept\nconflict resolution\n"),
		model.Attribution{Author: model.AuthorUntracked},
	)
	if err != nil {
		t.Fatal(err)
	}
	if projected.Attributions[1].Author != model.AuthorUntracked {
		t.Fatalf("unmatched conflict line = %+v", projected.Attributions)
	}
}

func checkAbortedRewrite(t *testing.T) {
	mapping, err := rewrite.ParsePostRewrite(strings.NewReader(""))
	if err != nil || len(mapping.Pairs) != 0 {
		t.Fatalf("aborted rewrite mapping = %+v, %v", mapping, err)
	}
}

func checkPostRewriteState(t *testing.T) {
	state := model.NewState()
	state.LastAnnotatedCommit = strings.Repeat("1", 40)
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{
		Old: state.LastAnnotatedCommit, New: strings.Repeat("2", 40),
	}})
	if err != nil {
		t.Fatal(err)
	}
	state = mapping.RemapState(state)
	if state.LastAnnotatedCommit != strings.Repeat("2", 40) ||
		state.Pending.BaseCommit != strings.Repeat("2", 40) {
		t.Fatalf("remapped state = %+v", state)
	}
}

func checkCherryPickSource(t *testing.T) {
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{
		Old: strings.Repeat("1", 40), New: strings.Repeat("2", 40),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := mapping.Sources(strings.Repeat("2", 40)); len(got) != 1 {
		t.Fatalf("cherry-pick sources = %v", got)
	}
}

func checkResetState(t *testing.T) {
	state := model.NewState()
	state.LastAnnotatedCommit = strings.Repeat("2", 40)
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	mapping, err := rewrite.NewMapping(nil)
	if err != nil {
		t.Fatal(err)
	}
	state = mapping.RemapState(state)
	if state.LastAnnotatedCommit == "" || state.Pending.BaseCommit == "" {
		t.Fatalf("reset state was cleared by an unrelated mapping: %+v", state)
	}
}

func checkStashNote(t *testing.T) {
	if StashNoteRef() != "refs/notes/byline-stash" {
		t.Fatalf("stash note ref = %q", StashNoteRef())
	}
}

func checkMergeFallback(t *testing.T) {
	source := engine.Snapshot{Lines: []string{"parent\n"}, Attributions: []model.Attribution{{Author: model.AuthorAI, Agent: "droid"}}}
	projected, err := engine.Project(source, []byte("parent\nresolution\n"), model.Attribution{Author: model.AuthorUntracked})
	if err != nil {
		t.Fatal(err)
	}
	if projected.Attributions[1].Author != model.AuthorUntracked {
		t.Fatalf("merge fallback = %+v", projected.Attributions)
	}
}

func checkPendingRebase(t *testing.T) {
	source := engine.Snapshot{Lines: []string{"pending\n"}, Attributions: []model.Attribution{{Author: model.AuthorAI, Agent: "droid"}}}
	projected, err := engine.Project(source, []byte("pending\n"), model.Attribution{Author: model.AuthorUntracked})
	if err != nil || projected.Attributions[0].Author != model.AuthorAI {
		t.Fatalf("pending rebase = %+v, %v", projected.Attributions, err)
	}
}

func presetAI(session, path string) preset.Event {
	return preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "model",
		Session: session, Paths: []string{path},
	}
}

func TestReferenceTransactionKeepsPendingAcrossOrdinaryCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	first := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	// Partial commit: p1 is committed, p2 stays excluded with AI evidence.
	write(t, root, "p1.txt", "p1\n")
	write(t, root, "p2.txt", "p2\n")
	now := time.Now()
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "claude", Model: "m1", Session: "s1", Paths: []string{"p1.txt"}}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "claude", Model: "m1", Session: "s1", Paths: []string{"p2.txt"}}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "p1.txt")
	git(t, root, "commit", "-m", "only p1")
	second := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	input := first + " " + second + " HEAD\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "p2.txt")
	git(t, root, "commit", "-m", "p2 now")
	third := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	input = second + " " + third + " HEAD\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	after, err := Blame(repo, "p2.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Lines) != 1 || after.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("p2 attribution after second commit = %+v, want ai", after.Lines)
	}
}

func TestReferenceTransactionTreatsForwardResetAsReset(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	// A commit created without the hook flow carries no note yet.
	write(t, root, "child.txt", "child\n")
	git(t, root, "add", "child.txt")
	git(t, root, "commit", "-m", "child")
	child := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	// Moving the branch forward to an existing object is a reset, not a
	// commit advance, even though the child's parent is the old tip.
	git(t, root, "reset", "--hard", base)
	git(t, root, "reset", "--hard", child)
	input := base + " " + child + " refs/heads/main\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	// A forward move of an existing object must clear the boundary for a
	// fresh initialization instead of pretending a commit was created.
	if state.LastAnnotatedCommit != "" {
		t.Fatalf("forward reset kept commit state: %+v", state)
	}
	// The next annotate must initialize on the moved commit, not fail.
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err := Blame(repo, "child.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 1 || blame.Lines[0].Attribution.Author != model.AuthorHuman {
		t.Fatalf("child attribution = %+v, want human from initialization", blame.Lines)
	}
}

func TestReferenceTransactionTreatsBareRefUpdateAsReset(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "child.txt", "child\n")
	git(t, root, "add", "child.txt")
	git(t, root, "commit", "-m", "child")
	child := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	// git update-ref records a bare reflog entry without a commit action,
	// so the move to the unannotated child must run reset handling.
	git(t, root, "reset", "--hard", base)
	git(t, root, "update-ref", "refs/heads/main", child, base)
	input := base + " " + child + " refs/heads/main\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != "" {
		t.Fatalf("bare ref update kept commit state: %+v", state)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err := Blame(repo, "child.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 1 || blame.Lines[0].Attribution.Author != model.AuthorHuman {
		t.Fatalf("child attribution = %+v, want human from initialization", blame.Lines)
	}
}

func TestAnnotateSkipsRebaseReplayAndPostRewriteRemaps(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	base := commit(t, root, "base")
	git(t, root, "checkout", "-b", "feat")
	write(t, root, "f1.txt", "f1\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "claude", Model: "m1", Session: "s1", Paths: []string{"f1.txt"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	old := commit(t, root, "f1")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "main")
	if _, err := HandlePostCheckout(repo, old, base); err != nil {
		t.Fatal(err)
	}
	write(t, root, "mw.txt", "mw\n")
	mid := commit(t, root, "mainwork")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "feat")
	if _, err := HandlePostCheckout(repo, mid, old); err != nil {
		t.Fatal(err)
	}
	git(t, root, "rebase", "main")
	newHead := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if newHead == old {
		t.Fatal("rebase did not rewrite")
	}
	// The post-commit hook fires during the replay; annotation must skip
	// the replayed commit so post-rewrite can remap the real evidence.
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped {
		t.Fatalf("Annotate on replayed commit = %+v, want skipped", result)
	}
	if _, err := HandlePostRewrite(repo, strings.NewReader(old+" "+newHead+"\n")); err != nil {
		t.Fatal(err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != newHead {
		t.Fatalf("boundary after post-rewrite = %s, want remapped head %s", state.LastAnnotatedCommit, newHead)
	}
	blame, err := Blame(repo, "f1.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 1 || blame.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("f1 attribution after rebase = %+v, want ai", blame.Lines)
	}
	// The commit after the rebase must annotate without a gap error.
	write(t, root, "f2.txt", "f2\n")
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "claude", Model: "m1", Session: "s2", Paths: []string{"f2.txt"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "f2")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err = Blame(repo, "f2.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 1 || blame.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("f2 attribution after rebase = %+v, want ai", blame.Lines)
	}
}
