package provenance

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

func TestCoverageApplyRewriteMoreBranches(t *testing.T) {
	t.Run("note cache budget", coverageApplyNoteCacheBudget)
	t.Run("note cache exceeded", coverageApplyNoteCacheExceeded)
	t.Run("invalid source note", coverageApplyInvalidSourceNote)
	t.Run("source blob budget", coverageApplySourceBlobBudget)
	t.Run("source snapshot", coverageApplySourceSnapshotError)
	t.Run("rewritten directory", coverageApplyRewrittenDirectoryError)
	t.Run("deleted path", coverageApplyDeletedPath)
	t.Run("rewritten content", coverageApplyRewrittenContentError)
	t.Run("rewritten size budget", coverageApplyRewrittenSizeBudget)
	t.Run("binary rewritten content", coverageApplyBinaryRewrittenContent)
	t.Run("matcher budget", coverageApplyMatcherBudget)
	t.Run("existing note budget", coverageApplyExistingNoteBudget)
	t.Run("read target note", coverageApplyReadTargetNoteError)
	t.Run("write target note", coverageApplyWriteTargetNoteError)
	t.Run("session conflict", coverageApplySessionConflict)
	t.Run("head error", coverageApplyHeadError)
	t.Run("head note error", coverageApplyHeadNoteError)
	t.Run("ancestor remap", coverageApplyAncestorRemap)
	t.Run("zero boundary parent", coverageApplyZeroBoundaryParentError)
	t.Run("ancestor parent", coverageApplyAncestorParentError)
	t.Run("state write", coverageApplyStateWriteError)
}

func TestCoveragePostMergeAndCherryPickBranches(t *testing.T) {
	t.Run("HEAD error", coveragePostMergeHeadError)
	t.Run("unborn", coveragePostMergeUnborn)
	t.Run("parent error", coveragePostMergeParentError)
	t.Run("root commit", coveragePostMergeRootCommit)
	t.Run("previous HEAD error", coveragePostMergePreviousHeadError)
	t.Run("amend parent error", coveragePostMergeAmendError)
	t.Run("commit message error", coveragePostMergeCommitMessageError)
	t.Run("object format error", coveragePostMergeObjectFormatError)
	t.Run("source note error", coveragePostMergeSourceNoteError)
	t.Run("invalid source note", coveragePostMergeInvalidSourceNote)
	t.Run("source parent error", coveragePostMergeSourceParentError)
	t.Run("target patch empty", coveragePostMergeTargetPatchEmpty)
	t.Run("source patch error", coveragePostMergeSourcePatchError)
	t.Run("amend helper errors", coverageAmendHelperErrors)
	t.Run("amend note error", coverageAmendNoteError)
	t.Run("valid source patch", coverageValidSourcePatch)
}

func TestCoverageRebasePendingMoreBranches(t *testing.T) {
	t.Run("HEAD error", coverageRebaseHeadError)
	t.Run("lock error", coverageRebaseLockError)
	t.Run("state error", coverageRebaseStateError)
	t.Run("missing worktree", coverageRebaseMissingWorktree)
	t.Run("blob budget", coverageRebaseBlobBudget)
	t.Run("snapshot error", coverageRebaseSnapshotError)
	t.Run("matcher budget", coverageRebaseMatcherBudget)
	t.Run("hash error", coverageRebaseHashError)
	t.Run("state write error", coverageRebaseStateWriteError)
}

func TestCoverageReferenceAndHeadErrorBranches(t *testing.T) {
	t.Run("reference object format", coverageReferenceObjectFormatError)
	t.Run("reference branch", coverageReferenceBranchError)
	t.Run("reference head", coverageReferenceHeadError)
	t.Run("reference stash", coverageReferenceStashError)
	t.Run("commit advance reflog", coverageCommitAdvanceReflogError)
	t.Run("commit advance note", coverageCommitAdvanceNoteError)
	t.Run("head move lock", coverageHeadMoveLockError)
	t.Run("head move state", coverageHeadMoveStateError)
}

func TestCoverageStashMoveAndDropBranches(t *testing.T) {
	t.Run("move old note error", coverageStashMoveOldNoteError)
	t.Run("move new note error", coverageStashMoveNewNoteError)
	t.Run("move common lock", coverageStashMoveCommonLockError)
	t.Run("move worktree lock", coverageStashMoveWorktreeLockError)
	t.Run("move state error", coverageStashMoveStateError)
	t.Run("move no pending", coverageStashMoveNoPending)
	t.Run("move paths error", coverageStashMovePathsError)
	t.Run("move no matching files", coverageStashMoveNoMatchingFiles)
	t.Run("move note error", coverageStashMoveNoteError)
	t.Run("move ownership error", coverageStashMoveOwnershipError)
	t.Run("move state write", coverageStashMoveStateWriteError)
	t.Run("drop empty", coverageStashDropEmpty)
	t.Run("drop paths error", coverageStashDropPathsError)
	t.Run("drop applied", coverageStashDropApplied)
	t.Run("drop applied error", coverageStashDropAppliedError)
	t.Run("drop common lock", coverageStashDropCommonLockError)
	t.Run("drop ref error", coverageStashDropRefError)
	t.Run("drop missing ref", coverageStashDropMissingRef)
}

func TestCoverageStashApplyBranches(t *testing.T) {
	t.Run("object format", coverageStashApplyObjectFormatError)
	t.Run("invalid ID", coverageStashApplyInvalidID)
	t.Run("common lock", coverageStashApplyCommonLockError)
	t.Run("worktree lock", coverageStashApplyWorktreeLockError)
	t.Run("note read", coverageStashApplyNoteReadError)
	t.Run("missing note", coverageStashApplyMissingNote)
	t.Run("ownership read", coverageStashApplyOwnershipReadError)
	t.Run("invalid note", coverageStashApplyInvalidNote)
	t.Run("state read", coverageStashApplyStateReadError)
	t.Run("HEAD read", coverageStashApplyHeadReadError)
	t.Run("missing worktree", coverageStashApplyMissingWorktree)
	t.Run("projection budget", coverageStashApplyProjectionBudget)
	t.Run("projection error", coverageStashApplyProjectionError)
	t.Run("hash error", coverageStashApplyHashError)
	t.Run("state write", coverageStashApplyStateWriteError)
	t.Run("remove note error", coverageStashApplyRemoveNoteError)
	t.Run("remove ownership error", coverageStashApplyRemoveOwnershipError)
	t.Run("unowned note", coverageStashApplyUnownedNote)
}

