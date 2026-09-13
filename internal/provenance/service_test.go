package provenance

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

func TestEndToEndAttributionAndPartialCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Annotate(repo); err != nil || result.Files != 1 {
		t.Fatalf("Annotate(base) = %+v, %v", result, err)
	}

	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if result, err := Capture(repo, human, now); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(human) = %+v, %v", result, err)
	}
	write(t, root, "file.txt", "base\nai-one\n")
	if result, err := Capture(repo, ai, now.Add(time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(ai one) = %+v, %v", result, err)
	}
	git(t, root, "add", "file.txt")
	write(t, root, "file.txt", "base\nai-one\nai-two\n")
	if result, err := Capture(repo, ai, now.Add(2*time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(ai two) = %+v, %v", result, err)
	}
	git(t, root, "gc", "--prune=now")
	git(t, root, "commit", "-m", "partial")
	if result, err := Annotate(repo); err != nil || result.Files != 1 {
		t.Fatalf("Annotate(partial) = %+v, %v", result, err)
	}
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingFiles != 1 || status.PendingCheckpoints != 0 || status.RetainedSnapshots != 1 {
		t.Fatalf("Status(partial) = %+v", status)
	}
	blame, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 2 ||
		blame.Lines[0].Attribution.Author != model.AuthorHuman ||
		blame.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame(partial) = %+v", blame.Lines)
	}

	git(t, root, "add", "file.txt")
	git(t, root, "commit", "-m", "finish")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err = Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 3 {
		t.Fatalf("Blame(finish) lines = %d", len(blame.Lines))
	}
	for i := 1; i < 3; i++ {
		if blame.Lines[i].Attribution.Author != model.AuthorAI || blame.Lines[i].Attribution.Agent != "droid" {
			t.Fatalf("line %d attribution = %+v", i+1, blame.Lines[i].Attribution)
		}
	}
	status, err = Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingFiles != 0 || status.RetainedSnapshots != 0 || status.Head != status.LastAnnotatedCommit {
		t.Fatalf("Status(finish) = %+v", status)
	}
	collection, err := BlameHead(repo)
	if err != nil {
		t.Fatal(err)
	}
	if collection.Commit != status.Head || len(collection.Files) != 1 ||
		collection.Files[0].File != "file.txt" || len(collection.Files[0].Lines) != 3 {
		t.Fatalf("BlameHead() = %+v", collection)
	}
	single, err := BlameHeadFile(repo, "file.txt")
	if err != nil || single.File != "file.txt" || len(single.Lines) != 3 {
		t.Fatalf("BlameHeadFile() = %+v, %v", single, err)
	}
	if _, err := BlameHeadFile(repo, "missing.txt"); err == nil {
		t.Fatal("BlameHeadFile accepted file absent from HEAD note")
	}
	if result, err := Annotate(repo); err != nil || !result.Noop {
		t.Fatalf("idempotent Annotate() = %+v, %v", result, err)
	}
}

func TestAnnotateRecordsHumanOverrideSessionMetrics(t *testing.T) {
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
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{"file.txt"},
	}, now); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nagent\nkept\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "model",
		Session: "session-1", Paths: []string{"file.txt"},
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nhuman\nkept\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{"file.txt"},
	}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	head := commit(t, root, "override")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote() = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ranges := note.Files["file.txt"].Ranges
	if len(ranges) != 3 || ranges[1].Author != model.AuthorHumanOverride ||
		ranges[1].Agent != "droid" || ranges[1].Model != "model" ||
		ranges[1].Session != "session-1" || ranges[2].Author != model.AuthorAI {
		t.Fatalf("override ranges = %+v", ranges)
	}
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	if session.Agent != "droid" || session.Model != "model" ||
		session.Added != 2 || session.Deleted != 0 ||
		session.Accepted != 1 || session.Overridden != 1 ||
		session.FirstTS != now.Add(time.Second).Format(time.RFC3339Nano) ||
		session.LastTS != now.Add(time.Second).Format(time.RFC3339Nano) {
		t.Fatalf("session metrics = %+v", session)
	}
}

