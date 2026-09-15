package provenance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

const (
	coverageFile = "coverage.txt"
	coverageDir  = "coverage-dir"
)

func TestCoverageCaptureValidation(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		event preset.Event
		want  string
	}{
		{
			name:  "shell pre requires human",
			event: preset.Event{Kind: model.CheckpointKindShellPre, Type: model.AuthorAI},
			want:  "shell_pre event must be human",
		},
		{
			name:  "shell post requires ai",
			event: preset.Event{Kind: model.CheckpointKindShellPost, Type: model.AuthorHuman},
			want:  "shell_post event must be ai",
		},
		{
			name:  "unknown checkpoint kind",
			event: preset.Event{Kind: "unknown-kind", Type: model.AuthorHuman},
			want:  "unsupported checkpoint kind",
		},
		{
			name:  "invalid ai attribution",
			event: preset.Event{Type: model.AuthorAI, Paths: []string{coverageFile}},
			want:  "validate event attribution",
		},
		{
			name:  "invalid event id",
			event: preset.Event{Type: model.AuthorHuman, EventID: "bad\nid"},
			want:  "validate event identifier",
		},
		{
			name:  "unsupported author",
			event: preset.Event{Type: model.AuthorHumanOverride},
			want:  "unsupported event author",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := Capture(repo, test.event, time.Now())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Capture() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCoverageCaptureSkipsDuplicateAndInvalidPaths(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	if err := os.Mkdir(filepath.Join(root, coverageDir), 0o700); err != nil {
		t.Fatal(err)
	}
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, preset.Event{
		Type:  model.AuthorHuman,
		Paths: []string{coverageFile, coverageFile, coverageDir},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 1 || len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], coverageDir) {
		t.Fatalf("Capture() = %+v", result)
	}
}

func TestCoverageCaptureRejectsDifferentShellBase(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Kind:    model.CheckpointKindShellPre,
		Type:    model.AuthorHuman,
		EventID: "shell-event",
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "next\n")
	commit(t, root, "next")
	result, err := Capture(repo, preset.Event{
		Kind:    model.CheckpointKindShellPost,
		Type:    model.AuthorAI,
		Agent:   "droid",
		Model:   "model",
		Session: "shell-session",
		EventID: "shell-event",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], "different base") {
		t.Fatalf("Capture() = %+v", result)
	}
}

func TestCoverageShellHelpers(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shellDirtyPaths(&gitcmd.Repo{Root: filepath.Join(root, "missing")}); err == nil {
		t.Fatal("shellDirtyPaths accepted a missing worktree")
	}
	before := model.Checkpoint{Files: make([]model.Snapshot, maxShellPaths+1)}
	for index := range before.Files {
		before.Files[index].Path = fmt.Sprintf("file-%d.txt", index)
	}
	if _, err := shellPostPaths(repo, before); !errors.Is(err, errShellPathLimit) {
		t.Fatalf("shellPostPaths() error = %v, want path limit", err)
	}
	if got, err := shellPreviousSnapshot(repo, "", model.Checkpoint{}, coverageFile); err != nil ||
		got.Path != coverageFile || got.Exists {
		t.Fatalf("shellPreviousSnapshot(empty head) = %+v, %v", got, err)
	}
	if _, err := shellPreviousSnapshot(repo, "not-a-commit", model.Checkpoint{}, coverageFile); err == nil {
		t.Fatal("shellPreviousSnapshot accepted an invalid head")
	}
	left := []model.Snapshot{{Path: coverageFile, Exists: true, Blob: "one"}}
	right := []model.Snapshot{{Path: coverageFile, Exists: true, Blob: "two"}}
	if snapshotsEqualCheckpoints(left, right) {
		t.Fatal("snapshotsEqualCheckpoints considered different blobs equal")
	}
	if snapshotsEqualCheckpoints(left, nil) {
		t.Fatal("snapshotsEqualCheckpoints considered different lengths equal")
	}
	if !snapshotsEqualCheckpoints(left, left) {
		t.Fatal("snapshotsEqualCheckpoints rejected equal snapshots")
	}
	if !timestampBefore("invalid-b", "invalid-c") {
		t.Fatal("timestampBefore did not use lexical fallback")
	}
}