func TestCoverageStashHelperBranches(t *testing.T) {
	t.Run("project helper", coverageProjectNoteHelper)
	t.Run("remove note", coverageRemoveStashNoteBranches)
	t.Run("delete read error", coverageDeleteStashReadError)
	t.Run("delete missing", coverageDeleteStashMissing)
	t.Run("delete ownership error", coverageDeleteStashOwnershipError)
	t.Run("delete unowned", coverageDeleteStashUnowned)
	t.Run("delete invalid", coverageDeleteStashInvalid)
	t.Run("delete changed", coverageDeleteStashChanged)
	t.Run("delete remove error", coverageDeleteStashRemoveError)
	t.Run("delete ownership remove error", coverageDeleteStashOwnershipRemoveError)
	t.Run("delete all list error", coverageDeleteAllStashListError)
}

func coverageProjectNoteHelper(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "one\ntwo\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("one\ntwo\n"))
	if err != nil {
		t.Fatal(err)
	}
	file := model.NoteFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if _, err := projectNoteFileWithBudgetAndCache(repo, file, []byte("one\ntwo\n"), nil, nil); err != nil {
		t.Fatal(err)
	}
	cache := newRewriteBlobCache()
	if _, err := projectNoteFileWithBudgetAndCache(repo, file, []byte("one\ntwo\n"), engine.NewMatcherBudget(maxRewriteMatcherCells), cache); err != nil {
		t.Fatal(err)
	}
	if _, err := projectNoteFileWithBudgetAndCache(repo, model.NoteFile{Blob: "bad"}, []byte("one\n"), nil, nil); err == nil {
		t.Fatal("projectNoteFile accepted an invalid blob")
	}
	invalid := file
	invalid.Ranges[0].End = 3
	if _, err := projectNoteFileWithBudgetAndCache(repo, invalid, []byte("one\ntwo\n"), nil, cache); err == nil {
		t.Fatal("projectNoteFile accepted invalid ranges")
	}
	if _, err := projectNoteFileWithBudgetAndCache(repo, file, []byte{0}, nil, cache); err == nil {
		t.Fatal("projectNoteFile accepted binary content")
	}
	budget := engine.NewMatcherBudget(0)
	budgetFile := model.NoteFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if _, err := projectNoteFileWithBudgetAndCache(repo, budgetFile, []byte("new\nlines\n"), budget, cache); err == nil {
		t.Fatal("projectNoteFile accepted an exhausted matcher budget")
	}
	_ = root
}

func coverageRemoveStashNoteBranches(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	if removed, err := removeStashNote(repo, stash, note, false); err != nil || removed {
		t.Fatalf("unowned remove = %t, %v", removed, err)
	}
	writeStashFixtureNote(t, repo, stash, note)
	if removed, err := removeStashNote(repo, stash, note, true); err != nil || !removed {
		t.Fatalf("owned remove = %t, %v", removed, err)
	}
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "notes-remove-error", "")
	if _, err := removeStashNote(fake, stash, note, true); err == nil {
		t.Fatal("removeStashNote accepted a delete failure")
	}
	writeStashFixtureNote(t, repo, stash, note)
	if removed, err := removeStashNote(repo, stash, []byte("different\n"), true); err != nil || removed {
		t.Fatalf("changed remove = %t, %v", removed, err)
	}
}

func coverageDeleteStashReadError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "notes-read-error", stash)
	if _, err := deleteDroppedStashNote(repo, stash); err == nil {
		t.Fatal("deleteDroppedStashNote accepted a read failure")
	}
}

func coverageDeleteStashMissing(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if result, err := deleteDroppedStashNote(repo, stash); err != nil || result.Written != 0 {
		t.Fatalf("missing stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageDeleteStashOwnershipError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "ownership-read-error", "")
	if _, err := deleteDroppedStashNote(fake, stash); err == nil {
		t.Fatal("deleteDroppedStashNote accepted ownership read failure")
	}
}

func coverageDeleteStashUnowned(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	result, err := deleteDroppedStashNote(repo, stash)
	if err != nil || !containsCoverageWarning(result.Warnings, "unowned") {
		t.Fatalf("unowned stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageDeleteStashInvalid(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	raw := []byte("invalid\n")
	if err := repo.WriteNoteRef(stashNotesRef, stash, raw); err != nil {
		t.Fatal(err)
	}
	if err := writeStashOwnership(repo, stash, raw); err != nil {
		t.Fatal(err)
	}
	result, err := deleteDroppedStashNote(repo, stash)
	if err != nil || !containsCoverageWarning(result.Warnings, "invalid") {
		t.Fatalf("invalid stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageDeleteStashChanged(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	raw := append([]byte(" "), note...)
	if err := repo.WriteNoteRef(stashNotesRef, stash, raw); err != nil {
		t.Fatal(err)
	}
	actual, found, err := repo.ReadNoteRef(stashNotesRef, stash)
	if err != nil || !found {
		t.Fatalf("changed note read = %t, %v", found, err)
	}
	if err := writeStashOwnership(repo, stash, actual); err != nil {
		t.Fatal(err)
	}
	result, err := deleteDroppedStashNote(repo, stash)
	if err != nil || !containsCoverageWarning(result.Warnings, "changed") {
		t.Fatalf("changed stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageDeleteStashRemoveError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "notes-remove-error", "")
	if _, err := deleteDroppedStashNote(fake, stash); err == nil {
		t.Fatal("deleteDroppedStashNote accepted a remove failure")
	}
}

func coverageDeleteStashOwnershipRemoveError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "ownership-remove-error", "")
	if _, err := deleteDroppedStashNote(fake, stash); err == nil {
		t.Fatal("deleteDroppedStashNote accepted an ownership remove failure")
	}
}

func coverageDeleteAllStashListError(t *testing.T) {
	root, _, _, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "notes-list-error", "")
	if _, err := deleteAllDroppedStashNotesLocked(repo); err == nil {
		t.Fatal("deleteAllDroppedStashNotesLocked accepted a list failure")
	}
}

func coverageStashApplyObjectFormatError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "object-format-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted object format failure")
	}
}

func coverageStashApplyInvalidID(t *testing.T) {
	root, repo, _, _ := stashCoverageFixture(t)
	if _, err := HandleStashApply(repo, "bad", false); err == nil {
		t.Fatal("HandleStashApply accepted an invalid stash ID")
	}
	_ = root
}

func coverageStashApplyCommonLockError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	held, err := lock.Acquire(filepath.Join(repo.CommonDir, "byline", "notes.lock"), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply acquired a held common lock")
	}
	_ = root
}

func coverageStashApplyWorktreeLockError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply acquired a held worktree lock")
	}
	_ = root
}

func coverageStashApplyNoteReadError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "notes-read-error", stash)
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted a stash note read failure")
	}
}