func TestAnnotateCountsAISessionReplacedByAnotherSession(t *testing.T) {
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
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	write(t, root, "file.txt", "base\nagent-a\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "model",
		Session: "session-a", Paths: []string{"file.txt"},
	}, now); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nagent-b\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "model",
		Session: "session-b", Paths: []string{"file.txt"},
	}, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	head := commit(t, root, "replace")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote() = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	first := note.Sessions[model.NoteSessionKey("droid", "session-a")]
	second := note.Sessions[model.NoteSessionKey("droid", "session-b")]
	if first.Overridden != 1 || second.Accepted != 1 {
		t.Fatalf("session metrics = %+v", note.Sessions)
	}
}

func TestSessionMetricTimestampsUseChronologicalBounds(t *testing.T) {
	t.Parallel()
	sessions := sessionMetrics{}
	addTransitionSessionMetrics(sessions, []engine.TransitionStats{
		{
			Attribution: model.Attribution{
				Author: model.AuthorAI, Agent: "droid", Model: "model",
				Session: "session-1", TS: "2026-01-02T03:04:06Z",
			},
		},
		{
			Attribution: model.Attribution{
				Author: model.AuthorAI, Agent: "droid", Model: "model",
				Session: "session-1", TS: "2026-01-02T03:04:05Z",
			},
		},
	})
	got := materializeSessionMetrics(sessions)[model.NoteSessionKey("droid", "session-1")]
	if got.FirstTS != "2026-01-02T03:04:05Z" || got.LastTS != "2026-01-02T03:04:06Z" {
		t.Fatalf("session timestamps = %+v", got)
	}
}

func TestSessionMetricsNamespaceIdenticalIDsByAgent(t *testing.T) {
	t.Parallel()
	sessions := sessionMetrics{}
	ensureSession(sessions, model.Attribution{
		Author: model.AuthorAI, Agent: "droid", Model: "model-a", Session: "shared",
	})
	ensureSession(sessions, model.Attribution{
		Author: model.AuthorAI, Agent: "claude", Model: "model-b", Session: "shared",
	})
	got := materializeSessionMetrics(sessions)
	if len(got) != 2 ||
		got[model.NoteSessionKey("droid", "shared")].Model != "model-a" ||
		got[model.NoteSessionKey("claude", "shared")].Model != "model-b" {
		t.Fatalf("session metrics = %+v", got)
	}
}

func TestSessionWithoutSurvivingOutputUsesZeroCounters(t *testing.T) {
	t.Parallel()
	sessions := sessionMetrics{
		model.NoteSessionKey("droid", "session-1"): {
			Agent: "droid", Model: "model",
			FirstTS: "2026-01-02T03:04:05Z", LastTS: "2026-01-02T03:04:06Z",
			Added: 3, Deleted: 2,
		},
	}
	got := materializeSessionMetrics(sessions)[model.NoteSessionKey("droid", "session-1")]
	if got.Added != 0 || got.Deleted != 0 || got.Accepted != 0 || got.Overridden != 0 ||
		got.Agent != "droid" || got.Model != "model" {
		t.Fatalf("session counters = %+v", got)
	}
}

