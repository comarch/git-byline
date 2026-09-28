package provenance

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/rewrite"
)

func TestCoverageStashPullBranches(t *testing.T) {
	t.Run("fast-forward common lock", coverageFastForwardCommonLock)
	t.Run("fast-forward save error", coverageFastForwardSaveError)
	t.Run("stash push note read", coverageStashPushNoteReadError)
	t.Run("save skips stashes", coverageSaveSkipsStashes)
	t.Run("save git errors", coverageSaveGitErrors)
	t.Run("stashed file errors", coverageStashedFileErrors)
	t.Run("autostash drop", coverageAutostashDropBranches)
	t.Run("stash merged", coverageStashMergedBranches)
	t.Run("head layer", coverageHeadLayerBranches)
	t.Run("project stash file", coverageProjectStashFileErrors)
	t.Run("delete all autostash ref", coverageDeleteAllAutostashRefError)
}

// stashedFastForward stashes agent edits, fast-forwards main to the forge
// tip, and returns the update the reference-transaction hook reports.
func stashedFastForward(t *testing.T) (string, *gitcmd.Repo, rewrite.RefUpdate) {
	t.Helper()
	root, repo, base, tip := stashPullRepo(t)
	agentEditsBeforePull(t, root, repo)
	git(t, root, "stash", "-q")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	return root, repo, rewrite.RefUpdate{Old: base, New: tip, Ref: "refs/heads/main"}
}

// stashSaveFixture returns a repository with an annotated base commit, a
// stash entry on base that holds an agent edit of coverageFile, and the
// checkpoints of that edit.
func stashSaveFixture(t *testing.T) (string, *gitcmd.Repo, string, string, []model.Checkpoint) {
	t.Helper()
	root, repo, base := annotatedRepo(t, coverageFile)
	write(t, root, coverageFile, "base\nagent\n")
	captureAI(t, repo, "save-session", coverageFile)
	git(t, root, "stash", "-q")
	stash := strings.TrimSpace(git(t, root, "rev-parse", "refs/stash"))
	records, _ := readCheckpointsAndState(t, repo)
	return root, repo, base, stash, records
}

// stashFileNote returns a valid stash note that covers coverageFile in stash.
func stashFileNote(t *testing.T, repo *gitcmd.Repo, stash string) []byte {
	t.Helper()
	return encodeCoverageNote(t, makeCoverageNoteForContent(mustBlob(t, repo, stash, coverageFile), coverageFile, 2))
}

// stashSaveBudgets bundles the shared budgets saveStashNote passes to
// stashedFile.
func stashSaveBudgets() (*rewriteBlobCache, *engine.MatcherBudget) {
	return newRewriteBlobCache(), engine.NewMatcherBudget(maxRewriteMatcherCells)
}