func coverageStashApplyMissingNote(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	result, err := HandleStashApply(repo, stash, false)
	if err != nil || result.Mapped != 0 {
		t.Fatalf("missing stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageStashApplyOwnershipReadError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	fake := fakeRewriteRepo(t, root, "ownership-read-error", "")
	if _, err := HandleStashApply(fake, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted an ownership read failure")
	}
}

func coverageStashApplyInvalidNote(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if err := repo.WriteNoteRef(stashNotesRef, stash, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted an invalid stash note")
	}
	_ = root
}

func coverageStashApplyStateReadError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted invalid state")
	}
	_ = root
}

func coverageStashApplyHeadReadError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "head-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted a HEAD failure")
	}
}

func coverageStashApplyMissingWorktree(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	blob, _, err := repo.BlobID(stash, coverageFile)
	if err != nil {
		t.Fatal(err)
	}
	note := encodeCoverageNote(t, makeCoverageNoteForPath(blob, "missing.txt"))
	writeStashFixtureNote(t, repo, stash, note)
	if result, err := HandleStashApply(repo, stash, true); err != nil || result.Mapped != 0 {
		t.Fatalf("missing worktree stash = %+v, %v", result, err)
	}
	_ = root
}

func coverageStashApplyProjectionBudget(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "blob-size-budget", "")
	result, err := HandleStashApply(repo, stash, false)
	if err != nil || !containsCoverageWarning(result.Warnings, "rewrite resource budget exceeded") {
		t.Fatalf("stash projection budget = %+v, %v", result, err)
	}
}

func coverageStashApplyProjectionError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	blob, _, err := repo.BlobID(stash, coverageFile)
	if err != nil {
		t.Fatal(err)
	}
	note = encodeCoverageNote(t, makeCoverageNoteForContent(blob, coverageFile, 2))
	writeStashFixtureNote(t, repo, stash, note)
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted an invalid source snapshot")
	}
	_ = root
}

func coverageStashApplyHashError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "hash-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted a hash failure")
	}
}

func coverageStashApplyStateWriteError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "update-ref-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted a state write failure")
	}
}

func coverageStashApplyRemoveNoteError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "notes-remove-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted a note remove failure")
	}
}

func coverageStashApplyRemoveOwnershipError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	writeStashFixtureNote(t, repo, stash, note)
	repo = fakeRewriteRepo(t, root, "ownership-remove-error", "")
	if _, err := HandleStashApply(repo, stash, false); err == nil {
		t.Fatal("HandleStashApply accepted an ownership remove failure")
	}
}

func coverageStashApplyUnownedNote(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	result, err := HandleStashApply(repo, stash, false)
	if err != nil || !containsCoverageWarning(result.Warnings, "changed stash attribution") {
		t.Fatalf("unowned stash note = %+v, %v", result, err)
	}
	_ = root
}

func coverageStashMoveOldNoteError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "notes-read-error", strings.Repeat("a", 40))
	if _, err := handleStashMove(repo, rewrite.RefUpdate{
		Old: strings.Repeat("a", 40), New: stash,
	}); err == nil {
		t.Fatal("handleStashMove accepted an old note read failure")
	}
}

func coverageStashMoveNewNoteError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	old := strings.Repeat("b", 40)
	if err := repo.WriteNoteRef(stashNotesRef, old, note); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", stash)
	if _, err := handleStashMove(fake, rewrite.RefUpdate{Old: old, New: stash}); err == nil {
		t.Fatal("handleStashMove accepted a new note read failure")
	}
}

func coverageStashMoveCommonLockError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.CommonDir = filepath.Join(root, "common-file")
	if err := os.WriteFile(repo.CommonDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove acquired a blocked common lock")
	}
}

func coverageStashMoveWorktreeLockError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if err := store.New(repo.GitDir).WriteState(stashPendingState(t, repo)); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove acquired a held worktree lock")
	}
	_ = root
}

func coverageStashMoveStateError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove accepted invalid state")
	}
	_ = root
}

func coverageStashMoveNoPending(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err != nil {
		t.Fatal(err)
	}
	_ = root
}

func coverageStashMovePathsError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	state := model.NewState()
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: strings.Repeat("b", 40),
		Ranges: []model.Range{{
			Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove accepted a stash path failure")
	}
}

func coverageStashMoveNoMatchingFiles(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	content := []byte("other\n")
	blob, err := repo.HashBytes(content)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.Files["other.txt"] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	if result, err := handleStashMove(repo, rewrite.RefUpdate{New: stash}); err != nil ||
		result.Written != 0 {
		t.Fatalf("unmatched stash paths = %+v, %v", result, err)
	}
	_ = root
}

func coverageStashMoveNoteError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	state := stashPendingState(t, repo)
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-write-error", "")
	if _, err := handleStashMove(fake, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove accepted a stash note write failure")
	}
}

func coverageStashMoveOwnershipError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if err := store.New(repo.GitDir).WriteState(stashPendingState(t, repo)); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "ownership-write-error", "")
	if _, err := handleStashMove(fake, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove accepted an ownership write failure")
	}
}

func coverageStashMoveStateWriteError(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	if err := store.New(repo.GitDir).WriteState(stashPendingState(t, repo)); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "update-ref-error", "")
	if _, err := handleStashMove(fake, rewrite.RefUpdate{New: stash}); err == nil {
		t.Fatal("handleStashMove accepted a state write failure")
	}
}

func coverageStashDropEmpty(t *testing.T) {
	root, repo, _, _ := stashCoverageFixture(t)
	if _, err := handleStashDrop(repo, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashDrop(repo, strings.Repeat("0", 40)); err != nil {
		t.Fatal(err)
	}
	_ = root
}

func coverageStashDropPathsError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	if _, err := handleStashDrop(repo, stash); err == nil {
		t.Fatal("handleStashDrop accepted a stash path failure")
	}
}

func coverageStashDropApplied(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	if err := writeStashOwnership(repo, stash, note); err != nil {
		t.Fatal(err)
	}
	if result, err := handleStashDrop(repo, stash); err != nil || result.Mapped == 0 {
		t.Fatalf("applied stash drop = %+v, %v", result, err)
	}
	_ = root
}

func coverageStashDropAppliedError(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	if err := writeStashOwnership(repo, stash, note); err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := handleStashDrop(repo, stash); err == nil {
		t.Fatal("handleStashDrop accepted a stash apply read failure")
	}
}

func coverageStashDropCommonLockError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "different\n")
	repo.CommonDir = filepath.Join(root, "common-file")
	if err := os.WriteFile(repo.CommonDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStashDrop(repo, stash); err == nil {
		t.Fatal("handleStashDrop acquired a blocked common lock")
	}
}

