package store

import (
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

func TestCheckCheckpointAppendUsage(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())

	base := validCheckpoint(1)
	base.Type = model.AuthorAI
	base.Agent = "droid"
	base.Model = "model"
	base.Session = "session-1"
	base.Usage = &model.CheckpointUsage{
		MsgID:      "msg_a",
		TokensIn:   10,
		TokensOut:  20,
		CacheRead:  30,
		CacheWrite: 40,
	}
	if err := value.CheckCheckpointAppend(base, 0); err != nil {
		t.Fatalf("ai usage append check = %v", err)
	}

	human := validCheckpoint(2)
	human.Usage = &model.CheckpointUsage{TokensIn: 1}
	if err := value.CheckCheckpointAppend(human, 0); err == nil ||
		!strings.Contains(err.Error(), "only an ai checkpoint") {
		t.Fatalf("human usage error = %v", err)
	}

	legacy := base
	legacy.Seq = 3
	legacy.Version = model.CheckpointVersionV2
	if err := value.CheckCheckpointAppend(legacy, 0); err == nil ||
		!strings.Contains(err.Error(), "requires checkpoint version 3") {
		t.Fatalf("v2 usage error = %v", err)
	}

	oversized := base
	oversized.Seq = 4
	oversized.Usage = &model.CheckpointUsage{TokensIn: model.MaxCheckpointUsageTokens + 1}
	if err := value.CheckCheckpointAppend(oversized, 0); err == nil ||
		!strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized usage error = %v", err)
	}

	reserved := base
	reserved.Seq = 5
	reserved.Usage = &model.CheckpointUsage{MsgID: "msg::id"}
	if err := value.CheckCheckpointAppend(reserved, 0); err == nil ||
		!strings.Contains(err.Error(), "reserved separator") {
		t.Fatalf("separator msg id error = %v", err)
	}
}

func TestReadStateForUpdateMarksNoteVersionMigration(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	data := []byte(`{"version":3,"last_checkpoint_seq":0,"notes_version":3,"pending":{"files":{}},"lanes":{}}`)
	writeStoreFile(t, value.StatePath(), data)
	state, migrated, err := value.ReadStateForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || state.NotesVersion != model.NoteVersion {
		t.Fatalf("ReadStateForUpdate() = %+v, %t, want note migration", state, migrated)
	}
}
