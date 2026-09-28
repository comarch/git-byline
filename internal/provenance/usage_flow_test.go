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

type usageFixture struct {
	root           string
	repo           *gitcmd.Repo
	transcriptPath string
	event          preset.Event
	now            time.Time
}

func newUsageFixture(t *testing.T) usageFixture {
	t.Helper()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	return usageFixture{
		root:           root,
		repo:           repo,
		transcriptPath: transcriptPath,
		event: preset.Event{
			Type: model.AuthorAI, Agent: "droid", Model: "test-model",
			Session: "session-1", Paths: []string{"file.txt"},
			TranscriptPath: transcriptPath,
		},
		now: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func (fixture usageFixture) capture(t *testing.T, content string, offset time.Duration) {
	t.Helper()
	write(t, fixture.root, "file.txt", content)
	result, err := Capture(fixture.repo, fixture.event, fixture.now.Add(offset))
	if err != nil || result.Recorded != 1 {
		t.Fatalf("Capture() = %+v, %v", result, err)
	}
}

func (fixture usageFixture) writeTranscript(t *testing.T, content []byte) {
	t.Helper()
	if err := os.WriteFile(fixture.transcriptPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func usageRecords(t *testing.T, repo *gitcmd.Repo) []model.Checkpoint {
	t.Helper()
	records, _, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func annotateUsageNote(t *testing.T, repo *gitcmd.Repo) model.Note {
	t.Helper()
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	head, err := repo.Head()
	if err != nil {
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
	return note
}

func assertCheckpointUsage(t *testing.T, usage *model.CheckpointUsage, msgID string, tokensIn, tokensOut uint64) {
	t.Helper()
	if usage == nil || usage.MsgID != msgID ||
		usage.TokensIn != tokensIn || usage.TokensOut != tokensOut {
		t.Fatalf("checkpoint usage = %+v, want %s %d/%d", usage, msgID, tokensIn, tokensOut)
	}
}

func assertSessionUsage(t *testing.T, session model.NoteSession, tokensIn, tokensOut, cacheRead, cacheWrite uint64) {
	t.Helper()
	if session.TokensIn != tokensIn || session.TokensOut != tokensOut ||
		session.CacheRead != cacheRead || session.CacheWrite != cacheWrite {
		t.Fatalf("session usage = %+v, want %d/%d/%d/%d", session, tokensIn, tokensOut, cacheRead, cacheWrite)
	}
}

func TestAnnotateFoldsSessionUsage(t *testing.T) {
	t.Parallel()
	fixture := newUsageFixture(t)

	fixture.writeTranscript(t, []byte(usageTurnA))
	fixture.capture(t, "base\nai-one\n", 0)
	fixture.writeTranscript(t, []byte(usageTurnA+usageTurnB))
	fixture.capture(t, "base\nai-one\nai-two\n", time.Second)
	// The same assistant turn still ends the transcript, so a third edit of
	// the same turn must not add its usage a second time.
	fixture.capture(t, "base\nai-one\nai-two\nai-three\n", 2*time.Second)

	records := usageRecords(t, fixture.repo)
	if len(records) != 3 {
		t.Fatalf("checkpoint records = %d, want 3", len(records))
	}
	assertCheckpointUsage(t, records[0].Usage, "msg_a", 10, 20)
	assertCheckpointUsage(t, records[1].Usage, "msg_b", 100, 200)
	assertCheckpointUsage(t, records[2].Usage, "msg_b", 100, 200)

	git(t, fixture.root, "add", "file.txt")
	git(t, fixture.root, "commit", "-m", "ai edits")
	note := annotateUsageNote(t, fixture.repo)
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	assertSessionUsage(t, session, 110, 220, 77, 55)
}

func TestAnnotateExcludesUsageFromPendingWork(t *testing.T) {
	t.Parallel()
	fixture := newUsageFixture(t)

	fixture.writeTranscript(t, []byte(usageTurnA))
	fixture.capture(t, "base\ncommitted\n", 0)
	git(t, fixture.root, "add", "file.txt")
	git(t, fixture.root, "commit", "-m", "partial")
	fixture.writeTranscript(t, []byte(usageTurnA+usageTurnB))
	fixture.capture(t, "base\ncommitted\npending\n", time.Second)

	note := annotateUsageNote(t, fixture.repo)
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	assertSessionUsage(t, session, 10, 20, 7, 5)
}

func TestFoldSessionUsageCapsTotals(t *testing.T) {
	t.Parallel()
	sessions := sessionMetrics{}
	foldSessionUsage(sessions, []model.Checkpoint{
		{
			Type: model.AuthorAI, Agent: "droid", Session: "session-1",
			Usage: &model.CheckpointUsage{MsgID: "first", TokensIn: model.MaxCheckpointUsageTokens - 1},
		},
		{
			Type: model.AuthorAI, Agent: "droid", Session: "session-1",
			Usage: &model.CheckpointUsage{MsgID: "second", TokensIn: 2},
		},
	})
	session := sessions[model.NoteSessionKey("droid", "session-1")]
	if session.TokensIn != model.MaxCheckpointUsageTokens {
		t.Fatalf("tokens in = %d, want cap %d", session.TokensIn, model.MaxCheckpointUsageTokens)
	}
}

func TestCaptureDropsUsageFromMissingTranscript(t *testing.T) {
	t.Parallel()
	fixture := newUsageFixture(t)
	fixture.capture(t, "base\nai-one\n", 0)
	records := usageRecords(t, fixture.repo)
	if len(records) != 1 || records[0].Usage != nil {
		t.Fatalf("records = %+v, want one without usage", records)
	}

	git(t, fixture.root, "add", "file.txt")
	git(t, fixture.root, "commit", "-m", "ai edit")
	note := annotateUsageNote(t, fixture.repo)
	session := note.Sessions[model.NoteSessionKey("droid", "session-1")]
	assertSessionUsage(t, session, 0, 0, 0, 0)
}

// TestCaptureUsageOversizedValuesAreDropped locks the rule that usage over
// the validation cap never blocks the checkpoint itself.
func TestCaptureUsageOversizedValuesAreDropped(t *testing.T) {
	t.Parallel()
	fixture := newUsageFixture(t)
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
	fixture.writeTranscript(t, append(oversized, '\n'))
	fixture.capture(t, "base\nai-one\n", 0)
	records := usageRecords(t, fixture.repo)
	if len(records) != 1 || records[0].Usage != nil {
		t.Fatalf("records = %+v, want one without usage", records)
	}
}