func coverageStashDropRefError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "ref-error", "")
	write(t, root, coverageFile, "different\n")
	if _, err := handleStashDrop(repo, stash); err == nil {
		t.Fatal("handleStashDrop accepted a stash ref failure")
	}
}

func coverageStashDropMissingRef(t *testing.T) {
	root, repo, stash, note := stashCoverageFixture(t)
	write(t, root, coverageFile, "different\n")
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	if err := writeStashOwnership(repo, stash, note); err != nil {
		t.Fatal(err)
	}
	if result, err := handleStashDrop(repo, stash); err != nil || result.Written != 1 {
		t.Fatalf("missing stash ref cleanup = %+v, %v", result, err)
	}
	_ = root
}

func stashCoverageFixture(t *testing.T) (string, *gitcmd.Repo, string, []byte) {
	t.Helper()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	write(t, root, coverageFile, "pending\n")
	stash := strings.TrimSpace(git(t, root, "stash", "create", "coverage"))
	if !model.ValidObjectID(stash) {
		t.Fatalf("stash object = %q", stash)
	}
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(stash, coverageFile)
	if err != nil || !exists {
		t.Fatalf("stash blob = %q, %t, %v", blob, exists, err)
	}
	ranges, err := engine.UniformRanges(
		[]byte("pending\n"),
		model.Attribution{Author: model.AuthorHuman},
	)
	if err != nil {
		t.Fatal(err)
	}
	note := encodeCoverageNote(t, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {Blob: blob, Ranges: ranges},
		},
	})
	return root, repo, stash, note
}

func writeStashFixtureNote(t *testing.T, repo *gitcmd.Repo, stash string, note []byte) {
	t.Helper()
	if err := repo.WriteNoteRef(stashNotesRef, stash, note); err != nil {
		t.Fatal(err)
	}
	if err := writeStashOwnership(repo, stash, note); err != nil {
		t.Fatal(err)
	}
}

func stashPendingState(t *testing.T, repo *gitcmd.Repo) model.State {
	t.Helper()
	blob, err := repo.HashBytes([]byte("pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	return state
}

func coverageReferenceObjectFormatError(t *testing.T) {
	root := testRepo(t)
	repo := fakeRewriteRepo(t, root, "object-format-error", "")
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(""), "committed"); err == nil {
		t.Fatal("reference transaction accepted object format failure")
	}
}

func coverageReferenceBranchError(t *testing.T) {
	root := testRepo(t)
	repo := fakeRewriteRepo(t, root, "branch-error", "")
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(""), "committed"); err == nil {
		t.Fatal("reference transaction accepted branch failure")
	}
}

func coverageReferenceHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	newCommit := commitWithMessage(t, root, "new")
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	input := old + " " + newCommit + " HEAD\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err == nil {
		t.Fatal("reference transaction accepted HEAD move failure")
	}
}

func coverageReferenceStashError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	newCommit := commitWithMessage(t, root, "new")
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	blob, err := repo.HashBytes([]byte("pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = old
	state.Pending.BaseCommit = old
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	input := strings.Repeat("0", 40) + " " + newCommit + " refs/stash\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err == nil {
		t.Fatal("reference transaction accepted stash move failure")
	}
}

func coverageCommitAdvanceReflogError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	write(t, root, coverageFile, "new\n")
	newCommit := commit(t, root, "new")
	repo := fakeRewriteRepo(t, root, "reflog-error", "")
	if _, err := commitAdvance(repo, rewrite.RefUpdate{Old: old, New: newCommit}); err == nil {
		t.Fatal("commitAdvance accepted a reflog failure")
	}
}

func coverageCommitAdvanceNoteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	write(t, root, coverageFile, "new\n")
	newCommit := commit(t, root, "new")
	repo := fakeRewriteRepo(t, root, "notes-read-error", newCommit)
	if _, err := commitAdvance(repo, rewrite.RefUpdate{Old: old, New: newCommit}); err == nil {
		t.Fatal("commitAdvance accepted a note read failure")
	}
}

func coverageHeadMoveLockError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: strings.Repeat("0", 40)}); err == nil {
		t.Fatal("handleHeadMove acquired a held lock")
	}
}

func coverageHeadMoveStateError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := handleHeadMove(repo, rewrite.RefUpdate{Old: old, New: strings.Repeat("0", 40)}); err == nil {
		t.Fatal("handleHeadMove accepted invalid state")
	}
}

func coverageRebaseHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = filepath.Join(root, "missing")
	if _, err := rebasePending(repo, ""); err == nil {
		t.Fatal("rebasePending accepted a missing HEAD repository")
	}
	_ = commitID
}

func coverageRebaseLockError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := rebasePending(repo, commitID); err == nil {
		t.Fatal("rebasePending acquired a held lock")
	}
}

func coverageRebaseStateError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := rebasePending(repo, commitID); err == nil {
		t.Fatal("rebasePending accepted invalid state")
	}
}

func coverageRebaseMissingWorktree(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	state.Pending.Files["missing.txt"] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if _, err := rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state); err != nil {
		t.Fatal(err)
	}
}

func coverageRebaseBlobBudget(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "blob-size-budget", "")
	blob, err := repo.HashBytes([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	result, err := rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state)
	if err != nil || !containsCoverageWarning(result.Warnings, "rewrite resource budget exceeded") {
		t.Fatalf("rebase blob budget = %+v, %v", result, err)
	}
}

func coverageRebaseSnapshotError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         2,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	_, err = rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state)
	assertCoverageError(t, err, "decode pending snapshot")
}

func coverageRebaseMatcherBudget(t *testing.T) {
	root := testRepo(t)
	const fileCount = 5
	source := matcherContent("", 0, "old", 1000)
	target := matcherContent("", 0, "new", 1000)
	for index := 0; index < fileCount; index++ {
		write(t, root, rebaseCoveragePath(index), source)
	}
	commitID := commit(t, root, "content")
	for index := 0; index < fileCount; index++ {
		write(t, root, rebaseCoveragePath(index), target)
	}
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	for index := 0; index < fileCount; index++ {
		blob, hashErr := repo.HashBytes([]byte(source))
		if hashErr != nil {
			t.Fatal(hashErr)
		}
		state.Pending.Files[rebaseCoveragePath(index)] = model.PendingFile{
			Blob: blob,
			Ranges: []model.Range{{
				Start:       1,
				End:         1000,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}},
		}
	}
	result, err := rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state)
	if err != nil || !containsCoverageWarning(result.Warnings, "line matcher budget exceeded") {
		t.Fatalf("rebase matcher budget = %+v, %v", result, err)
	}
}

func rebaseCoveragePath(index int) string {
	return "rebase-" + string(rune('a'+index)) + ".txt"
}

