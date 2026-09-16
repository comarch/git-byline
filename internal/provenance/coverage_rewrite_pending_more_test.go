package provenance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

func TestCoveragePendingRewriteBranches(t *testing.T) {
	t.Run("cached note exceeded", coverageApplyCachedNoteExceeded)
	t.Run("rewritten blob error", coverageApplyRewrittenBlobError)
	t.Run("rewritten blob missing", coverageApplyRewrittenBlobMissing)
	// The "rewritten note encode error" scenario needs more than 500
	// note files to trip the encode budget, and the rewrite pipeline
	// shells out to git for every file. That spawn storm tripped macOS
	// with EINVAL and runners with fork limits, so the branch stays
	// with the defensive residue instead of a flaky scenario.
	t.Run("handle head early return", coverageHandleHeadEarlyReturn)
	t.Run("handle head dirty error", coverageHandleHeadDirtyError)
	t.Run("handle head note error", coverageHandleHeadNoteError)
	t.Run("handle head invalid source note", coverageHandleHeadInvalidSourceNote)
	t.Run("handle head rebase pending", coverageHandleHeadRebasePending)
	t.Run("handle head rebase error", coverageHandleHeadRebaseError)
	t.Run("handle head project error", coverageHandleHeadProjectError)
	t.Run("handle head target note error", coverageHandleHeadTargetNoteError)
	t.Run("handle head state write error", coverageHandleHeadStateWriteError)
	t.Run("rewrite state validation", coverageRewriteStateValidationError)
	t.Run("rewrite state checkpoints error", coverageRewriteStateCheckpointsError)
	t.Run("rewrite state write error", coverageRewriteStateWriteError)
	t.Run("rewrite state final protect error", coverageRewriteStateFinalProtectError)
	t.Run("pending note read warning", coveragePendingNoteReadWarning)
	t.Run("pending note missing", coveragePendingNoteMissing)
	t.Run("pending note budget", coveragePendingNoteBudget)
	t.Run("pending note projection error", coveragePendingNoteProjectionError)
	t.Run("pending note hash error", coveragePendingNoteHashError)
	t.Run("handle head reset", coverageHandleHeadReset)
	t.Run("handle head reset target note error", coverageHandleHeadResetTargetNoteError)
	t.Run("handle head reset invalid target note", coverageHandleHeadResetInvalidTargetNote)
	t.Run("annotation boundary empty", coverageAnnotationBoundaryEmpty)
	t.Run("delete all removal error", coverageDeleteAllRemovalError)
	t.Run("delete all changed note", coverageDeleteAllChangedNote)
	t.Run("stash note encode error", coverageStashNoteEncodeError)
}

func coverageApplyCachedNoteExceeded(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	oldOne := commit(t, root, "old one")
	oldTwo := commitWithMessage(t, root, "old two")
	targetOne := commitWithMessage(t, root, "target one")
	targetTwo := commitWithMessage(t, root, "target two")
	targetThree := commitWithMessage(t, root, "target three")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(oldOne, encodeCoverageNote(t, makeCoverageNote(mustBlob(t, repo, oldOne, coverageFile)))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "large-note", oldTwo)
	mapping, err := rewrite.NewMapping([]rewrite.Pair{
		{Old: oldOne, New: targetOne},
		{Old: oldTwo, New: targetTwo},
		{Old: oldTwo, New: targetThree},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(fake, mapping)
	if err != nil || !containsCoverageWarning(result.Warnings, "note budget exceeded") {
		t.Fatalf("cached note exceeded = %+v, %v", result, err)
	}
}

func coverageApplyRewrittenBlobError(t *testing.T) {
	root, repo, old, target := rewriteRenameCoverageRepo(t)
	blob := mustBlob(t, repo, old, "old.txt")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForContent(blob, "old.txt", 21))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "target-blob-error", target)
	if _, err := applyRewriteMapping(fake, coverageMapping(t, old, target)); err == nil {
		t.Fatal("applyRewriteMapping accepted a rewritten blob failure")
	}
}

func coverageApplyRewrittenBlobMissing(t *testing.T) {
	root, repo, old, target := rewriteRenameCoverageRepo(t)
	blob := mustBlob(t, repo, old, "old.txt")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForContent(blob, "old.txt", 21))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "target-blob-missing", target)
	if _, err := applyRewriteMapping(fake, coverageMapping(t, old, target)); err != nil {
		t.Fatal(err)
	}
}