func TestLegacyStateLoadsAndNextAnnotationWritesCurrentNote(t *testing.T) {
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
	currentData, found, err := repo.ReadNote(base)
	if err != nil || !found {
		t.Fatalf("ReadNote(base) = %t, %v", found, err)
	}
	currentNote, err := notes.Decode(currentData)
	if err != nil {
		t.Fatal(err)
	}
	legacyFiles := map[string]model.NoteFile{}
	for path, file := range currentNote.Files {
		ranges := make([]model.Range, len(file.Ranges))
		for index, value := range file.Ranges {
			value.Identity = ""
			ranges[index] = value
		}
		legacyFiles[path] = model.NoteFile{Blob: file.Blob, Ranges: ranges}
	}
	legacyData, err := json.Marshal(struct {
		Version int                       `json:"version"`
		Files   map[string]model.NoteFile `json:"files"`
	}{Version: model.NoteVersionV1, Files: legacyFiles})
	if err != nil {
		t.Fatal(err)
	}
	legacyData = append(legacyData, '\n')
	git(t, root, "notes", "--ref=refs/notes/byline", "remove", base)
	if err := repo.WriteNote(base, legacyData); err != nil {
		t.Fatal(err)
	}
	stateStore := store.New(repo.GitDir)
	stateData, err := os.ReadFile(stateStore.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	currentVersion := fmt.Sprintf(`"notes_version":%d`, model.NoteVersion)
	if !strings.Contains(string(stateData), currentVersion) {
		t.Fatalf("state does not contain current notes version: %s", stateData)
	}
	stateData = []byte(strings.Replace(string(stateData), currentVersion, `"notes_version":1`, 1))
	if err := os.WriteFile(stateStore.StatePath(), stateData, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := stateStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.NotesVersion != model.NoteVersion {
		t.Fatalf("ReadState() notes version = %d, want %d", loaded.NotesVersion, model.NoteVersion)
	}
	write(t, root, "file.txt", "base\nnext\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "model",
		Session: "session-1", Paths: []string{"file.txt"},
	}, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	head := commit(t, root, "v2")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote() = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if note.Version != model.NoteVersion {
		t.Fatalf("note version = %d, want %d", note.Version, model.NoteVersion)
	}
	finalState, err := os.ReadFile(stateStore.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(finalState), currentVersion) {
		t.Fatalf("state was not upgraded: %s", finalState)
	}
}

func TestBlameHeadRequiresAttributionNote(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil || !strings.Contains(err.Error(), "HEAD has no attribution note") {
		t.Fatalf("BlameHead() error = %v", err)
	}
	if _, err := BlameHeadFile(repo, "file.txt"); err == nil || !strings.Contains(err.Error(), "HEAD has no attribution note") {
		t.Fatalf("BlameHeadFile() error = %v", err)
	}
}

func TestBlameHeadRejectsOversizedCollectionBeforeBlobRead(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\n")
	head := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: maxBlameCollectionLines + 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHead(repo); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("BlameHead() error = %v", err)
	}
}

func TestBlameHeadFileRejectsOversizedNoteCollection(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\n")
	head := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	files := make(map[string]model.NoteFile, maxBlameCollectionFiles+1)
	for index := 0; index <= maxBlameCollectionFiles; index++ {
		files[fmt.Sprintf("file-%03d.txt", index)] = model.NoteFile{Blob: blob}
	}
	files["file.txt"] = model.NoteFile{
		Blob: blob,
		Ranges: []model.Range{{
			Start: 1, End: 1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	data, err := json.Marshal(model.Note{Version: model.NoteVersion, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	if _, err := BlameHeadFile(repo, "file.txt"); err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("BlameHeadFile() error = %v", err)
	}
}

func TestAnnotateProtectsPendingBlobsBeforeStateWrite(t *testing.T) {
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
	event := preset.Event{Type: model.AuthorAI, Agent: "droid", Model: "test", Paths: []string{"file.txt"}}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	head := commit(t, root, "pending")
	git(t, root, "update-ref", "-d", "refs/worktree/byline/checkpoints")
	refParent := filepath.Join(repo.GitDir, "refs", "worktree", "byline")
	if info, err := os.Stat(refParent); err == nil && info.IsDir() {
		if err := os.Remove(refParent); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(refParent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refParent, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "protect pending snapshots") {
		t.Fatalf("Annotate() error = %v", err)
	}
	if _, found, err := repo.ReadNote(head); err != nil || found {
		t.Fatalf("failed annotation note found = %t, error = %v", found, err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit == head {
		t.Fatalf("state advanced to failed commit: %+v", state)
	}
}

func TestLegacyHistoryStartsUntracked(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "legacy.txt", "legacy\n")
	first := commit(t, root, "legacy")
	write(t, root, "legacy.txt", "legacy\nnew\n")
	head := commit(t, root, "change")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Annotate did not warn about existing history")
	}
	data, ok, err := repo.ReadNote(head)
	if err != nil || !ok {
		t.Fatalf("ReadNote() = %t, %v", ok, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ranges := note.Files["legacy.txt"].Ranges
	if len(ranges) != 2 || ranges[0].Author != model.AuthorUntracked || ranges[1].Author != model.AuthorHuman {
		t.Fatalf("legacy ranges = %+v", ranges)
	}
	if _, ok, err := repo.ReadNote(first); err != nil || ok {
		t.Fatalf("unexpected note on first commit: %t, %v", ok, err)
	}
}

func TestBlamePreservesLoneCarriageReturn(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\r")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 1 || result.Lines[0].Content != "content\r" {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestLinkedWorktreeKeepsStateSeparateAndNotesShared(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	mainRepo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(mainRepo); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	linked, err = filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	linkedRepo, err := gitcmd.Discover(linked)
	if err != nil {
		t.Fatal(err)
	}
	if linkedRepo.GitDir == mainRepo.GitDir || linkedRepo.CommonDir != mainRepo.CommonDir {
		t.Fatalf("worktree directories are not isolated: main=%+v linked=%+v", mainRepo, linkedRepo)
	}

	write(t, linked, "file.txt", "base\nlinked\n")
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	if _, err := Capture(linkedRepo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	if retained, err := linkedRepo.ProtectedBlobCount(); err != nil || retained != 1 {
		t.Fatalf("linked retained snapshots = %d, %v", retained, err)
	}
	if retained, err := mainRepo.ProtectedBlobCount(); err != nil || retained != 0 {
		t.Fatalf("main retained snapshots = %d, %v", retained, err)
	}
	commit(t, linked, "linked")
	if _, err := Annotate(linkedRepo); err != nil {
		t.Fatal(err)
	}

	mainStatus, err := Status(mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	linkedStatus, err := Status(linkedRepo)
	if err != nil {
		t.Fatal(err)
	}
	if mainStatus.Head == linkedStatus.Head ||
		mainStatus.Head != mainStatus.LastAnnotatedCommit ||
		linkedStatus.Head != linkedStatus.LastAnnotatedCommit {
		t.Fatalf("worktree states are not separate: main=%+v linked=%+v", mainStatus, linkedStatus)
	}
	if _, found, err := mainRepo.ReadNote(linkedStatus.Head); err != nil || !found {
		t.Fatalf("shared linked-worktree note found = %t, error = %v", found, err)
	}
}

func TestNoopAnnotateKeepsPendingCheckpointBlobs(t *testing.T) {
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
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	if result, err := Annotate(repo); err != nil || !result.Noop {
		t.Fatalf("Annotate() = %+v, %v", result, err)
	}
	git(t, root, "gc", "--prune=now")
	commit(t, root, "pending")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 2 || result.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestMissingCurrentNoteFailsClosed(t *testing.T) {
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
	git(t, root, "notes", "--ref=refs/notes/byline", "remove", "HEAD")
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Annotate() error = %v", err)
	}
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "missing") {
		t.Fatalf("Status() warnings = %v", status.Warnings)
	}
}

func TestUnsupportedFileDoesNotBlockLaterDeletion(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "binary", "a\x00b")
	commit(t, root, "binary")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil || len(result.Warnings) == 0 {
		t.Fatalf("Annotate(binary) = %+v, %v", result, err)
	}
	if err := os.Remove(filepath.Join(root, "binary")); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "delete binary")
	result, err = Annotate(repo)
	if err != nil || len(result.Warnings) == 0 {
		t.Fatalf("Annotate(delete binary) = %+v, %v", result, err)
	}
}

func TestPendingRenameKeepsAttribution(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "old.txt", "content\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"old.txt", "new.txt"},
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "other.txt", "other\n")
	git(t, root, "add", "other.txt")
	git(t, root, "commit", "-m", "other")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", "rename")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 1 || result.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestDivergentHistoryFailsClosed(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file", "one\n")
	commit(t, root, "one")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "two\n")
	commit(t, root, "two")
	write(t, root, "file", "three\n")
	commit(t, root, "three")
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("Annotate error = %v", err)
	}
}

func TestCaptureSkipsUnsafePaths(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, preset.Event{
		Type:  model.AuthorHuman,
		Paths: []string{"../outside", ".git/config"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 2 {
		t.Fatalf("Capture() = %+v", result)
	}
}

func TestCaptureRejectsHumanOverrideEvents(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Capture(repo, preset.Event{
		Type: model.AuthorHumanOverride, Agent: "droid", Paths: []string{"file.txt"},
	}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "unsupported event author") {
		t.Fatalf("Capture() error = %v", err)
	}
}

func TestShellPrePairing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		records []model.Checkpoint
		eventID string
		wantSeq uint64
		found   bool
	}{
		{
			name: "identifier",
			records: []model.Checkpoint{
				{Kind: model.CheckpointKindShellPre, Seq: 1, EventID: "first"},
				{Kind: model.CheckpointKindShellPre, Seq: 2, EventID: "second"},
			},
			eventID: "first", wantSeq: 1, found: true,
		},
		{
			name: "latest unpaired fallback",
			records: []model.Checkpoint{
				{Kind: model.CheckpointKindShellPre, Seq: 1, EventID: "first"},
				{Kind: model.CheckpointKindShellPre, Seq: 2, EventID: "second"},
				{Kind: model.CheckpointKindShellPost, Seq: 3, EventID: "first"},
			},
			wantSeq: 2, found: true,
		},
		{
			name: "missing identifier",
			records: []model.Checkpoint{
				{Kind: model.CheckpointKindShellPre, Seq: 1, EventID: "first"},
			},
			eventID: "missing", found: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, found := shellPreForPost(test.records, test.eventID)
			if found != test.found {
				t.Fatalf("found = %t, want %t", found, test.found)
			}
			if found && got.Seq != test.wantSeq {
				t.Fatalf("paired seq = %d, want %d", got.Seq, test.wantSeq)
			}
		})
	}
}

func TestShellCaptureKeepsOverlappingIdentifiedPres(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "a.txt", "a\n")
	write(t, root, "b.txt", "b\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, eventID := range []string{"first", "second"} {
		if result, err := Capture(repo, preset.Event{
			Kind: model.CheckpointKindShellPre, Type: model.AuthorHuman, EventID: eventID,
		}, time.Now()); err != nil || result.Recorded != 2 {
			t.Fatalf("Capture(shell_pre %s) = %+v, %v", eventID, result, err)
		}
	}
	write(t, root, "a.txt", "a changed\n")
	if result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
		Agent: "droid", Model: "model", Session: "session", EventID: "first",
	}, time.Now()); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(shell_post first) = %+v, %v", result, err)
	}
	write(t, root, "b.txt", "b changed\n")
	if result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
		Agent: "droid", Model: "model", Session: "session", EventID: "second",
	}, time.Now()); err != nil || result.Recorded != 2 {
		t.Fatalf("Capture(shell_post second) = %+v, %v", result, err)
	}
	records, _, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 || records[2].EventID != "first" || records[3].EventID != "second" {
		t.Fatalf("identified shell checkpoints = %+v", records)
	}
}