func coverageRebaseHashError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "hash-error", "")
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	_, err = rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state)
	if err == nil {
		t.Fatal("rebasePendingLocked accepted a hash failure")
	}
}

func coverageRebaseStateWriteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("content\n"))
	if err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "update-ref-error", "")
	state := model.NewState()
	state.Pending.BaseCommit = commitID
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start:       1,
			End:         1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	_, err = rebasePendingLocked(repo, commitID, store.New(repo.GitDir), state)
	assertCoverageError(t, err, "write checkout state")
}

func coveragePostMergeHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := HandlePostMerge(repo); err == nil {
		t.Fatal("HandlePostMerge accepted a HEAD failure")
	}
}

func coveragePostMergeUnborn(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil || result.Mapped != 0 || result.Written != 0 {
		t.Fatalf("unborn post merge = %+v, %v", result, err)
	}
}

func coveragePostMergeParentError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	if _, err := HandlePostMerge(repo); err == nil {
		t.Fatal("HandlePostMerge accepted a parent failure")
	}
}

func coveragePostMergeRootCommit(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "root")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil || result.Mapped != 0 || result.Written != 0 {
		t.Fatalf("root post merge = %+v, %v", result, err)
	}
}

func coveragePostMergePreviousHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "base")
	commitWithMessage(t, root, "target")
	repo := fakeRewriteRepo(t, root, "previous-head-error", "")
	if _, err := HandlePostMerge(repo); err == nil {
		t.Fatal("HandlePostMerge accepted a previous HEAD failure")
	}
}

func coveragePostMergeAmendError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "base")
	commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := repo.HashBytes([]byte("blob\n"))
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "previous-head-blob", blobID)
	if _, err := HandlePostMerge(fake); err == nil {
		t.Fatal("HandlePostMerge accepted an invalid previous HEAD")
	}
}

func coveragePostMergeCommitMessageError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "base")
	commitWithMessage(t, root, "target")
	repo := fakeRewriteRepo(t, root, "commit-message-error", "")
	if _, err := HandlePostMerge(repo); err == nil {
		t.Fatal("HandlePostMerge accepted a commit message failure")
	}
}

func coveragePostMergeObjectFormatError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "base")
	commitWithMessage(t, root, "(cherry picked from commit "+strings.Repeat("a", 40)+")")
	repo := fakeRewriteRepo(t, root, "object-format-error", "")
	if _, err := HandlePostMerge(repo); err == nil {
		t.Fatal("HandlePostMerge accepted an object format failure")
	}
}

func coveragePostMergeSourceNoteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	git(t, root, "checkout", "-q", "-b", "side")
	write(t, root, coverageFile, "source\n")
	source := commit(t, root, "source")
	git(t, root, "checkout", "-q", "main")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "(cherry picked from commit "+source+")")
	repo := fakeRewriteRepo(t, root, "notes-read-error", source)
	result, err := HandlePostMerge(repo)
	if err != nil || !containsCoverageWarning(result.Warnings, "skipped cherry-pick marker") {
		t.Fatalf("source note error = %+v, %v", result, err)
	}
	_ = target
	_ = base
}

func coveragePostMergeInvalidSourceNote(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	source := commit(t, root, "source")
	target := commitWithMessage(t, root, "(cherry picked from commit "+source+")")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(source, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil || !containsCoverageWarning(result.Warnings, "invalid source note") {
		t.Fatalf("invalid source note = %+v, %v", result, err)
	}
	_ = target
}

func coveragePostMergeSourceParentError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	base := commit(t, root, "base")
	source := commitWithMessage(t, root, "source")
	target := commitWithMessage(t, root, "(cherry picked from commit "+strings.Repeat("a", 40)+")")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("blob\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(blob, encodeCoverageNote(t, model.Note{Version: model.NoteVersion})); err != nil {
		t.Fatal(err)
	}
	if _, warning, err := validCherryPickSource(repo, blob, target); err == nil || warning != "" {
		t.Fatalf("source parent error = %q, %v", warning, err)
	}
	_ = base
	_ = source
}

func coveragePostMergeTargetPatchEmpty(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	source := commit(t, root, "source")
	target := commitWithMessage(t, root, "(cherry picked from commit "+source+")")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(source, encodeCoverageNote(t, model.Note{Version: model.NoteVersion})); err != nil {
		t.Fatal(err)
	}
	result, err := HandlePostMerge(repo)
	if err != nil || !containsCoverageWarning(result.Warnings, "without target patch") {
		t.Fatalf("target patch empty = %+v, %v", result, err)
	}
	_ = target
}

func coveragePostMergeSourcePatchError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	git(t, root, "checkout", "-q", "-b", "side")
	write(t, root, coverageFile, "side\n")
	commit(t, root, "side base")
	source := commitWithMessage(t, root, "source")
	git(t, root, "checkout", "-q", "main")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "(cherry picked from commit "+source+")")
	repo := fakeRewriteRepo(t, root, "source-patch-error", source)
	if err := repo.WriteNote(source, encodeCoverageNote(t, model.Note{Version: model.NoteVersion})); err != nil {
		t.Fatal(err)
	}
	valid, warning, err := validCherryPickSource(repo, source, target)
	if err != nil || valid || !strings.Contains(warning, "without source patch") {
		t.Fatalf("source patch error = %t, %q, %v", valid, warning, err)
	}
	_ = base
}

func coverageAmendHelperErrors(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	write(t, root, coverageFile, "previous\n")
	previous := commit(t, root, "previous")
	write(t, root, coverageFile, "target\n")
	git(t, root, "commit", "--amend", "-am", "target")
	target := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("blob\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := amendMapping(repo, blob, target); err == nil {
		t.Fatal("amendMapping accepted a blob previous commit")
	}
	if _, _, err := amendMapping(repo, previous, blob); err == nil {
		t.Fatal("amendMapping accepted a blob target commit")
	}
	if _, found, err := amendMapping(repo, previous, target); err != nil || found {
		t.Fatalf("amendMapping without note = %t, %v", found, err)
	}
	if err := repo.WriteNote(previous, encodeCoverageNote(t, model.Note{Version: model.NoteVersion})); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "", "")
	t.Setenv("FAKE_GIT_MODE", "object-format-error")
	if length, err := fake.ObjectIDLength(); err == nil || length != 0 {
		t.Fatalf("fake object format = %d, %v", length, err)
	}
	if _, _, err := amendMapping(fake, previous, target); err == nil {
		t.Fatal("amendMapping accepted an object format failure")
	}
}

func coverageAmendNoteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	git(t, root, "checkout", "-q", "-b", "side")
	write(t, root, coverageFile, "old\n")
	old := commit(t, root, "old")
	git(t, root, "checkout", "-q", "main")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", old)
	if _, _, err := amendMapping(fake, old, target); err == nil {
		t.Fatal("amendMapping accepted a note read failure")
	}
}

