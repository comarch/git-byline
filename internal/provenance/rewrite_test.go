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
	git(t, root, "cherry-pick", "-n", source)
	target := commit(t, root, "cherry-picked")
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