func TestCoverageCaptureNoopAndStoreFailures(t *testing.T) {
	t.Parallel()
	t.Run("duplicate shell pre", coverageDuplicateShellPre)
	t.Run("state read failure", coverageCaptureStateFailure)
	t.Run("checkpoint read failure", coverageCaptureCheckpointReadFailure)
	t.Run("head read failure", coverageCaptureHeadFailure)
	t.Run("retention failure", coverageCaptureRetentionFailure)
	t.Run("append failure", coverageCaptureAppendFailure)
}

func coverageDuplicateShellPre(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Kind:    model.CheckpointKindShellPre,
		Type:    model.AuthorHuman,
		EventID: "duplicate-pre",
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, event, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 0 {
		t.Fatalf("duplicate shell pre = %+v", result)
	}
}

func coverageCaptureStateFailure(t *testing.T) {
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
	_, err = Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "state") {
		t.Fatalf("Capture() error = %v, want state error", err)
	}
}

func coverageCaptureCheckpointReadFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dataStore.CheckpointPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("Capture() error = %v, want checkpoint error", err)
	}
}

func coverageCaptureHeadFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = filepath.Join(root, "missing")
	_, err = Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now())
	if err == nil {
		t.Fatal("Capture accepted a missing repository root")
	}
}

func coverageCaptureRetentionFailure(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	refParent := filepath.Join(repo.GitDir, "refs", "worktree", "byline")
	if err := os.MkdirAll(filepath.Dir(refParent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refParent, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "protect checkpoint blobs") {
		t.Fatalf("Capture() error = %v, want retention error", err)
	}
}

func coverageCaptureAppendFailure(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "first\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	event := preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	path := store.New(repo.GitDir).CheckpointPath()
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "second\n")
	_, err = Capture(repo, event, time.Now())
	if err == nil || !strings.Contains(err.Error(), "checkpoint") {
		t.Fatalf("Capture() error = %v, want checkpoint error", err)
	}
}

func TestCoverageSnapshotsAndInitialState(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := initialSnapshot(repo, model.NewState(), "", coverageFile); err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	state.Pending.Files[coverageFile] = model.PendingFile{Blob: "not-a-blob"}
	if _, _, err := initialSnapshot(repo, state, "", coverageFile); err == nil {
		t.Fatal("initialSnapshot accepted an invalid pending blob")
	}
	if _, _, err := initialSnapshot(repo, model.NewState(), "not-a-commit", coverageFile); err == nil {
		t.Fatal("initialSnapshot accepted an invalid parent")
	}
	if snapshot, warnings, err := initialSnapshot(repo, model.NewState(), commitID, "missing.txt"); err != nil ||
		len(snapshot.Lines) != 0 || len(warnings) != 0 {
		t.Fatalf("missing parent path snapshot = %+v, %v, %v", snapshot, warnings, err)
	}
	if _, err := commitIdentity(&gitcmd.Repo{Root: filepath.Join(root, "missing")}, commitID); err == nil {
		t.Fatal("commitIdentity accepted a missing repository root")
	}
	if !snapshotsEqual(engine.Snapshot{Lines: []string{"one\n"}}, engine.Snapshot{Lines: []string{"two\n"}}) == false {
		t.Fatal("snapshotsEqual changed lines were equal")
	}
	if snapshotsEqual(
		engine.Snapshot{Lines: []string{"one\n"}, Attributions: []model.Attribution{{Author: model.AuthorHuman}}},
		engine.Snapshot{Lines: []string{"one\n"}, Attributions: []model.Attribution{{Author: model.AuthorAI}}},
	) {
		t.Fatal("snapshotsEqual different attributions were equal")
	}
}