func TestShellPathLimit(t *testing.T) {
	t.Parallel()
	for _, count := range []int{maxShellPaths, maxShellPaths + 1} {
		count := count
		t.Run(fmt.Sprintf("%d paths", count), func(t *testing.T) {
			t.Parallel()
			root := testRepo(t)
			for index := 0; index < count; index++ {
				write(t, root, fmt.Sprintf("file-%03d.go", index), "content\n")
			}
			repo, err := gitcmd.Discover(root)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Capture(repo, preset.Event{
				Kind: model.CheckpointKindShellPre,
				Type: model.AuthorHuman,
			}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if count == maxShellPaths {
				if result.Recorded != count || len(result.Warnings) != 0 {
					t.Fatalf("Capture(%d) = %+v", count, result)
				}
				return
			}
			if result.Recorded != 0 || len(result.Warnings) != 1 {
				t.Fatalf("Capture(%d) = %+v", count, result)
			}
			if records, _, err := store.New(repo.GitDir).ReadCheckpoints(); err != nil || len(records) != 0 {
				t.Fatalf("oversized shell checkpoint records = %v, error = %v", records, err)
			}
		})
	}
}

func TestShellSnapshotBudgetStopsLoop(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "a.txt", strings.Repeat("a", maxShellSnapshotBytes/2))
	write(t, root, "b.txt", strings.Repeat("b", maxShellSnapshotBytes/2))
	write(t, root, "c.txt", "c\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPre,
		Type: model.AuthorHuman,
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 2 || len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], "16 MiB") {
		t.Fatalf("Capture() = %+v", result)
	}
	records, _, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Files) != 2 {
		t.Fatalf("budget checkpoint = %+v", records)
	}
}