// blockCommonLock makes the common notes lock impossible to create.
func blockCommonLock(t *testing.T, root string, repo *gitcmd.Repo) {
	t.Helper()
	repo.CommonDir = filepath.Join(root, "common-file")
	if err := os.WriteFile(repo.CommonDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func coverageFastForwardCommonLock(t *testing.T) {
	root, repo, update := stashedFastForward(t)
	blockCommonLock(t, root, repo)
	if _, err := handleFastForward(repo, update); err == nil {
		t.Fatal("handleFastForward acquired a blocked common lock")
	}
}

func coverageFastForwardSaveError(t *testing.T) {
	root, _, update := stashedFastForward(t)
	fake := fakeRewriteRepo(t, root, "ref-error", "")
	if _, err := handleFastForward(fake, update); err == nil {
		t.Fatal("handleFastForward accepted a stash ref failure")
	}
}

func coverageStashPushNoteReadError(t *testing.T) {
	root, _, stash, _ := stashCoverageFixture(t)
	fake := fakeRewriteRepo(t, root, "notes-read-error", stash)
	if _, err := handleStashMove(fake, rewrite.RefUpdate{New: stash, Ref: "refs/stash"}); err == nil {
		t.Fatal("handleStashMove accepted a pushed stash note read failure")
	}
}

func coverageSaveSkipsStashes(t *testing.T) {
	_, repo, base, stash, records := stashSaveFixture(t)
	other := []model.Checkpoint{{Seq: records[0].Seq, Files: []model.Snapshot{{Path: "other.txt"}}}}
	for _, tc := range []struct {
		name      string
		oldTip    string
		touched   []model.Checkpoint
		untouched []model.Checkpoint
	}{
		{"stash taken on another commit", stash, records, nil},
		{"stash without the paths", base, other, nil},
		{"paths with live checkpoints", base, records, records},
	} {
		result, err := saveStashedCheckpoints(repo, tc.oldTip, tc.touched, tc.untouched)
		if err != nil || result.Written != 0 || len(result.Warnings) != 0 {
			t.Fatalf("%s = %+v, %v; want nothing saved", tc.name, result, err)
		}
	}
	assertNoStashNotes(t, repo)
}

func coverageSaveGitErrors(t *testing.T) {
	root, _, base, stash, records := stashSaveFixture(t)
	// notes-read-error fails only for the stash commit, so it reaches the
	// stash note read.
	for _, mode := range []string{"ref-error", "parents-error", "changes-error", "notes-read-error"} {
		t.Run(mode, func(t *testing.T) {
			fake := fakeRewriteRepo(t, root, mode, stash)
			if _, err := saveStashedCheckpoints(fake, base, records, nil); err == nil {
				t.Fatalf("saveStashedCheckpoints accepted %s", mode)
			}
		})
	}
}

// A stash note whose ownership read fails cannot be extended.
func TestCoverageSaveNoteOwnershipReadError(t *testing.T) {
	root, repo, base, stash, records := stashSaveFixture(t)
	writeStashFixtureNote(t, repo, stash, stashFileNote(t, repo, stash))
	fake := fakeRewriteRepo(t, root, "ownership-read-error", "")
	if _, err := saveStashNote(fake, stash, base, records, []string{coverageFile}); err == nil {
		t.Fatal("saveStashNote accepted an ownership read failure")
	}
}

// saveStashNote skips a stash note git-byline does not own or cannot decode.
func TestCoverageSaveNoteUnownedAndInvalidStays(t *testing.T) {
	_, repo, base, stash, records := stashSaveFixture(t)
	paths := []string{coverageFile}
	unowned := stashFileNote(t, repo, stash)
	if err := repo.WriteNoteRef(stashNotesRef, stash, unowned); err != nil {
		t.Fatal(err)
	}
	result, err := saveStashNote(repo, stash, base, records, paths)
	if err != nil || result.Written != 0 || !containsCoverageWarning(result.Warnings, "unowned or invalid") {
		t.Fatalf("unowned stash note = %+v, %v", result, err)
	}
	if err := repo.DeleteNoteRef(stashNotesRef, stash); err != nil {
		t.Fatal(err)
	}
	writeStashFixtureNote(t, repo, stash, []byte("invalid\n"))
	result, err = saveStashNote(repo, stash, base, records, paths)
	if err != nil || result.Written != 0 || !containsCoverageWarning(result.Warnings, "unowned or invalid") {
		t.Fatalf("invalid stash note = %+v, %v", result, err)
	}
}

// A stash note at the file limit is not extended and stays byte-identical.
func TestCoverageSaveNoteFullNoteStays(t *testing.T) {
	_, repo, base, stash, records := stashSaveFixture(t)
	blob := mustBlob(t, repo, stash, coverageFile)
	files := make(map[string]model.NoteFile, notes.MaxFiles)
	for index := 0; index < notes.MaxFiles; index++ {
		files[fmt.Sprintf("full-%03d.txt", index)] = makeCoverageNoteForContent(blob, coverageFile, 2).Files[coverageFile]
	}
	full := encodeCoverageNote(t, model.Note{Version: model.NoteVersion, Files: files})
	writeStashFixtureNote(t, repo, stash, full)
	result, err := saveStashNote(repo, stash, base, records, []string{coverageFile})
	if err != nil || result.Written != 0 || !containsCoverageWarning(result.Warnings, "more than") {
		t.Fatalf("full stash note = %+v, %v", result, err)
	}
	if data, _, err := repo.ReadNoteRef(stashNotesRef, stash); err != nil || string(data) != string(full) {
		t.Fatalf("full stash note changed: %v", err)
	}
}

// An unreadable stash path warns and keeps the other paths going.
func TestCoverageSaveNoteStashedPathWarning(t *testing.T) {
	root, _, base, stash, records := stashSaveFixture(t)
	fake := fakeRewriteRepo(t, root, "read-blob-error", "")
	result, err := saveStashNote(fake, stash, base, records, []string{coverageFile})
	if err != nil || result.Written != 0 || !containsCoverageWarning(result.Warnings, "stash path "+coverageFile) {
		t.Fatalf("unreadable stash path = %+v, %v", result, err)
	}
}

// A stash blob over the rewrite budget warns and keeps going.
func TestCoverageSaveNoteBlobBudgetWarning(t *testing.T) {
	root, _, base, stash, records := stashSaveFixture(t)
	fake := fakeRewriteRepo(t, root, "blob-size-budget", "")
	result, err := saveStashNote(fake, stash, base, records, []string{coverageFile})
	if err != nil || result.Written != 0 || !containsCoverageWarning(result.Warnings, "stash path "+coverageFile) {
		t.Fatalf("over-budget stash path = %+v, %v", result, err)
	}
}

// Git failures while replacing the stash note surface as errors.
func TestCoverageSaveNoteWriteErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     string
		existing bool
	}{
		{"author error", "author-error", false},
		{"new note write", "notes-write-error", false},
		{"new ownership write", "ownership-write-error", false},
		{"replacement note write", "notes-write-error", true},
		{"replacement ownership write", "ownership-write-error", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repo, base, stash, records := stashSaveFixture(t)
			var old []byte
			if tc.existing {
				old = stashFileNote(t, repo, stash)
				writeStashFixtureNote(t, repo, stash, old)
			}
			fake := fakeRewriteRepo(t, root, tc.mode, "")
			if _, err := saveStashNote(fake, stash, base, records, []string{coverageFile}); err == nil {
				t.Fatalf("saveStashNote accepted %s", tc.mode)
			}
			if tc.existing {
				data, found, err := repo.ReadNoteRef(stashNotesRef, stash)
				if err != nil || !found || !bytes.Equal(data, old) {
					t.Fatalf("stash note after %s = %q, %t, %v; want original", tc.mode, data, found, err)
				}
			}
		})
	}
}