func coverageHandleHeadEarlyReturn(t *testing.T) {
	_, repo, old, target := headMoveCoverageRepo(t)
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err != nil {
		t.Fatal(err)
	}
}

func coverageHandleHeadDirtyError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	repo = fakeRewriteRepo(t, root, "dirty-error", "")
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a dirty path failure")
	}
}

func coverageHandleHeadNoteError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	repo = fakeRewriteRepo(t, root, "notes-read-error", old)
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted an old note read failure")
	}
}

func coverageHandleHeadInvalidSourceNote(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	if err := repo.WriteNote(old, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err != nil {
		t.Fatal(err)
	}
	_ = root
}

func coverageHandleHeadRebasePending(t *testing.T) {
	_, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err != nil {
		t.Fatal(err)
	}
}

func coverageHandleHeadRebaseError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	if err := repo.WriteNote(old, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "update-ref-error", "")
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a rebase state failure")
	}
}

func coverageHandleHeadProjectError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a pending projection failure")
	}
}

func coverageHandleHeadTargetNoteError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", target)
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a target note read failure")
	}
}

func coverageHandleHeadStateWriteError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "rewrite-state-write-error", "")
	t.Setenv("FAKE_GIT_STATE_PATH", store.New(fake.GitDir).StatePath())
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a state write failure")
	}
}

func coverageRewriteStateValidationError(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	state := model.NewState()
	state.Pending.BaseCommit = strings.Repeat("a", 40)
	if err := writeRewriteState(repo, store.New(repo.GitDir), state); err == nil {
		t.Fatal("writeRewriteState accepted invalid state")
	}
	_ = root
}

func coverageRewriteStateCheckpointsError(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.CheckpointPath(), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRewriteState(repo, dataStore, model.NewState()); err == nil {
		t.Fatal("writeRewriteState accepted invalid checkpoints")
	}
	_ = root
}

func coverageRewriteStateWriteError(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	fake := fakeRewriteRepo(t, root, "rewrite-state-write-error", "")
	t.Setenv("FAKE_GIT_STATE_PATH", store.New(fake.GitDir).StatePath())
	if err := os.MkdirAll(store.New(fake.GitDir).Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeRewriteState(fake, store.New(fake.GitDir), model.NewState()); err == nil {
		t.Fatal("writeRewriteState accepted a state write failure")
	}
	_ = repo
}

func coverageRewriteStateFinalProtectError(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	fake := fakeRewriteRepo(t, root, "update-ref-second-error", "")
	if err := writeRewriteState(fake, store.New(repo.GitDir), model.NewState()); err == nil {
		t.Fatal("writeRewriteState accepted a final retention failure")
	}
}

func coveragePendingNoteReadWarning(t *testing.T) {
	root, repo, old, _ := rewriteCoverageRepo(t)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := os.Remove(filepath.Join(root, coverageFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, coverageFile), 0o700); err != nil {
		t.Fatal(err)
	}
	note := makeCoverageNote(blob)
	if _, err := pendingFromNote(repo, note, newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)); err != nil {
		t.Fatal(err)
	}
}

func coveragePendingNoteMissing(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"missing.txt": {Blob: strings.Repeat("a", 40)},
		},
	}
	if _, err := pendingFromNote(repo, note, newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)); err != nil {
		t.Fatal(err)
	}
	_ = root
}

func coveragePendingNoteBudget(t *testing.T) {
	root, repo, old, _ := rewriteCoverageRepo(t)
	note := makeCoverageNote(mustBlob(t, repo, old, coverageFile))
	fake := fakeRewriteRepo(t, root, "blob-size-budget", "")
	if _, err := pendingFromNote(fake, note, newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)); err != nil {
		t.Fatal(err)
	}
}

func coveragePendingNoteProjectionError(t *testing.T) {
	root, repo, old, _ := rewriteCoverageRepo(t)
	blob := mustBlob(t, repo, old, coverageFile)
	note := makeCoverageNote(blob)
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := pendingFromNote(fake, note, newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)); err == nil {
		t.Fatal("pendingFromNote accepted a projection failure")
	}
}

func coveragePendingNoteHashError(t *testing.T) {
	root, repo, old, _ := rewriteCoverageRepo(t)
	blob := mustBlob(t, repo, old, coverageFile)
	note := makeCoverageNote(blob)
	fake := fakeRewriteRepo(t, root, "hash-error", "")
	if _, err := pendingFromNote(fake, note, newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)); err == nil {
		t.Fatal("pendingFromNote accepted a hash failure")
	}
}