func TestShellCaptureUsesBlobDifferences(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "tracked.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	write(t, root, "human.txt", "human\n")
	if result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPre, Type: model.AuthorHuman,
	}, time.Now()); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(shell_pre) = %+v, %v", result, err)
	}
	write(t, root, "human.txt", "human\n")
	write(t, root, "agent.txt", "agent\n")
	if result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
		Agent: "claude", Model: "test-model", Session: "session-1",
	}, time.Now()); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(shell_post unchanged preexisting) = %+v, %v", result, err)
	}

	records, warnings, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(records) != 2 {
		t.Fatalf("shell checkpoints = %d, warnings = %v", len(records), warnings)
	}
	if records[0].Kind != model.CheckpointKindShellPre || len(records[0].Files) != 1 ||
		records[0].Files[0].Path != "human.txt" {
		t.Fatalf("shell_pre record = %+v", records[0])
	}
	if records[1].Kind != model.CheckpointKindShellPost || len(records[1].Files) != 1 ||
		records[1].Files[0].Path != "agent.txt" ||
		records[1].Agent != "claude" || records[1].Model != "test-model" ||
		records[1].Session != "session-1" {
		t.Fatalf("shell_post record = %+v", records[1])
	}
}

func TestShellCaptureRecordsChangedPreexistingPath(t *testing.T) {
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
	write(t, root, "file.txt", "human\n")
	if _, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPre, Type: model.AuthorHuman,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "human\nagent\n")
	if _, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
		Agent: "droid", Model: "model", Session: "session",
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	records, _, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || len(records[1].Files) != 1 ||
		records[1].Files[0].Path != "file.txt" || !records[1].Files[0].Exists {
		t.Fatalf("shell changed path record = %+v", records)
	}
}

