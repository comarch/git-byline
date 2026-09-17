package provenance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/store"
)

func TestCoverageAnnotateBranches(t *testing.T) {
	t.Run("operation lock", coverageAnnotateOperationLock)
	t.Run("common lock", coverageAnnotateCommonLock)
	t.Run("HEAD error", coverageAnnotateHeadError)
	t.Run("current branch error", coverageAnnotateBranchError)
	t.Run("unborn", coverageAnnotateUnborn)
	t.Run("reflog error", coverageAnnotateReflogError)
	t.Run("state error", coverageAnnotateStateError)
	t.Run("checkpoint error", coverageAnnotateCheckpointError)
	t.Run("require note error", coverageAnnotateRequireNoteError)
	t.Run("noop protect error", coverageAnnotateNoopProtectError)
	t.Run("parent error", coverageAnnotateParentError)
	t.Run("divergent history", coverageAnnotateDivergentHistory)
	t.Run("changes error", coverageAnnotateChangesError)
	t.Run("initial history", coverageAnnotateInitialHistory)
	t.Run("identity error", coverageAnnotateIdentityError)
	t.Run("transition error", coverageAnnotateTransitionError)
	t.Run("replay error", coverageAnnotateReplayError)
	t.Run("project error", coverageAnnotateProjectError)
	t.Run("worktree read warning", coverageAnnotateWorktreeReadWarning)
	t.Run("missing worktree", coverageAnnotateMissingWorktree)
	t.Run("protect error", coverageAnnotateProtectError)
	t.Run("note write error", coverageAnnotateNoteWriteError)
	t.Run("state write error", coverageAnnotateStateWriteError)
	t.Run("final protect error", coverageAnnotateFinalProtectError)
	t.Run("drop non-stranded error", coverageDropNonStrandedError)
	t.Run("drop lock error", coverageDropLockError)
	t.Run("drop checkpoint error", coverageDropCheckpointError)
	t.Run("drop state error", coverageDropStateError)
	t.Run("drop HEAD error", coverageDropHeadError)
	t.Run("drop branch error", coverageDropBranchError)
	t.Run("drop parent error", coverageDropParentError)
	t.Run("drop reachability error", coverageDropReachabilityError)
	t.Run("drop skipped record", coverageDropSkippedRecord)
	t.Run("drop retry error", coverageDropRetryError)
	t.Run("drop write error", coverageDropWriteError)
}

func TestCoverageRemainingBlameAndStatusBranches(t *testing.T) {
	t.Run("HEAD note read error", coverageHeadNoteReadError)
	t.Run("working directory resolution error", coverageBlameWorkingDirectoryError)
	t.Run("head file resolution error", coverageBlameHeadFileResolutionError)
	t.Run("head file render error", coverageBlameHeadFileRenderError)
	t.Run("collection file limit", coverageBlameCollectionFileLimit)
	t.Run("absolute path error", coverageBlameAbsolutePathError)
	t.Run("multiple candidates", coverageBlameMultipleCandidates)
	t.Run("head file preflight error", coverageBlameHeadFilePreflightError)
	t.Run("blob size error", coverageBlameBlobSizeError)
	t.Run("Blame HEAD error", coverageBlameHeadError)
	t.Run("Blame note error", coverageBlameNoteError)
	t.Run("Blame invalid ranges", coverageBlameInvalidRanges)
	t.Run("pending status count", coverageStatusPendingCount)
}

func TestCoverageAnnotateTargetBranches(t *testing.T) {
	t.Run("initial snapshot error", coverageAnnotateInitialSnapshotError)
	t.Run("target blob error", coverageAnnotateTargetBlobError)
	t.Run("target path missing", coverageAnnotateTargetPathMissing)
	t.Run("content output limit", coverageAnnotateContentOutputLimit)
	t.Run("content read error", coverageAnnotateContentReadError)
	t.Run("carry transition error", coverageAnnotateCarryTransitionError)
	t.Run("carry replay error", coverageAnnotateCarryReplayError)
	t.Run("pending hash error", coverageAnnotatePendingHashError)
	t.Run("note encode error", coverageAnnotateNoteEncodeError)
}