func coverageHandleHeadReset(t *testing.T) {
	root, repo, old, _ := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	fake := fakeRewriteRepo(t, root, "rewrite-state-write-error", "")
	t.Setenv("FAKE_GIT_STATE_PATH", store.New(fake.GitDir).StatePath())
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: strings.Repeat("0", 40)}); err == nil {
		t.Fatal("handleHeadMove accepted a reset state write failure")
	}
}

func coverageHandleHeadResetTargetNoteError(t *testing.T) {
	root, repo, old, target := headMoveCoverageRepo(t)
	write(t, root, coverageFile, "old\n")
	setHeadMoveState(t, repo, old)
	fake := fakeRewriteRepo(t, root, "notes-read-error", target)
	if _, err := handleHeadMove(fake, rewrite.RefUpdate{Old: old, New: target}); err == nil {
		t.Fatal("handleHeadMove accepted a reset target note failure")
	}
}

func coverageHandleHeadResetInvalidTargetNote(t *testing.T) {
	_, repo, old, target := headMoveCoverageRepo(t)
	setHeadMoveState(t, repo, old)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(target, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: target}); err != nil {
		t.Fatal(err)
	}
}

func coverageAnnotationBoundaryEmpty(t *testing.T) {
	_, repo, _, _ := rewriteCoverageRepo(t)
	if got := annotationBoundary(repo, ""); got != "" {
		t.Fatalf("annotationBoundary(empty) = %q", got)
	}
	if got := annotationBoundary(repo, strings.Repeat("0", 40)); got != "" {
		t.Fatalf("annotationBoundary(zero) = %q", got)
	}
}

func coverageDeleteAllRemovalError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "notes-remove-error", "")
	if _, err := deleteAllDroppedStashNotesLocked(fake); err == nil {
		t.Fatal("deleteAllDroppedStashNotesLocked accepted a removal failure")
	}
	_ = stash
}

func coverageDeleteAllChangedNote(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "notes-remove-race", "")
	result, err := deleteAllDroppedStashNotesLocked(fake)
	if err != nil || !containsCoverageWarning(result.Warnings, "changed") {
		t.Fatalf("deleteAll changed note = %+v, %v", result, err)
	}
}

func coverageStashNoteEncodeError(t *testing.T) {
	root := testRepo(t)
	const fileCount = 501
	for index := 0; index < fileCount; index++ {
		write(t, root, fmt.Sprintf("stash-%03d.txt", index), "base\n")
	}
	base := commit(t, root, "base")
	for index := 0; index < fileCount; index++ {
		write(t, root, fmt.Sprintf("stash-%03d.txt", index), "pending\n")
	}
	stash := strings.TrimSpace(git(t, root, "stash", "create", "coverage"))
	if !model.ValidObjectID(stash) {
		t.Fatalf("stash object = %q", stash)
	}
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]model.PendingFile, fileCount)
	for index := 0; index < fileCount; index++ {
		files[fmt.Sprintf("stash-%03d.txt", index)] = model.PendingFile{
			Blob: blob,
			Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}},
		}
	}
	state := model.NewState()
	state.LastAnnotatedCommit = base
	state.Pending = model.PendingState{BaseCommit: base, Files: files}
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err == nil ||
		!strings.Contains(err.Error(), "encode stash attribution") {
		t.Fatalf("handleStashMove accepted an oversized stash note: %v", err)
	}
}

func headMoveCoverageRepo(t *testing.T) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	write(t, root, coverageFile, "old\n")
	old := commit(t, root, "old")
	git(t, root, "checkout", "-q", "-b", "target")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	git(t, root, "checkout", "-q", "main")
	write(t, root, coverageFile, "dirty\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, old, target
}

func setHeadMoveState(t *testing.T, repo *gitcmd.Repo, old string) {
	t.Helper()
	state := model.NewState()
	state.LastAnnotatedCommit = old
	state.Pending.BaseCommit = old
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
}

func rewriteRenameCoverageRepo(t *testing.T) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, "old.txt", strings.Repeat("same\n", 20)+"old\n")
	old := commit(t, root, "old")
	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", strings.Repeat("same\n", 20)+"new\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, old, target
}