func TestCoverageTransitionsAndNoteValidation(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	record := model.Checkpoint{
		Seq:   1,
		Type:  model.AuthorUntracked,
		Files: []model.Snapshot{{Path: coverageFile, Exists: true, Blob: "not-a-blob"}},
	}
	if _, err := transitionsFor(repo, []model.Checkpoint{record}, coverageFile, coverageFile, "identity"); err == nil {
		t.Fatal("transitionsFor accepted unsupported checkpoint author")
	}
	record.Type = model.AuthorAI
	if _, err := transitionsFor(repo, []model.Checkpoint{record}, coverageFile, coverageFile, "identity"); err == nil {
		t.Fatal("transitionsFor accepted an invalid snapshot blob")
	}
	if err := repo.WriteNote(commitID, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if err := requireAttributionNote(repo, commitID); err == nil ||
		!strings.Contains(err.Error(), "invalid") {
		t.Fatalf("requireAttributionNote() error = %v", err)
	}
}

func TestCoverageStatusAndBlameErrors(t *testing.T) {
	t.Parallel()
	t.Run("status failures", coverageStatusFailures)
	t.Run("blame note failures", coverageBlameNoteFailures)
	t.Run("head note failures", coverageHeadNoteFailures)
}

func coverageStatusFailures(t *testing.T) {
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
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted invalid state")
	}
	if err := os.Remove(dataStore.StatePath()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dataStore.CheckpointPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(repo); err == nil {
		t.Fatal("Status accepted a checkpoint directory")
	}
}

func coverageBlameNoteFailures(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	head := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, coverageFile)
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	if _, err := Blame(repo, "../outside"); err == nil {
		t.Fatal("Blame accepted an escaping path")
	}
	if _, err := blamePath(repo, head, "not\x00a-path"); err == nil {
		t.Fatal("blamePath accepted a NUL path")
	}
	if _, err := blameNotedFile(repo, head, "../outside", blob, model.NoteFile{}); err == nil {
		t.Fatal("blameNotedFile accepted an escaping path")
	}
	if _, err := blameNotedFile(repo, head, "coverage/../"+coverageFile, blob, model.NoteFile{}); err == nil {
		t.Fatal("blameNotedFile accepted a non-normalized path")
	}
	if _, err := blameNotedFile(repo, head, coverageFile, "not-a-blob", model.NoteFile{}); err == nil {
		t.Fatal("blameNotedFile accepted an invalid blob")
	}
}

func coverageHeadNoteFailures(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := headNote(repo); err == nil {
		t.Fatal("headNote accepted an unborn repository")
	}
	write(t, root, coverageFile, "content\n")
	head := commit(t, root, "content")
	if err := repo.WriteNote(head, []byte("invalid\n")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := headNote(repo); err == nil || !strings.Contains(err.Error(), "decode HEAD") {
		t.Fatalf("headNote() error = %v", err)
	}
}

func TestCoveragePreflightAndCandidateBranches(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	head := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, coverageFile)
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	note := model.Note{Version: model.NoteVersion, Files: map[string]model.NoteFile{
		coverageFile: {Blob: blob, Ranges: []model.Range{{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}}}},
	}}
	if _, err := preflightBlameFiles(repo, "not-a-commit", []string{coverageFile}, note); err == nil {
		t.Fatal("preflightBlameFiles accepted an invalid head")
	}
	missing := note
	missing.Files = map[string]model.NoteFile{"missing.txt": {Blob: blob}}
	if _, err := preflightBlameFiles(repo, head, []string{"missing.txt"}, missing); err == nil {
		t.Fatal("preflightBlameFiles accepted a missing path")
	}
	mismatch := note
	mismatch.Files = map[string]model.NoteFile{coverageFile: {Blob: strings.Repeat("a", 40)}}
	if _, err := preflightBlameFiles(repo, head, []string{coverageFile}, mismatch); err == nil {
		t.Fatal("preflightBlameFiles accepted a mismatched blob")
	}
	if candidates, err := blameCandidates(repo, coverageFile); err != nil || len(candidates) != 1 {
		t.Fatalf("blameCandidates() = %v, %v", candidates, err)
	}
	if _, err := BlameHeadFile(repo, filepath.Join(root, "missing.txt")); err == nil {
		t.Fatal("BlameHeadFile accepted an unattributed absolute path")
	}
}