func coverageAnnotateInitialSnapshotError(t *testing.T) {
	root, repo, base, head := annotateCoverageRepo(t)
	blob := mustBlob(t, repo, base, coverageFile)
	pendingBlob := strings.Repeat("a", 40)
	if err := repo.WriteNote(base, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := `{"version":1,"last_annotated_commit":"` + base +
		`","last_checkpoint_seq":0,"notes_version":3,"pending":{"base_commit":"` +
		base + `","files":{"` + coverageFile + `":{"blob":"` + pendingBlob +
		`","ranges":[{"start":1,"end":1,"author":"human"}]}}}}` + "\n"
	if err := os.WriteFile(dataStore.StatePath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Annotate(repo)
	if err == nil {
		t.Fatal("Annotate accepted invalid pending ranges")
	}
	_ = root
	_ = head
}

func coverageAnnotateTargetBlobError(t *testing.T) {
	root, _, _, head := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "target-blob-error", head)
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a target blob lookup failure")
	}
}

func coverageAnnotateTargetPathMissing(t *testing.T) {
	root, _, _, head := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "target-blob-missing", head)
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a missing changed path")
	}
}

func coverageAnnotateContentOutputLimit(t *testing.T) {
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	commit(t, root, "base")
	write(t, root, "new.txt", "new\n")
	commit(t, root, "new")
	repo := fakeRewriteRepo(t, root, "content-size-budget", "")
	result, err := Annotate(repo)
	if err != nil || !containsCoverageWarning(result.Warnings, "skipped unsupported path") {
		t.Fatalf("Annotate output budget = %+v, %v", result, err)
	}
}

func coverageAnnotateContentReadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	commit(t, root, "base")
	write(t, root, "new.txt", "new\n")
	commit(t, root, "new")
	repo := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a target blob read failure")
	}
}

func coverageAnnotateCarryTransitionError(t *testing.T) {
	_, repo, _, head := annotateCoverageRepo(t)
	if err := store.New(repo.GitDir).AppendCheckpoint(coverageAnnotateRecord(head, strings.Repeat("a", 40))); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a carried transition blob failure")
	}
}

func coverageAnnotateCarryReplayError(t *testing.T) {
	_, repo, _, head := annotateCoverageRepo(t)
	blob, err := repo.HashBytes([]byte("binary\x00\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(repo.GitDir).AppendCheckpoint(coverageAnnotateRecord(head, blob)); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a binary carried transition")
	}
}

func coverageAnnotatePendingHashError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	write(t, root, coverageFile, "pending\n")
	repo := fakeRewriteRepo(t, root, "hash-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a pending hash failure")
	}
}

func coverageAnnotateNoteEncodeError(t *testing.T) {
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	commit(t, root, "base")
	for index := 0; index < 501; index++ {
		write(t, root, fmt.Sprintf("annotate-%03d.txt", index), "content\n")
	}
	commit(t, root, "many files")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "more than 500") {
		t.Fatalf("Annotate accepted too many note files: %v", err)
	}
}

func coverageHeadNoteReadError(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", head)
	if _, _, err := headNote(fake); err == nil {
		t.Fatal("headNote accepted a note read failure")
	}
}

func coverageBlameWorkingDirectoryError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a directory the process is inside")
	}
	root, repo := blameResolveRepo(t)
	removed := filepath.Join(t.TempDir(), "removed")
	if err := os.Mkdir(removed, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(removed)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	if _, err := blameCandidates(repo, coverageFile); err == nil {
		t.Fatal("blameCandidates accepted a deleted working directory")
	}
	_ = root
}

func coverageBlameHeadFileResolutionError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a directory the process is inside")
	}
	root, repo := blameNoteRepo(t, "content\n")
	removed := filepath.Join(t.TempDir(), "removed")
	if err := os.Mkdir(removed, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(removed)
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHeadFile(repo, coverageFile); err == nil {
		t.Fatal("BlameHeadFile accepted a deleted working directory")
	}
	_ = root
}