func coverageValidSourcePatch(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	git(t, root, "checkout", "-q", "-b", "side")
	write(t, root, "source.txt", "side\n")
	source := commit(t, root, "source")
	git(t, root, "checkout", "-q", "main")
	write(t, root, "main.txt", "main\n")
	commit(t, root, "main advance")
	git(t, root, "cherry-pick", source)
	target := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	if source == target {
		t.Fatalf("cherry-pick did not create a new commit: %s", source)
	}
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, source, coverageFile)
	if err := repo.WriteNote(source, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	valid, warning, err := validCherryPickSource(repo, source, target)
	if err != nil || !valid || warning != "" {
		t.Fatalf("valid source patch = %t, %q, %v", valid, warning, err)
	}
}

func coverageApplyNoteCacheBudget(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	oldOne := commit(t, root, "old one")
	oldTwo := commitWithMessage(t, root, "old two")
	targetOne := commitWithMessage(t, root, "target one")
	targetTwo := commitWithMessage(t, root, "target two")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(oldOne, encodeCoverageNote(t, makeCoverageNote(mustBlob(t, repo, oldOne, coverageFile)))); err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "large-note", oldTwo)
	mapping, err := rewrite.NewMapping([]rewrite.Pair{
		{Old: oldOne, New: targetOne},
		{Old: oldTwo, New: targetTwo},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "note budget") {
		t.Fatalf("note budget result = %+v", result)
	}
}

func coverageApplyNoteCacheExceeded(t *testing.T) {
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
	repo = fakeRewriteRepo(t, root, "large-note", oldTwo)
	mapping, err := rewrite.NewMapping([]rewrite.Pair{
		{Old: oldOne, New: targetOne},
		{Old: oldTwo, New: targetTwo},
		{Old: oldTwo, New: targetThree},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, mapping)
	if err != nil || !containsCoverageWarning(result.Warnings, "note budget exceeded") {
		t.Fatalf("cached note budget = %+v, %v", result, err)
	}
}

func coverageApplyInvalidSourceNote(t *testing.T) {
	_, repo, old, target := rewriteCoverageRepo(t)
	if err := repo.WriteNote(old, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	_, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "decode old attribution note")
}

func coverageApplySourceBlobBudget(t *testing.T) {
	root := testRepo(t)
	content := []byte(strings.Repeat("x", maxRewriteBlobBytes+1))
	content = append(content, '\n')
	write(t, root, coverageFile, string(content))
	old := commit(t, root, "old")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "rewrite resource budget exceeded") {
		t.Fatalf("source blob budget result = %+v", result)
	}
}

func coverageApplySourceSnapshotError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "one\n")
	old := commit(t, root, "old")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1,
					End:   2,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			},
		},
	}
	if err := repo.WriteNote(old, encodeCoverageNote(t, note)); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "decode source snapshot")
}

func coverageApplyRewrittenDirectoryError(t *testing.T) {
	root := testRepo(t)
	write(t, root, "dir", "file\n")
	old := commit(t, root, "old")
	if err := os.Remove(filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "dir/file", strings.Repeat("different\n", 20))
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, "dir")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForPath(blob, "dir"))); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "map rewritten path")
}

func coverageApplyDeletedPath(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	if err := os.Remove(filepath.Join(root, coverageFile)); err != nil {
		t.Fatal(err)
	}
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 {
		t.Fatalf("deleted path result = %+v", result)
	}
}

func coverageApplyRewrittenContentError(t *testing.T) {
	root := testRepo(t)
	write(t, root, "old.txt", strings.Repeat("same\n", 19)+"old\n")
	old := commit(t, root, "old")
	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", strings.Repeat("same\n", 19)+"new\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceBlob := mustBlob(t, repo, old, "old.txt")
	targetBlob := mustBlob(t, repo, target, "new.txt")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForContent(sourceBlob, "old.txt", 20))); err != nil {
		t.Fatal(err)
	}
	if err := removeLooseObject(repo, targetBlob); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "read rewritten content")
}

func coverageApplyRewrittenSizeBudget(t *testing.T) {
	root := testRepo(t)
	sourceContent := strings.Repeat("same\n", 20) + "old\n"
	write(t, root, coverageFile, sourceContent)
	old := commit(t, root, "old")
	if err := os.Rename(filepath.Join(root, coverageFile), filepath.Join(root, "target.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "target.txt", strings.Repeat("same\n", 20)+"target\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceBlob := mustBlob(t, repo, old, coverageFile)
	targetBlob := mustBlob(t, repo, target, "target.txt")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForContent(sourceBlob, coverageFile, 21))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "target-size-budget", targetBlob)
	result, err := applyRewriteMapping(fake, coverageMapping(t, old, target))
	if err != nil || !containsCoverageWarning(result.Warnings, "rewrite resource budget exceeded") {
		t.Fatalf("rewritten size budget = %+v, %v", result, err)
	}
}

func coverageApplyBinaryRewrittenContent(t *testing.T) {
	root := testRepo(t)
	sourceContent := strings.Repeat("same\n", 20) + "old\n"
	write(t, root, "old.txt", sourceContent)
	old := commit(t, root, "old")
	if err := os.Remove(filepath.Join(root, "old.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", strings.Repeat("same\n", 19)+"binary\x00\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceBlob := mustBlob(t, repo, old, "old.txt")
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNoteForContent(sourceBlob, "old.txt", 21))); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	if err == nil {
		t.Fatal("applyRewriteMapping accepted a binary rewritten blob")
	}
}

func coverageApplyMatcherBudget(t *testing.T) {
	root := testRepo(t)
	const fileCount = 5
	for index := 0; index < fileCount; index++ {
		write(t, root, coveragePath(index), matcherContent("same-"+string(rune('a'+index)), 1500, "old-"+string(rune('a'+index)), 1000))
	}
	old := commit(t, root, "old")
	for index := 0; index < fileCount; index++ {
		path := coveragePath(index)
		if err := os.Rename(filepath.Join(root, path), filepath.Join(root, "new-"+path)); err != nil {
			t.Fatal(err)
		}
	}
	for index := 0; index < fileCount; index++ {
		write(t, root, "new-"+coveragePath(index), matcherContent("same-"+string(rune('a'+index)), 1500, "new-"+string(rune('a'+index)), 1000))
	}
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]model.NoteFile, fileCount)
	for index := 0; index < fileCount; index++ {
		path := coveragePath(index)
		files[path] = model.NoteFile{
			Blob: mustBlob(t, repo, old, path),
			Ranges: []model.Range{{
				Start: 1,
				End:   2500,
				Attribution: model.Attribution{
					Author: model.AuthorHuman,
				},
			}},
		}
	}
	if err := repo.WriteNote(old, encodeCoverageNote(t, model.Note{
		Version: model.NoteVersion,
		Files:   files,
	})); err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "line matcher budget") {
		t.Fatalf("matcher budget result = %+v", result)
	}
}