func TestShellPostWithoutPreIsIgnored(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, preset.Event{
		Kind: model.CheckpointKindShellPost, Type: model.AuthorAI,
		Agent: "droid", Model: "model", Session: "session",
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 1 {
		t.Fatalf("Capture(shell_post) = %+v", result)
	}
	if records, _, err := store.New(repo.GitDir).ReadCheckpoints(); err != nil || len(records) != 0 {
		t.Fatalf("ignored shell_post records = %v, error = %v", records, err)
	}
}

func TestRenameDeleteAndUntrackedBlame(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "old.txt", "one\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := Blame(repo, "old.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(untracked.Lines) != 1 || untracked.Lines[0].Attribution.Author != model.AuthorUntracked {
		t.Fatalf("untracked blame = %+v", untracked)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"old.txt"}}
	if _, err := Capture(repo, human, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", "one\ntwo\n")
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test",
		Paths: []string{"old.txt", "new.txt"},
	}
	if _, err := Capture(repo, ai, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "rename")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	renamed, err := Blame(repo, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(renamed.Lines) != 2 ||
		renamed.Lines[0].Attribution.Author != model.AuthorHuman ||
		renamed.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("renamed blame = %+v", renamed)
	}
	if _, err := Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{"new.txt"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Paths: []string{"new.txt"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "delete")
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 0 {
		t.Fatalf("delete annotated %d files", result.Files)
	}
	if _, err := Blame(repo, "new.txt"); err == nil {
		t.Fatal("Blame accepted deleted path")
	}
}

func TestUnbornOperationsAndUnannotatedStatus(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted unborn repository")
	}
	if _, err := Blame(repo, "file"); err == nil {
		t.Fatal("Blame accepted unborn repository")
	}
	write(t, root, "file", "one\n")
	commit(t, root, "one")
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Warnings) == 0 {
		t.Fatal("Status did not warn about unannotated HEAD")
	}
}

func TestCheckpointAfterCommitCarriesToNextCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "base\ncommitted\n")
	commit(t, root, "first")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{"file"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "base\ncommitted\nfuture-ai\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test", Paths: []string{"file"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	status, err := Status(repo)
	if err != nil || status.PendingFiles != 1 {
		t.Fatalf("Status() = %+v, %v", status, err)
	}
	commit(t, root, "second")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err := Blame(repo, "file")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 3 || blame.Lines[2].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() = %+v", blame)
	}
}

func TestAnnotateRecordsCommitIdentityForHumanLines(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	git(t, root, "config", "user.name", "John Doe")
	git(t, root, "config", "user.email", "john.doe@example.invalid")
	write(t, root, "file.txt", "one\ntwo\n")
	commit(t, root, "human lines")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range blame.Lines {
		if line.Attribution.Author != model.AuthorHuman ||
			line.Attribution.Identity != "john.doe" {
			t.Fatalf("line %d attribution = %+v", line.Number, line.Attribution)
		}
	}
}