func TestCoverageCollectAndRetainedHelpers(t *testing.T) {
	t.Parallel()
	records := []model.Checkpoint{{
		Seq:   1,
		Files: []model.Snapshot{{Path: coverageFile, Exists: true, Blob: strings.Repeat("a", 40)}},
	}}
	state := model.NewState()
	state.Pending.Files[coverageFile] = model.PendingFile{Blob: strings.Repeat("b", 40)}
	state.LastCheckpointSeq = 2
	extra := model.Checkpoint{Files: []model.Snapshot{
		{Exists: true, Blob: strings.Repeat("c", 40)},
		{Exists: false},
	}}
	if got := retainedBlobs(records, state, extra); len(got) != 2 {
		t.Fatalf("retainedBlobs() = %v", got)
	}
	paths := collectPaths(records, state, map[string]gitcmd.Change{
		"z.txt": {},
		"a.txt": {},
	})
	if strings.Join(paths, ",") != "a.txt,coverage.txt,z.txt" {
		t.Fatalf("collectPaths() = %v", paths)
	}
}

func TestCoverageShellPostLimit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < maxShellPaths; index++ {
		write(t, root, fmt.Sprintf("shell-%03d.txt", index), "content\n")
	}
	event := preset.Event{
		Kind:    model.CheckpointKindShellPre,
		Type:    model.AuthorHuman,
		EventID: "shell-limit",
	}
	if result, err := Capture(repo, event, time.Now()); err != nil || result.Recorded != maxShellPaths {
		t.Fatalf("shell_pre = %+v, %v", result, err)
	}
	write(t, root, "shell-over-limit.txt", "content\n")
	result, err := Capture(repo, preset.Event{
		Kind:    model.CheckpointKindShellPost,
		Type:    model.AuthorAI,
		Agent:   "droid",
		Model:   "model",
		Session: "shell-limit-session",
		EventID: "shell-limit",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], "more than 500") {
		t.Fatalf("shell_post = %+v", result)
	}
}

func TestCoverageCaptureRejectsTooManyRecords(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for index := 1; index <= 100_000; index++ {
		fmt.Fprintf(&lines,
			`{"version":1,"kind":"shell_pre","seq":%d,"base_commit":"","event_id":"","ts":"2026-01-01T00:00:00Z","type":"human","files":[]}`+"\n",
			index,
		)
	}
	if err := os.WriteFile(dataStore.CheckpointPath(), []byte(lines.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{coverageFile}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "would exceed 100000 records") {
		t.Fatalf("Capture() error = %v, want record limit", err)
	}
}

func TestCoverageAnnotateEarlyFailures(t *testing.T) {
	t.Parallel()
	t.Run("common lock", coverageAnnotateCommonLockFailure)
	t.Run("worktree lock", coverageAnnotateWorktreeLockFailure)
	t.Run("head read", coverageAnnotateHeadFailure)
	t.Run("reflog read", coverageAnnotateReflogFailure)
	t.Run("state read", coverageAnnotateStateFailure)
	t.Run("checkpoint read", coverageAnnotateCheckpointFailure)
	t.Run("noop retention", coverageAnnotateNoopRetentionFailure)
}

func coverageAnnotateCommonLockFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.CommonDir = filepath.Join(root, "common-file")
	if err := os.WriteFile(repo.CommonDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate acquired a blocked common lock")
	}
}

func coverageAnnotateWorktreeLockFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.GitDir, "byline"), []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate acquired a blocked worktree lock")
	}
}

func coverageAnnotateHeadFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = filepath.Join(root, "missing")
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a missing repository root")
	}
}

func coverageAnnotateReflogFailure(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo.GitDir, "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo.GitDir, "logs"), []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a broken HEAD reflog")
	}
}

func coverageAnnotateStateFailure(t *testing.T) {
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
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted invalid state")
	}
}

func coverageAnnotateCheckpointFailure(t *testing.T) {
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
	if err := os.Mkdir(dataStore.CheckpointPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted a checkpoint directory")
	}
}

func coverageAnnotateNoopRetentionFailure(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "content\npending\n")
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "droid", Model: "model", Paths: []string{coverageFile}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	refParent := filepath.Join(repo.GitDir, "refs", "worktree", "byline")
	if err := os.RemoveAll(refParent); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(refParent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refParent, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "compact retained") {
		t.Fatalf("Annotate() error = %v", err)
	}
}