func coverageApplyExistingNoteBudget(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "large-note", target)
	result, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "note budget") {
		t.Fatalf("existing note budget result = %+v", result)
	}
}

func coverageApplyReadTargetNoteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	target := commitWithMessage(t, root, "target")
	repo := fakeRewriteRepo(t, root, "notes-read-error", target)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	_, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "read rewritten note")
}

func coverageApplyWriteTargetNoteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	old := commit(t, root, "old")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "notes-write-error", "")
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "write rewritten note")
}

func coverageApplySessionConflict(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	oldOne := commit(t, root, "old one")
	oldTwo := commitWithMessage(t, root, "old two")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	key := model.NoteSessionKey("agent", "session")
	first := model.Note{
		Version: model.NoteVersion,
		Sessions: map[string]model.NoteSession{
			key: {Agent: "agent", Model: "one"},
		},
	}
	second := model.Note{
		Version: model.NoteVersion,
		Sessions: map[string]model.NoteSession{
			key: {Agent: "agent", Model: "two"},
		},
	}
	if err := repo.WriteNote(oldOne, encodeCoverageNote(t, first)); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(oldTwo, encodeCoverageNote(t, second)); err != nil {
		t.Fatal(err)
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{
		{Old: oldOne, New: target},
		{Old: oldTwo, New: target},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, mapping)
	assertCoverageError(t, err, "conflicting metadata")
}

func coverageApplyHeadError(t *testing.T) {
	root, repo, _, _ := rewriteCoverageRepo(t)
	repo.Root = filepath.Join(root, "missing")
	_, err := applyRewriteMapping(repo, rewrite.Mapping{})
	if err == nil {
		t.Fatal("applyRewriteMapping accepted a missing HEAD repository")
	}
}

func coverageApplyHeadNoteError(t *testing.T) {
	root, repo, old, target := rewriteCoverageRepo(t)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", target)
	if _, err := applyRewriteMapping(fake, coverageMapping(t, old, target)); err == nil {
		t.Fatal("applyRewriteMapping accepted a rewritten HEAD note failure")
	}
}

func coverageApplyAncestorRemap(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "previous")
	write(t, root, coverageFile, "rewritten\n")
	oldBoundary := commit(t, root, "old boundary")
	unmapped := commitWithMessage(t, root, "unmapped")
	target := commitWithMessage(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, oldBoundary, coverageFile)
	if err := repo.WriteNote(oldBoundary, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = unmapped
	state.Pending.BaseCommit = unmapped
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{Old: oldBoundary, New: target}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written == 0 {
		t.Fatalf("ancestor remap result = %+v", result)
	}
}

func coverageApplyZeroBoundaryParentError(t *testing.T) {
	_, repo, _, _ := rewriteCoverageRepo(t)
	bad, err := repo.HashBytes([]byte("not a commit\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = bad
	state.Pending.BaseCommit = bad
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{
		Old: bad,
		New: strings.Repeat("0", 40),
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, mapping)
	if err == nil {
		t.Fatal("applyRewriteMapping accepted a zero mapping from a non-commit boundary")
	}
}

func coverageApplyAncestorParentError(t *testing.T) {
	_, repo, _, _ := rewriteCoverageRepo(t)
	bad, err := repo.HashBytes([]byte("not a commit\n"))
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = bad
	state.Pending.BaseCommit = bad
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, rewrite.Mapping{})
	if err == nil {
		t.Fatal("applyRewriteMapping accepted a broken state boundary")
	}
}

func coverageApplyStateWriteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "update-ref-error", "")
	_, err := applyRewriteMapping(repo, rewrite.Mapping{})
	assertCoverageError(t, err, "write remapped state")
}

func largeRewriteNote(size int) []byte {
	base := []byte(`{"version":3,"files":{},"sessions":{}}`)
	if size <= len(base) {
		return base
	}
	return append(base, bytes.Repeat([]byte(" "), size-len(base))...)
}

func matcherContent(commonPrefix string, common int, changedPrefix string, changed int) string {
	var builder strings.Builder
	for index := 0; index < common; index++ {
		builder.WriteString(commonPrefix)
		builder.WriteByte('\n')
	}
	for index := 0; index < changed; index++ {
		builder.WriteString(changedPrefix)
		builder.WriteString("-")
		builder.WriteString(string(rune('a' + index%26)))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func coveragePath(index int) string {
	return "file-" + string(rune('a'+index)) + ".txt"
}

func containsCoverageWarning(warnings []string, text string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, text) {
			return true
		}
	}
	return false
}

func makeCoverageNoteForContent(blob, path string, lines int) model.Note {
	return model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			path: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1,
					End:   lines,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			},
		},
	}
}

