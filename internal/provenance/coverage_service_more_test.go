package provenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

func TestCoverageCaptureErrorBranches(t *testing.T) {
	t.Run("edit lock", coverageCaptureLockError)
	t.Run("current branch", coverageCaptureBranchError)
	t.Run("migration state write", coverageCaptureMigrationWriteError)
	t.Run("shell pre paths", coverageCaptureShellPrePathError)
	t.Run("shell post paths", coverageCaptureShellPostPathError)
	t.Run("shell previous snapshot", coverageCaptureShellPreviousError)
	t.Run("dirty helper", coverageCaptureDirtyHelperError)
}

func coverageCaptureBranchError(t *testing.T) {
	repo := branchErrorCoverageRepo(t)
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{coverageFile},
	}, time.Now()); err == nil {
		t.Fatal("Capture accepted a current branch failure")
	}
}

func coverageCaptureMigrationWriteError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "capture-migration-write-error", "")
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"version":1,"notes_version":3,"pending":{"files":{}}}` + "\n")
	if err := os.WriteFile(dataStore.StatePath(), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GIT_STATE_PATH", dataStore.StatePath())
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{coverageFile},
	}, time.Now()); err == nil || !strings.Contains(err.Error(), "persist state migration") {
		t.Fatalf("Capture migration write error = %v", err)
	}
}

func coverageCaptureLockError(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now()); err == nil {
		t.Fatal("Capture acquired a held lock")
	}
}

func coverageCaptureShellPrePathError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	repo := fakeRewriteRepo(t, root, "dirty-error", "")
	if _, err := Capture(repo, preset.Event{
		Type:    model.AuthorHuman,
		Kind:    model.CheckpointKindShellPre,
		EventID: "shell-error",
	}, time.Now()); err == nil {
		t.Fatal("shell_pre accepted a dirty path failure")
	}
}

func coverageCaptureShellPostPathError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "pre\n")
	eventID := "shell-post-error"
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Kind: model.CheckpointKindShellPre, EventID: eventID,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "dirty-error", "")
	if _, err := Capture(fake, preset.Event{
		Type: model.AuthorAI, Kind: model.CheckpointKindShellPost, EventID: eventID,
		Agent: "droid", Model: "model", Session: "shell",
	}, time.Now()); err == nil {
		t.Fatal("shell_post accepted a dirty path failure")
	}
}

func coverageCaptureShellPreviousError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	eventID := "shell-previous-error"
	write(t, root, coverageFile, "pre\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Kind: model.CheckpointKindShellPre, EventID: eventID,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", "new\n")
	fake := fakeRewriteRepo(t, root, "ls-tree-error", "")
	result, err := Capture(fake, preset.Event{
		Type: model.AuthorAI, Kind: model.CheckpointKindShellPost, EventID: eventID,
		Agent: "droid", Model: "model", Session: "shell",
	}, time.Now())
	if err != nil || len(result.Warnings) == 0 {
		t.Fatalf("shell_post previous snapshot = %+v, %v", result, err)
	}
}

func coverageCaptureDirtyHelperError(t *testing.T) {
	root := testRepo(t)
	repo := fakeRewriteRepo(t, root, "dirty-error", "")
	if _, err := shellDirtyPaths(repo); err == nil {
		t.Fatal("shellDirtyPaths accepted a dirty path failure")
	}
	before := model.Checkpoint{Files: []model.Snapshot{{Path: coverageFile}}}
	if _, err := shellPostPaths(repo, before); err == nil {
		t.Fatal("shellPostPaths accepted a dirty path failure")
	}
}

func TestCoverageInitialAndReachabilityBranches(t *testing.T) {
	t.Run("reachable error", coverageReachableError)
	t.Run("pending blob error", coverageInitialPendingBlobError)
	t.Run("parent blob error", coverageInitialParentBlobError)
	t.Run("unsupported ranges", coverageInitialUnsupportedRanges)
	t.Run("output budget", coverageInitialOutputBudget)
	t.Run("binary base", coverageInitialBinaryBase)
	t.Run("unsupported transition", coverageTransitionsUnsupported)
	t.Run("transition blob error", coverageTransitionsBlobError)
}

func coverageReachableError(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = filepath.Join(root, "missing")
	if _, err := cachedReachable(repo)(strings.Repeat("a", 40)); err == nil {
		t.Fatal("cachedReachable accepted a missing repository")
	}
}

func coverageInitialPendingBlobError(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	blob, err := repo.HashBytes([]byte("pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := removeLooseObject(repo, blob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := initialSnapshot(repo, state, "", coverageFile); err == nil {
		t.Fatal("initialSnapshot accepted a missing pending blob")
	}
	_ = root
}

func coverageInitialParentBlobError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	parent := commit(t, root, "parent")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, _, err := repo.BlobID(parent, coverageFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeLooseObject(repo, blob); err != nil {
		t.Fatal(err)
	}
	if _, _, err := initialSnapshot(repo, model.NewState(), parent, coverageFile); err == nil {
		t.Fatal("initialSnapshot accepted a missing parent blob")
	}
}

func coverageInitialUnsupportedRanges(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "one\n")
	parent := commit(t, root, "parent")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, parent, coverageFile)
	if err := repo.WriteNote(parent, encodeCoverageNote(t, makeCoverageNoteForContent(blob, coverageFile, 2))); err != nil {
		t.Fatal(err)
	}
	_, warnings, err := initialSnapshot(repo, model.NewState(), parent, coverageFile)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("initialSnapshot ranges = %v, warnings=%v", err, warnings)
	}
	repo = fakeRewriteRepo(t, root, "notes-read-error", parent)
	if _, _, err := initialSnapshot(repo, model.NewState(), parent, coverageFile); err == nil ||
		!strings.Contains(err.Error(), "forced notes read") {
		t.Fatalf("initialSnapshot note read = %v", err)
	}
	_ = root
}

func coverageInitialOutputBudget(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	parent := commit(t, root, "parent")
	repo := fakeRewriteRepo(t, root, "content-size-budget", "")
	if _, _, err := initialSnapshot(repo, model.NewState(), parent, coverageFile); err == nil ||
		!strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("initialSnapshot output budget = %v", err)
	}
}

func coverageInitialBinaryBase(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	write(t, root, coverageFile, "binary\x00\n")
	parent := commit(t, root, "binary")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = initialSnapshot(repo, model.NewState(), parent, coverageFile)
	if err == nil {
		t.Fatal("initialSnapshot accepted binary base")
	}
}

func coverageTransitionsUnsupported(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transitionsFor(repo, []model.Checkpoint{{
		Seq: 1, Type: model.Author("invalid"),
	}}, coverageFile, coverageFile, "")
	if err == nil {
		t.Fatal("transitionsFor accepted an unsupported author")
	}
	_ = root
}

func coverageTransitionsBlobError(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transitionsFor(repo, []model.Checkpoint{{
		Seq: 1, Type: model.AuthorHuman, Files: []model.Snapshot{{
			Path: coverageFile, Exists: true, Blob: strings.Repeat("a", 40),
		}},
	}}, coverageFile, coverageFile, "")
	if err == nil {
		t.Fatal("transitionsFor accepted a missing snapshot blob")
	}
	_ = root
}

func TestCoverageBlameAndStatusErrorBranches(t *testing.T) {
	t.Run("head note error", coverageHeadNoteErrors)
	t.Run("collection invalid range", coverageBlameCollectionInvalidRange)
	t.Run("collection size error", coverageBlameCollectionSizeError)
	t.Run("collection line limit", coverageBlameCollectionLineLimit)
	t.Run("candidates", coverageBlameCandidateErrors)
	t.Run("noted file errors", coverageBlameNotedFileErrors)
	t.Run("blame path errors", coverageBlamePathErrors)
	t.Run("status state", coverageStatusStateError)
	t.Run("status log", coverageStatusLogError)
	t.Run("status head", coverageStatusHeadError)
	t.Run("status branch", coverageStatusBranchError)
	t.Run("status protected", coverageStatusProtectedError)
}

func coverageHeadNoteErrors(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, _, err := headNote(repo); err == nil {
		t.Fatal("headNote accepted a HEAD failure")
	}
	t.Setenv("FAKE_GIT_MODE", "")
	unbornRoot := testRepo(t)
	unborn, err := gitcmd.Discover(unbornRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := headNote(unborn); err == nil {
		t.Fatal("headNote accepted an unborn repository")
	}
	normal, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := headNote(normal); err == nil {
		t.Fatal("headNote accepted a missing note")
	}
	head, err := normal.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := normal.WriteNote(head, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := headNote(normal); err == nil {
		t.Fatal("headNote accepted an invalid note")
	}
}

func coverageBlameCollectionInvalidRange(t *testing.T) {
	root, repo := blameNoteRepo(t, "one\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	if err := repo.DeleteNoteRef("refs/notes/byline", head); err != nil {
		t.Fatal(err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 2,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	if err := repo.WriteNote(head, encodeCoverageNote(t, note)); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil {
		t.Fatal("BlameHead accepted invalid ranges")
	}
	_ = root
}

func coverageBlameCollectionSizeError(t *testing.T) {
	root, repo := blameNoteRepo(t, "one\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	if err := repo.DeleteNoteRef("refs/notes/byline", head); err != nil {
		t.Fatal(err)
	}
	repo = fakeRewriteRepo(t, root, "blob-size-budget", "")
	if err := repo.WriteNote(head, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil {
		t.Fatal("BlameHead accepted an oversized content report")
	}
}

func coverageBlameCollectionLineLimit(t *testing.T) {
	root, repo := blameNoteRepo(t, "one\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	if err := repo.DeleteNoteRef("refs/notes/byline", head); err != nil {
		t.Fatal(err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: maxBlameCollectionLines + 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	if err := repo.WriteNote(head, encodeCoverageNote(t, note)); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil {
		t.Fatal("BlameHead accepted an oversized line range")
	}
	_ = root
}

func coverageBlameCandidateErrors(t *testing.T) {
	root, repo := blameResolveRepo(t)
	if _, err := blameCandidates(repo, string([]byte{'a', 0})); err == nil {
		t.Fatal("blameCandidates accepted a NUL path")
	}
	if _, err := BlameHeadFile(repo, filepath.Join(root, "missing.txt")); err == nil {
		t.Fatal("BlameHeadFile accepted a missing absolute path")
	}
	if _, err := BlameHeadFile(repo, "missing.txt"); err == nil {
		t.Fatal("BlameHeadFile accepted an unattributed path")
	}
}

func coverageBlameNotedFileErrors(t *testing.T) {
	root, repo := blameNoteRepo(t, "one\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, head, coverageFile)
	file := makeCoverageNote(blob).Files[coverageFile]
	if _, err := blameNotedFile(repo, head, "../bad", blob, file); err == nil {
		t.Fatal("blameNotedFile accepted an escaping path")
	}
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := blameNotedFile(fake, head, coverageFile, blob, file); err == nil {
		t.Fatal("blameNotedFile accepted a blob read failure")
	}
	file.Ranges[0].End = 2
	if _, err := blameNotedFile(repo, head, coverageFile, blob, file); err == nil {
		t.Fatal("blameNotedFile accepted invalid ranges")
	}
}

func coverageBlamePathErrors(t *testing.T) {
	root, repo := blameNoteRepo(t, "one\n")
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	if _, err := blamePath(fake, head, coverageFile); err == nil {
		t.Fatal("blamePath accepted a blob read failure")
	}
	if _, err := Blame(fake, coverageFile); err == nil {
		t.Fatal("Blame accepted a blob read failure")
	}
	binaryRoot := testRepo(t)
	write(t, binaryRoot, coverageFile, "binary\x00\n")
	binaryCommit := commit(t, binaryRoot, "binary")
	binaryRepo, err := gitcmd.Discover(binaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blamePath(binaryRepo, binaryCommit, coverageFile); err == nil {
		t.Fatal("blamePath accepted binary content")
	}
	_ = repo
}

func coverageStatusStateError(t *testing.T) {
	root := testRepo(t)
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
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted invalid state")
	}
}

func coverageStatusLogError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.CheckpointPath(), []byte("{\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted invalid checkpoint log")
	}
}

func coverageStatusHeadError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "head-error", "")
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted a HEAD failure")
	}
}

func coverageStatusBranchError(t *testing.T) {
	repo := branchErrorCoverageRepo(t)
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted a current branch failure")
	}
}

func coverageStatusProtectedError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo := fakeRewriteRepo(t, root, "protected-error", "")
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted a protected blob failure")
	}
}

func blameNoteRepo(t *testing.T, content string) (string, *gitcmd.Repo) {
	t.Helper()
	root := testRepo(t)
	write(t, root, coverageFile, content)
	commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	return root, repo
}