func coverageBlameHeadFileRenderError(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := BlameHeadFile(fake, coverageFile); err == nil {
		t.Fatal("BlameHeadFile accepted a blob read failure")
	}
	_ = blob
}

func coverageBlameCollectionFileLimit(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]model.NoteFile, maxBlameCollectionFiles+1)
	for index := 0; index <= maxBlameCollectionFiles; index++ {
		files[fmt.Sprintf("file-%03d.txt", index)] = model.NoteFile{}
	}
	data, err := json.Marshal(model.Note{Version: model.NoteVersion, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteNoteRef("refs/notes/byline", head); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil {
		t.Fatal("BlameHead accepted too many note files")
	}
	_ = root
}

func coverageBlameAbsolutePathError(t *testing.T) {
	_, repo := blameResolveRepo(t)
	if _, err := blameCandidates(repo, "/outside"); err == nil {
		t.Fatal("blameCandidates accepted an outside absolute path")
	}
}

func coverageBlameMultipleCandidates(t *testing.T) {
	root, repo := blameResolveRepo(t)
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(root, "sub")
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	if _, err := BlameHeadFile(repo, "missing.txt"); err == nil {
		t.Fatal("BlameHeadFile accepted missing multiple candidates")
	}
}

func coverageBlameHeadFilePreflightError(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	_, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "ls-tree-error", "")
	if _, err := BlameHeadFile(fake, coverageFile); err == nil {
		t.Fatal("BlameHeadFile accepted a preflight failure")
	}
}

func coverageBlameBlobSizeError(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	fake := fakeRewriteRepo(t, root, "blob-size-error", "")
	if _, err := BlameHeadFile(fake, coverageFile); err == nil {
		t.Fatal("BlameHeadFile accepted a blob size failure")
	}
	_ = blob
}

func coverageBlameHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := Blame(repo, coverageFile); err == nil {
		t.Fatal("Blame accepted a HEAD failure")
	}
}

func coverageBlameNoteError(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", head)
	if _, err := Blame(fake, coverageFile); err == nil {
		t.Fatal("Blame accepted a note lookup failure")
	}
}

func coverageBlameInvalidRanges(t *testing.T) {
	root, repo := blameNoteRepo(t, "content\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	if err := repo.DeleteNoteRef("refs/notes/byline", head); err != nil {
		t.Fatal(err)
	}
	note := makeCoverageNote(blob)
	note.Files[coverageFile] = model.NoteFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 2,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := repo.WriteNote(head, encodeCoverageNote(t, note)); err != nil {
		t.Fatal(err)
	}
	if _, err := Blame(repo, coverageFile); err == nil {
		t.Fatal("Blame accepted invalid ranges")
	}
	_ = root
}

func coverageStatusPendingCount(t *testing.T) {
	_, repo, _, head := annotateCoverageRepo(t)
	record := coverageAnnotateRecord(head, strings.Repeat("a", 40))
	if err := store.New(repo.GitDir).AppendCheckpoint(record); err != nil {
		t.Fatal(err)
	}
	result, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.PendingCheckpoints != 1 {
		t.Fatalf("Status pending checkpoints = %d", result.PendingCheckpoints)
	}
}

func coverageAnnotateOperationLock(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate acquired a held operation lock")
	}
	_ = root
}

func coverageAnnotateCommonLock(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	path := filepath.Join(repo.CommonDir, "byline", "notes.lock")
	held, err := lock.Acquire(path, lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate acquired a held common lock")
	}
	_ = root
}

func coverageAnnotateHeadError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a HEAD failure")
	}
}

func coverageAnnotateBranchError(t *testing.T) {
	repo := branchErrorCoverageRepo(t)
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a current branch failure")
	}
}

func coverageAnnotateUnborn(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted an unborn repository")
	}
}

func coverageAnnotateReflogError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "reflog-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a reflog failure")
	}
}

func coverageAnnotateStateError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted invalid state")
	}
	_ = root
}

func coverageAnnotateCheckpointError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.CheckpointPath(), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted invalid checkpoints")
	}
	_ = root
}