func coverageStashedFileErrors(t *testing.T) {
	root, repo, base, stash, records := stashSaveFixture(t)
	state := model.NewState()
	cache, budget := stashSaveBudgets()
	if file, exists, err := stashedFile(repo, stash, base, "missing.txt", state, records, "", cache, budget); err != nil || exists {
		t.Fatalf("path the stash lacks = %+v, %t, %v", file, exists, err)
	}
	for _, mode := range []string{"ls-tree-error", "read-blob-error"} {
		t.Run(mode, func(t *testing.T) {
			fake := fakeRewriteRepo(t, root, mode, "")
			if _, _, err := stashedFile(fake, stash, base, coverageFile, state, records, "", cache, budget); err == nil {
				t.Fatalf("stashedFile accepted %s", mode)
			}
		})
	}
	short, err := repo.HashBytes([]byte("base\n"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := repo.HashBytes([]byte("bin\x00\n"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := model.NewState()
	invalid.Pending.Files[coverageFile] = model.PendingFile{
		Blob:   short,
		Ranges: []model.Range{{Start: 1, End: 3, Attribution: model.Attribution{Author: model.AuthorHuman}}},
	}
	checkpoint := func(author model.Author, blob string) []model.Checkpoint {
		return []model.Checkpoint{{
			Seq:   1,
			Type:  author,
			Files: []model.Snapshot{{Path: coverageFile, Blob: blob, Exists: true}},
		}}
	}
	for _, tc := range []struct {
		name    string
		state   model.State
		records []model.Checkpoint
	}{
		{"invalid pending ranges", invalid, records},
		{"unsupported checkpoint author", state, checkpoint(model.AuthorUntracked, short)},
		{"binary checkpoint", state, checkpoint(model.AuthorHuman, binary)},
	} {
		if _, _, err := stashedFile(repo, stash, base, coverageFile, tc.state, tc.records, "", cache, budget); err == nil {
			t.Fatalf("stashedFile accepted %s", tc.name)
		}
	}
	write(t, root, coverageFile, "bin\x00\n")
	binaryStash := strings.TrimSpace(git(t, root, "stash", "create", "binary"))
	if _, _, err := stashedFile(repo, binaryStash, base, coverageFile, state, nil, "", cache, budget); err == nil {
		t.Fatal("stashedFile accepted binary stash content")
	}
}

func coverageAutostashDropBranches(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	// The worktree no longer holds the autostash, so the drop goes on to
	// the note cleanup.
	git(t, root, "reset", "-q", "--hard")
	for _, value := range []string{"", strings.Repeat("0", 40)} {
		result, err := handleAutostashDrop(repo, value)
		if err != nil || result.Written != 0 || result.Mapped != 0 {
			t.Fatalf("autostash drop without an old value %q = %+v, %v", value, result, err)
		}
	}
	// blob-size-error lets the exact match check pass and fails the merge
	// check, which reads blobs through the size-checked cache.
	for _, mode := range []string{"ref-error", "blob-size-error"} {
		t.Run(mode, func(t *testing.T) {
			fake := fakeRewriteRepo(t, root, mode, "")
			if _, err := handleAutostashDrop(fake, stash); err == nil {
				t.Fatalf("handleAutostashDrop accepted %s", mode)
			}
		})
	}
	blockCommonLock(t, root, repo)
	if _, err := handleAutostashDrop(repo, stash); err == nil {
		t.Fatal("handleAutostashDrop acquired a blocked common lock")
	}
}

func coverageStashMergedBranches(t *testing.T) {
	root, repo, stash, _ := stashCoverageFixture(t)
	base := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	paths := []string{coverageFile}
	for _, tc := range []struct {
		mode    string
		fail    string
		wantErr bool
	}{
		{"parents-error", "", true},
		// Content over the budget counts as not merged.
		{"blob-size-budget", "", false},
		{"target-blob-error", base, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			fake := fakeRewriteRepo(t, root, tc.mode, tc.fail)
			merged, err := stashMerged(fake, stash, paths)
			if merged || (err != nil) != tc.wantErr {
				t.Fatalf("stashMerged with %s = %t, %v", tc.mode, merged, err)
			}
		})
	}
	if merged, err := stashMerged(repo, stash, []string{"missing.txt"}); err != nil || merged {
		t.Fatalf("path missing everywhere = %t, %v", merged, err)
	}
	full := filepath.Join(root, coverageFile)
	if err := os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if merged, err := stashMerged(repo, stash, paths); err != nil || merged {
		t.Fatalf("path missing from the worktree = %t, %v", merged, err)
	}
	if err := os.Mkdir(full, 0o700); err != nil {
		t.Fatal(err)
	}
	if merged, err := stashMerged(repo, stash, paths); err == nil || merged {
		t.Fatalf("directory in the worktree = %t, %v", merged, err)
	}
}

func coverageHeadLayerBranches(t *testing.T) {
	root, repo, base := annotatedRepo(t, coverageFile)
	if _, found, err := headLayer(repo, "", coverageFile, newRewriteBlobCache()); err != nil || found {
		t.Fatalf("unborn HEAD layer = %t, %v", found, err)
	}
	if _, found, err := headLayer(repo, base, "missing.txt", newRewriteBlobCache()); err != nil || found {
		t.Fatalf("missing path layer = %t, %v", found, err)
	}
	for _, mode := range []string{"ls-tree-error", "blob-size-error"} {
		t.Run(mode, func(t *testing.T) {
			fake := fakeRewriteRepo(t, root, mode, "")
			if _, _, err := headLayer(fake, base, coverageFile, newRewriteBlobCache()); err == nil {
				t.Fatalf("headLayer accepted %s", mode)
			}
		})
	}
}

func coverageProjectStashFileErrors(t *testing.T) {
	root, repo, base := annotatedRepo(t, coverageFile)
	file := makeCoverageNoteForContent(mustBlob(t, repo, base, coverageFile), coverageFile, 1).Files[coverageFile]
	budget := engine.NewMatcherBudget(maxRewriteMatcherCells)
	t.Run("head layer error", func(t *testing.T) {
		fake := fakeRewriteRepo(t, root, "ls-tree-error", "")
		if _, err := projectStashFile(fake, base, coverageFile, file, []byte("base\n"), budget, newRewriteBlobCache()); err == nil {
			t.Fatal("projectStashFile accepted a HEAD tree failure")
		}
	})
	if _, err := projectStashFile(repo, base, coverageFile, file, []byte("bin\x00\n"), budget, newRewriteBlobCache()); err == nil {
		t.Fatal("projectStashFile accepted binary worktree content")
	}
}

func coverageDeleteAllAutostashRefError(t *testing.T) {
	root, _, _, _ := stashCoverageFixture(t)
	repo := fakeRewriteRepo(t, root, "autostash-ref-error", "")
	if _, err := deleteAllDroppedStashNotesLocked(repo); err == nil {
		t.Fatal("deleteAllDroppedStashNotesLocked accepted an autostash ref failure")
	}
}