func TestAnnotateKeepsMergedContentUntrackedAcrossCommits(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "base.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-b", "side")
	write(t, root, "vendor.txt", "vendor one\nvendor two\n")
	commit(t, root, "vendor import")
	git(t, root, "checkout", "main")
	git(t, root, "merge", "--no-ff", "-m", "merge side", "side")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	merged, err := Blame(repo, "vendor.txt")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range merged.Lines {
		if line.Attribution.Author != model.AuthorUntracked {
			t.Fatalf("merged line %d attribution = %+v", line.Number, line.Attribution)
		}
	}
	// A later commit that touches the merged file must not promote the
	// merge result to human attribution.
	write(t, root, "vendor.txt", "vendor one\nvendor two\nlocal three\n")
	commit(t, root, "extend vendor file")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	after, err := Blame(repo, "vendor.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Lines) != 3 {
		t.Fatalf("Blame() lines = %d", len(after.Lines))
	}
	for _, line := range after.Lines[:2] {
		if line.Attribution.Author != model.AuthorUntracked {
			t.Fatalf("line %d attribution = %+v", line.Number, line.Attribution)
		}
	}
	if after.Lines[2].Attribution.Author != model.AuthorHuman {
		t.Fatalf("new line attribution = %+v", after.Lines[2].Attribution)
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.name", "Test User")
	git(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, root, message string) string {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", message)
	return strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// blameResolveRepo prepares a repository where the nested sub/f.txt carries
// AI evidence and the root f.txt has none, with both committed and annotated.
func blameResolveRepo(t *testing.T) (string, *gitcmd.Repo) {
	t.Helper()
	root := testRepo(t)
	write(t, root, "sub/f.txt", "nested\n")
	write(t, root, "f.txt", "root\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(repo, preset.Event{Type: model.AuthorAI, Agent: "claude", Model: "m1", Session: "s1", Paths: []string{"sub/f.txt"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "both files")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	return root, repo
}

func TestBlameResolvesCwdRelativePath(t *testing.T) {
	root, repo := blameResolveRepo(t)

	cases := []struct {
		name       string
		chdir      string
		path       string
		wantHit    string
		wantAuthor model.Author
	}{
		{name: "cwd-relative hits nested file", chdir: filepath.Join(root, "sub"), path: "f.txt", wantHit: "nested", wantAuthor: model.AuthorAI},
		{name: "root-relative still works from subdirectory", chdir: filepath.Join(root, "sub"), path: "sub/f.txt", wantHit: "nested", wantAuthor: model.AuthorAI},
		{name: "cwd-relative wins on ambiguity like git", chdir: filepath.Join(root, "sub"), path: "f.txt", wantHit: "nested", wantAuthor: model.AuthorAI},
		{name: "absolute path is exact from subdirectory", chdir: filepath.Join(root, "sub"), path: filepath.Join(root, "f.txt"), wantHit: "root", wantAuthor: model.AuthorHuman},
		{name: "parent-relative resolves root file", chdir: filepath.Join(root, "sub"), path: filepath.Join("..", "f.txt"), wantHit: "root", wantAuthor: model.AuthorHuman},
		{name: "root-relative hits root file without evidence", chdir: root, path: "f.txt", wantHit: "root", wantAuthor: model.AuthorHuman},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.chdir)
			result, err := Blame(repo, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Lines) != 1 || result.Lines[0].Content != tc.wantHit {
				t.Fatalf("blame content = %q, want %q", result.Lines[0].Content, tc.wantHit)
			}
			if result.Lines[0].Attribution.Author != tc.wantAuthor {
				t.Fatalf("attribution = %+v, want %s", result.Lines[0].Attribution, tc.wantAuthor)
			}
		})
	}
}

func TestBlameMissingPathNamesBothInterpretations(t *testing.T) {
	root, repo := blameResolveRepo(t)
	t.Chdir(filepath.Join(root, "sub"))
	_, err := Blame(repo, "missing.txt")
	if err == nil {
		t.Fatal("expected an error for a missing path")
	}
	if !strings.Contains(err.Error(), "sub/missing.txt") || !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("error should name both candidates: %v", err)
	}
}

func TestBlameHeadFileResolvesCwdRelativePath(t *testing.T) {
	root, repo := blameResolveRepo(t)
	t.Chdir(filepath.Join(root, "sub"))
	cases := []struct {
		name    string
		path    string
		wantHit string
	}{
		{name: "cwd-relative argument", path: "f.txt", wantHit: "nested"},
		{name: "absolute argument is exact", path: filepath.Join(root, "f.txt"), wantHit: "root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := BlameHeadFile(repo, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Lines) != 1 || result.Lines[0].Content != tc.wantHit {
				t.Fatalf("dashboard blame content = %q, want %q", result.Lines[0].Content, tc.wantHit)
			}
		})
	}
}