func coverageAnnotateRequireNoteError(t *testing.T) {
	root, repo, _, head := annotateCoverageRepo(t)
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "notes-read-error", head)
	if _, err := Annotate(fake); err == nil {
		t.Fatal("Annotate accepted an attribution note read failure")
	}
}

func coverageAnnotateNoopProtectError(t *testing.T) {
	root, repo, _, head := annotateCoverageRepo(t)
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "update-ref-error", "")
	if _, err := Annotate(fake); err == nil {
		t.Fatal("Annotate accepted a noop retention failure")
	}
	_ = head
}

func coverageAnnotateParentError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a parent lookup failure")
	}
}

func coverageAnnotateDivergentHistory(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	git(t, root, "checkout", "-q", "-b", "side")
	write(t, root, coverageFile, "side\n")
	other := commit(t, root, "other")
	git(t, root, "checkout", "-q", "main")
	write(t, root, coverageFile, "main\n")
	commit(t, root, "main")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(other, []byte(`{"version":3,"files":{},"sessions":{}}`)); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = other
	state.Pending.BaseCommit = other
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "divergent history") {
		t.Fatalf("Annotate divergence = %v", err)
	}
}

func coverageAnnotateChangesError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "changes-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a changes failure")
	}
}

func coverageAnnotateInitialHistory(t *testing.T) {
	_, repo, _, _ := annotateCoverageRepo(t)
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "initializing attribution") {
		t.Fatalf("initial history result = %+v", result)
	}
}

func coverageAnnotateIdentityError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "author-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted an author failure")
	}
}

func coverageAnnotateTransitionError(t *testing.T) {
	root, repo, base, _ := annotateCoverageRepo(t)
	record := coverageAnnotateRecord(base, strings.Repeat("a", 40))
	if err := store.New(repo.GitDir).AppendCheckpoint(record); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a transition blob failure")
	}
	_ = root
}

func coverageAnnotateReplayError(t *testing.T) {
	root, repo, base, _ := annotateCoverageRepo(t)
	blob, err := repo.HashBytes([]byte("binary\x00\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.New(repo.GitDir).AppendCheckpoint(coverageAnnotateRecord(base, blob)); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a binary transition")
	}
	_ = root
}

func coverageAnnotateProjectError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	write(t, root, coverageFile, "binary\x00\n")
	commit(t, root, "binary")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "unsupported path") {
		t.Fatalf("binary project result = %+v", result)
	}
}

func coverageAnnotateWorktreeReadWarning(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	if err := os.Remove(filepath.Join(root, coverageFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, coverageFile), 0o700); err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !containsCoverageWarning(result.Warnings, "cannot retain pending path") {
		t.Fatalf("worktree read result = %+v", result)
	}
}

func coverageAnnotateMissingWorktree(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	if err := os.Remove(filepath.Join(root, coverageFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
}

func coverageAnnotateProtectError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "update-ref-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a retention failure")
	}
}

func coverageAnnotateNoteWriteError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "notes-write-error", "")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a note write failure")
	}
}

func coverageAnnotateStateWriteError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "next\n")
	commit(t, root, "next")
	fake := fakeRewriteRepo(t, root, "state-write-error", "")
	t.Setenv("FAKE_GIT_STATE_PATH", store.New(fake.GitDir).StatePath())
	if _, err := Annotate(fake); err == nil {
		t.Fatal("Annotate accepted a state write failure")
	}
}

func coverageAnnotateFinalProtectError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "next\n")
	commit(t, root, "next")
	fake := fakeRewriteRepo(t, root, "update-ref-second-error", "")
	if _, err := Annotate(fake); err == nil {
		t.Fatal("Annotate accepted a final retention failure")
	}
}

func coverageDropNonStrandedError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := AnnotateDroppingStranded(repo); err == nil {
		t.Fatal("AnnotateDroppingStranded accepted a non-stranded failure")
	}
}

func coverageDropLockError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints acquired a held lock")
	}
	_ = root
}

func coverageDropCheckpointError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.CheckpointPath(), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted invalid checkpoints")
	}
	_ = root
}

