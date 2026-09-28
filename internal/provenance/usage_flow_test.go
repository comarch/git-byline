package provenance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

const usageTurnA = `{"type":"assistant","message":{"id":"msg_a","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":5,"cache_read_input_tokens":7}}}` + "\n"

const usageTurnB = `{"type":"assistant","message":{"id":"msg_b","usage":{"input_tokens":100,"output_tokens":200,"cache_creation_input_tokens":50,"cache_read_input_tokens":70}}}` + "\n"

func TestAnnotateFoldsSessionUsage(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(usageTurnA), 0o600); err != nil {
		t.Fatal(err)
	}
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
		TranscriptPath: transcriptPath,
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	write(t, root, "file.txt", "base\nai-one\n")
	if result, err := Capture(repo, ai, now); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(turn a) = %+v, %v", result, err)
	}
	if err := os.WriteFile(transcriptPath, []byte(usageTurnA+usageTurnB), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file.txt", "base\nai-one\nai-two\n")
	if result, err := Capture(repo, ai, now.Add(time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(turn b) = %+v, %v", result, err)
	}
	// The same assistant turn still ends the transcript, so a third edit of
	// the same turn must not add its usage a second time.
	write(t, root, "file.txt", "base\nai-one\nai-two\nai-three\n")
	if result, err := Capture(repo, ai, now.Add(2*time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(turn b again) = %+v, %v", result, err)
	}

	dataStore := store.New(repo.GitDir)
	records, _, err := dataStore.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("checkpoint records = %d, want 3", len(records))
	}
	if records[0].Usage == nil || records[0].Usage.MsgID != "msg_a" || records[0].Usage.TokensIn != 10 {
		t.Fatalf("turn a usage = %+v", records[0].Usage)
	}
	if records[1].Usage == nil || records[1].Usage.MsgID != "msg_b" || records[1].Usage.TokensOut != 200 {
		t.Fatalf("turn b usage = %+v", records[1].Usage)
	}
	if records[2].Usage == nil || records[2].Usage.MsgID != "msg_b" {
		t.Fatalf("turn b repeat usage = %+v", records[2].Usage)
	}

	git(t, root, "add", "file.txt")
	git(t, root, "commit", "-m", "ai edits")
	if result, err := Annotate(repo); err != nil || result.Files != 1 {
		t.Fatalf("Annotate(usage) = %+v, %v", result, err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote(usage) = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	if session.TokensIn != 110 || session.TokensOut != 220 ||
		session.CacheRead != 77 || session.CacheWrite != 55 {
		t.Fatalf("session usage = %+v, want 110/220/77/55", session)
	}
}

func TestCaptureDropsUsageFromMissingTranscript(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "missing.jsonl")
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
		TranscriptPath: transcriptPath,
	}
	write(t, root, "file.txt", "base\nai-one\n")
	if result, err := Capture(repo, ai, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(missing transcript) = %+v, %v", result, err)
	}
	dataStore := store.New(repo.GitDir)
	records, _, err := dataStore.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Usage != nil {
		t.Fatalf("records = %+v, want one without usage", records)
	}

	git(t, root, "add", "file.txt")
	git(t, root, "commit", "-m", "ai edit")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
		t.Fatal(err)
	}
	data, found, err := repo.ReadNote(head)
	if err != nil || !found {
		t.Fatalf("ReadNote(no usage) = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	if session.TokensIn != 0 || session.TokensOut != 0 ||
		session.CacheRead != 0 || session.CacheWrite != 0 {
		t.Fatalf("session usage = %+v, want zero", session)
	}
}

// TestCaptureUsageOversizedValuesAreDropped locks the rule that usage over
// the validation cap never blocks the checkpoint itself.
func TestCaptureUsageOversizedValuesAreDropped(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	oversized, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"id": "msg_big",
			"usage": map[string]any{
				"input_tokens": model.MaxCheckpointUsageTokens * 2,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(transcriptPath, append(oversized, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
		TranscriptPath: transcriptPath,
	}
	write(t, root, "file.txt", "base\nai-one\n")
	if result, err := Capture(repo, ai, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(oversized usage) = %+v, %v", result, err)
	}
	dataStore := store.New(repo.GitDir)
	records, _, err := dataStore.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Usage != nil {
		t.Fatalf("records = %+v, want one without usage", records)
	}
}