func fakeRewriteRepo(t *testing.T, root, mode, failCommit string) *gitcmd.Repo {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	script := filepath.Join(binDir, "git")
	const source = `#!/bin/sh
if [ "$FAKE_GIT_MODE" = "notes-read-error" ] &&
   [ "$1" = "notes" ] && [ "$3" = "show" ] &&
   [ "$4" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  printf '%s\n' 'forced notes read failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "notes-write-error" ] &&
   [ "$1" = "notes" ] && [ "$3" = "add" ]; then
  printf '%s\n' 'forced notes write failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "ownership-write-error" ] &&
   [ "$1" = "notes" ] && [ "$2" = "--ref=refs/notes/byline-stash-owner" ] &&
   [ "$3" = "add" ]; then
  printf '%s\n' 'forced ownership write failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "ownership-read-error" ] &&
   [ "$1" = "notes" ] && [ "$2" = "--ref=refs/notes/byline-stash-owner" ] &&
   [ "$3" = "show" ]; then
  printf '%s\n' 'forced ownership read failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "notes-remove-error" ] &&
   [ "$1" = "notes" ] && [ "$3" = "remove" ]; then
  printf '%s\n' 'forced notes remove failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "notes-remove-missing" ] &&
   [ "$1" = "notes" ] && [ "$3" = "remove" ]; then
  exit 1
fi
if [ "$FAKE_GIT_MODE" = "notes-remove-race" ] &&
   [ "$1" = "notes" ] && [ "$2" = "--ref=refs/notes/byline-stash" ] &&
   [ "$3" = "show" ]; then
  if [ -f "$FAKE_GIT_COUNTER" ]; then
    printf '%s\n' 'different note'
    exit 0
  fi
  : > "$FAKE_GIT_COUNTER"
fi
if [ "$FAKE_GIT_MODE" = "ownership-remove-error" ] &&
   [ "$1" = "notes" ] && [ "$2" = "--ref=refs/notes/byline-stash-owner" ] &&
   [ "$3" = "remove" ]; then
  printf '%s\n' 'forced ownership remove failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "notes-list-error" ] &&
   [ "$1" = "notes" ] && [ "$3" = "list" ]; then
  printf '%s\n' 'forced notes list failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "large-note" ] &&
   [ "$1" = "notes" ] && [ "$3" = "show" ] &&
   [ "$4" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  dd if=/dev/zero bs=16777200 count=1 2>/dev/null
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "object-format-error" ] &&
   [ "$1" = "rev-parse" ] && [ "$2" = "--show-object-format=storage" ]; then
  printf '%s\n' 'unknown-format'
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "previous-head-error" ] &&
   [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ] &&
   [ "$3" = "HEAD@{1}" ]; then
  printf '%s\n' 'not-an-object'
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "previous-head-blob" ] &&
   [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ] &&
   [ "$3" = "HEAD@{1}" ]; then
  printf '%s\n' "$FAKE_GIT_FAIL_COMMIT"
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "reflog-error" ] &&
   [ "$1" = "log" ] && [ "$2" = "-g" ]; then
  printf '%s\n' 'forced reflog failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "commit-message-error" ] &&
   [ "$1" = "show" ] && [ "$2" = "-s" ]; then
  printf '%s\n' 'forced commit message failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "patch-error" ] &&
   [ "$1" = "diff-tree" ]; then
  printf '%s\n' 'forced patch failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "blob-size-budget" ] &&
   [ "$1" = "cat-file" ] && [ "$2" = "-s" ]; then
  printf '%s\n' '16777217'
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "blob-size-error" ] &&
   [ "$1" = "cat-file" ] && [ "$2" = "-s" ]; then
  printf '%s\n' 'forced blob size failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "content-size-budget" ] &&
   [ "$1" = "cat-file" ] && [ "$2" = "blob" ]; then
  head -c 67108865 /dev/zero
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "target-size-budget" ] &&
   [ "$1" = "cat-file" ] && [ "$2" = "-s" ] &&
   [ "$3" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  printf '%s\n' '16777217'
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "hash-error" ] &&
   [ "$1" = "hash-object" ]; then
  printf '%s\n' 'forced hash failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "read-blob-error" ] &&
   [ "$1" = "cat-file" ] && [ "$2" = "blob" ]; then
  printf '%s\n' 'forced blob read failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "ls-tree-error" ] &&
   [ "$1" = "ls-tree" ]; then
  printf '%s\n' 'forced tree read failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "target-blob-error" ] &&
   [ "$1" = "ls-tree" ] && [ "$3" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  printf '%s\n' 'forced target tree read failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "target-blob-missing" ] &&
   [ "$1" = "ls-tree" ] && [ "$3" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "dirty-error" ] &&
   [ "$1" = "-c" ] && [ "$3" = "status" ]; then
  printf '%s\n' 'forced dirty path failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "author-error" ] &&
   [ "$1" = "show" ] && [ "$2" = "-s" ] &&
   [ "$3" = "--format=%an%x00%ae" ]; then
  printf '%s\n' 'forced author failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "protected-error" ] &&
   [ "$1" = "ls-tree" ] && [ "$2" = "-r" ]; then
  printf '%s\n' 'forced protected blob failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "branch-error" ] &&
   [ "$1" = "symbolic-ref" ]; then
  printf '%s\n' 'forced branch failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "ref-error" ] &&
   [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ] &&
   [ "$3" = "refs/stash" ]; then
  printf '%s\n' 'forced ref failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "update-ref-error" ] &&
   [ "$1" = "update-ref" ]; then
  printf '%s\n' 'forced update-ref failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "rewrite-state-write-error" ] &&
   [ "$1" = "update-ref" ] &&
   [ -n "$FAKE_GIT_STATE_PATH" ]; then
  rm -f "$FAKE_GIT_STATE_PATH"
  mkdir "$FAKE_GIT_STATE_PATH"
fi
if [ "$FAKE_GIT_MODE" = "update-ref-second-error" ] &&
   [ "$1" = "update-ref" ]; then
  if [ -f "$FAKE_GIT_COUNTER" ]; then
    printf '%s\n' 'forced second update-ref failure' >&2
    exit 2
  fi
  : > "$FAKE_GIT_COUNTER"
fi
if [ "$FAKE_GIT_MODE" = "state-write-error" ] &&
   [ "$1" = "diff-tree" ] &&
   [ -n "$FAKE_GIT_STATE_PATH" ]; then
  rm -f "$FAKE_GIT_STATE_PATH"
  mkdir "$FAKE_GIT_STATE_PATH"
fi
if [ "$FAKE_GIT_MODE" = "head-error" ] &&
   [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ] &&
   [ "$3" = "HEAD" ]; then
  printf '%s\n' 'forced HEAD failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "parents-error" ] &&
   [ "$1" = "rev-list" ] && [ "$2" = "--parents" ]; then
  printf '%s\n' 'forced parent failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "source-patch-error" ] &&
   [ "$1" = "diff-tree" ] && [ "$7" = "$FAKE_GIT_FAIL_COMMIT" ]; then
  printf '%s\n' 'forced source patch failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "source-patch-error" ] &&
   [ "$1" = "patch-id" ]; then
  printf '%s\n' '1111111111111111111111111111111111111111  -'
  exit 0
fi
if [ "$FAKE_GIT_MODE" = "changes-error" ] &&
   [ "$1" = "diff-tree" ]; then
  printf '%s\n' 'forced changes failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "any-branch-error" ] &&
   [ "$1" = "for-each-ref" ]; then
  printf '%s\n' 'forced branch containment failure' >&2
  exit 2
fi
if [ "$FAKE_GIT_MODE" = "drop-write-error" ] &&
   [ "$1" = "for-each-ref" ] &&
   [ -n "$FAKE_GIT_CHECKPOINT_PATH" ]; then
  rm -f "$FAKE_GIT_CHECKPOINT_PATH"
  mkdir "$FAKE_GIT_CHECKPOINT_PATH"
fi
exec "$FAKE_GIT_REAL" "$@"
`
	if err := os.WriteFile(script, []byte(source), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_GIT_REAL", realGit)
	t.Setenv("FAKE_GIT_MODE", mode)
	t.Setenv("FAKE_GIT_FAIL_COMMIT", failCommit)
	t.Setenv("FAKE_GIT_COUNTER", filepath.Join(t.TempDir(), "counter"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