func coverageDropStateError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dataStore.StatePath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted an invalid state path")
	}
	_ = root
}

func coverageDropHeadError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted a HEAD failure")
	}
}

func coverageDropBranchError(t *testing.T) {
	repo := branchErrorCoverageRepo(t)
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted a current branch failure")
	}
}

func coverageDropParentError(t *testing.T) {
	root, _, _, _ := annotateCoverageRepo(t)
	repo := fakeRewriteRepo(t, root, "parents-error", "")
	if _, err := dropStrandedCheckpoints(repo); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted a parent failure")
	}
}

func coverageDropReachabilityError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	git(t, root, "checkout", "-q", "-b", "stranded")
	write(t, root, coverageFile, "stranded\n")
	stranded := commit(t, root, "stranded")
	git(t, root, "checkout", "-q", "main")
	git(t, root, "branch", "-D", "stranded")
	if err := store.New(repo.GitDir).AppendCheckpoint(
		coverageAnnotateRecord(stranded, strings.Repeat("a", 40)),
	); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "any-branch-error", "")
	if _, err := dropStrandedCheckpoints(fake); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted a reachability failure")
	}
}

func coverageDropSkippedRecord(t *testing.T) {
	_, repo, base, _ := annotateCoverageRepo(t)
	record := coverageAnnotateRecord(base, strings.Repeat("a", 40))
	if err := store.New(repo.GitDir).AppendCheckpoint(record); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.LastCheckpointSeq = record.Seq
	if err := store.New(repo.GitDir).WriteState(state); err != nil {
		t.Fatal(err)
	}
	if dropped, err := dropStrandedCheckpoints(repo); err != nil || dropped != 0 {
		t.Fatalf("skipped checkpoint = %d, %v", dropped, err)
	}
}

func coverageDropRetryError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	git(t, root, "checkout", "-q", "-b", "stranded")
	write(t, root, coverageFile, "stranded\n")
	stranded := commit(t, root, "stranded")
	git(t, root, "checkout", "-q", "main")
	git(t, root, "branch", "-D", "stranded")
	blob := mustBlob(t, repo, stranded, coverageFile)
	if err := store.New(repo.GitDir).AppendCheckpoint(coverageAnnotateRecord(stranded, blob)); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "after-branch-scan-head-error", "")
	if _, err := AnnotateDroppingStranded(fake); err == nil ||
		!strings.Contains(err.Error(), "post-scan HEAD") {
		t.Fatalf("AnnotateDroppingStranded retry error = %v", err)
	}
}

func coverageDropWriteError(t *testing.T) {
	root, repo, _, _ := annotateCoverageRepo(t)
	git(t, root, "checkout", "-q", "-b", "stranded")
	write(t, root, coverageFile, "stranded\n")
	stranded := commit(t, root, "stranded")
	git(t, root, "checkout", "-q", "main")
	git(t, root, "branch", "-D", "stranded")
	if err := store.New(repo.GitDir).AppendCheckpoint(coverageAnnotateRecord(stranded, strings.Repeat("a", 40))); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "drop-write-error", "")
	t.Setenv("FAKE_GIT_CHECKPOINT_PATH", store.New(fake.GitDir).CheckpointPath())
	if _, err := dropStrandedCheckpoints(fake); err == nil {
		t.Fatal("dropStrandedCheckpoints accepted a checkpoint write failure")
	}
}

func annotateCoverageRepo(t *testing.T) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	write(t, root, coverageFile, "head\n")
	head := commit(t, root, "head")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, base, head
}

func coverageAnnotateRecord(base, blob string) model.Checkpoint {
	return model.Checkpoint{
		Version:    model.CheckpointVersion,
		Kind:       model.CheckpointKindEdit,
		Seq:        1,
		LaneID:     model.CheckpointLaneID(1),
		BaseCommit: base,
		BranchRef:  "refs/heads/main",
		TS:         "2026-01-02T03:04:05Z",
		Type:       model.AuthorHuman,
		Files: []model.Snapshot{{
			Path:   coverageFile,
			Exists: true,
			Blob:   blob,
		}},
	}
}
