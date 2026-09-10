package store

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

func validCheckpoint(seq uint64) model.Checkpoint {
	return model.Checkpoint{
		Version: model.CheckpointVersion,
		Kind:    "edit",
		Seq:     seq,
		TS:      "2026-01-01T00:00:00Z",
		Type:    model.AuthorHuman,
		Files:   []model.Snapshot{{Path: "file.go", Exists: true, Blob: "abcd1234"}},
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := value.AppendCheckpoint(validCheckpoint(1)); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendCheckpoint(validCheckpoint(2)); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[1].Seq != 2 || len(warnings) != 0 {
		t.Fatalf("ReadCheckpoints() = %+v, %v", records, warnings)
	}
	info, err := os.Stat(value.CheckpointPath())
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint mode = %o", info.Mode().Perm())
	}
}

func TestCheckpointReadEdgeCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		data        string
		wantRecords int
		wantWarning bool
		wantError   bool
	}{
		{"absent", "", 0, false, false},
		{"unknown version", `{"version":9,"new_field":true}` + "\n", 0, true, false},
		{"truncated final", `{"version":1`, 0, true, false},
		{"invalid interior", "{bad}\n{}\n", 0, false, true},
		{"empty interior", "\n", 0, false, true},
		{"duplicate sequence", mustJSON(validCheckpoint(1)) + "\n" + mustJSON(validCheckpoint(1)) + "\n", 0, false, true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := New(t.TempDir())
			if test.name != "absent" {
				if err := os.MkdirAll(value.Dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(value.CheckpointPath(), []byte(test.data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			records, warnings, err := value.ReadCheckpoints()
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %t", err, test.wantError)
			}
			if err == nil && len(records) != test.wantRecords {
				t.Fatalf("records = %d, want %d", len(records), test.wantRecords)
			}
			if err == nil && (len(warnings) > 0) != test.wantWarning {
				t.Fatalf("warnings = %v", warnings)
			}
		})
	}
}

func TestAppendRepairsTruncatedCheckpointTail(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := mustJSON(validCheckpoint(1)) + "\n" + `{"version":1`
	if err := os.WriteFile(value.CheckpointPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	prefix, keptSize, dropped, err := inspectCheckpointTail(value.CheckpointPath(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefix) != 0 || keptSize != int64(len(mustJSON(validCheckpoint(1)))+1) || !dropped {
		t.Fatalf("tail plan = %q, %d, %t", prefix, keptSize, dropped)
	}
	if current, err := os.ReadFile(value.CheckpointPath()); err != nil || string(current) != data {
		t.Fatalf("tail inspection changed log to %q, %v", current, err)
	}
	if err := value.AppendCheckpoint(validCheckpoint(2)); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Seq != 1 || records[1].Seq != 2 || len(warnings) != 0 {
		t.Fatalf("ReadCheckpoints() = %+v, %v", records, warnings)
	}
}

func TestAppendRejectsCompleteInvalidCheckpointTail(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := mustJSON(validCheckpoint(1)) + "\n" + `{"version":1,"unknown":true}`
	if err := os.WriteFile(value.CheckpointPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inspectCheckpointTail(value.CheckpointPath(), false); err == nil {
		t.Fatal("inspectCheckpointTail accepted complete invalid record")
	}
	if err := value.AppendCheckpoint(validCheckpoint(2)); err == nil {
		t.Fatal("AppendCheckpoint accepted complete invalid record")
	}
	if current, err := os.ReadFile(value.CheckpointPath()); err != nil || string(current) != data {
		t.Fatalf("rejected tail changed log to %q, %v", current, err)
	}
}

func TestAppendPreservesValidCheckpointWithoutNewline(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(value.CheckpointPath(), []byte(mustJSON(validCheckpoint(1))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendCheckpoint(validCheckpoint(2)); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || len(warnings) != 0 {
		t.Fatalf("ReadCheckpoints() = %+v, %v", records, warnings)
	}
}

func TestCheckpointValidation(t *testing.T) {
	t.Parallel()
	tests := []model.Checkpoint{
		{},
		func() model.Checkpoint { value := validCheckpoint(1); value.Kind = "other"; return value }(),
		func() model.Checkpoint { value := validCheckpoint(1); value.TS = ""; return value }(),
		func() model.Checkpoint { value := validCheckpoint(1); value.TS = "invalid"; return value }(),
		func() model.Checkpoint { value := validCheckpoint(1); value.Files = nil; return value }(),
		func() model.Checkpoint { value := validCheckpoint(1); value.Files[0].Blob = ""; return value }(),
		func() model.Checkpoint {
			value := validCheckpoint(1)
			value.Files = append(value.Files, value.Files[0])
			return value
		}(),
	}
	for i, record := range tests {
		if err := (New(t.TempDir())).AppendCheckpoint(record); err == nil {
			t.Fatalf("case %d accepted invalid checkpoint", i)
		}
	}
}

func TestCheckpointAndStateReadLimits(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(value.CheckpointPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(value.CheckpointPath(), maxCheckpointBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := value.ReadCheckpoints(); err == nil {
		t.Fatal("ReadCheckpoints accepted oversized log")
	}
	if err := os.WriteFile(value.StatePath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(value.StatePath(), maxStateBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := value.ReadState(); err == nil {
		t.Fatal("ReadState accepted oversized state")
	}
}

func TestCheckpointAndStateWriteLimits(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const truncated = `{"version":1`
	if err := os.WriteFile(value.CheckpointPath(), []byte(truncated), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint := validCheckpoint(1)
	checkpoint.Files[0].Path = strings.Repeat("x", maxRecordBytes)
	if err := value.AppendCheckpoint(checkpoint); err == nil {
		t.Fatal("AppendCheckpoint accepted oversized record")
	}
	if data, err := os.ReadFile(value.CheckpointPath()); err != nil || string(data) != truncated {
		t.Fatalf("checkpoint changed to %q, %v", data, err)
	}

	state := model.NewState()
	if err := value.WriteState(state); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(value.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.Files[strings.Repeat("x", maxStateBytes)] = model.PendingFile{Blob: "abcd1234"}
	if err := value.WriteState(state); err == nil {
		t.Fatal("WriteState accepted oversized state")
	}
	if data, err := os.ReadFile(value.StatePath()); err != nil || !bytes.Equal(data, original) {
		t.Fatalf("state changed after oversized write: %v", err)
	}
}

func TestCheckpointRecordCountLimit(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := []byte(`{"version":9}` + "\n")
	data := bytes.Repeat(record, maxCheckpointRecords)
	if err := os.WriteFile(value.CheckpointPath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendCheckpoint(validCheckpoint(1)); err == nil {
		t.Fatal("AppendCheckpoint accepted excessive record count")
	}
	got, err := os.ReadFile(value.CheckpointPath())
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("checkpoint log changed after rejected append: %v", err)
	}
	if err := os.WriteFile(value.CheckpointPath(), append(data, record...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := value.ReadCheckpoints(); err == nil {
		t.Fatal("ReadCheckpoints accepted excessive record count")
	}
}

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	state, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	state.LastAnnotatedCommit = "abcd1234"
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	state.Pending.Files["file.go"] = model.PendingFile{
		Blob: "abcd5678",
		Ranges: []model.Range{{
			Start: 1, End: 1,
			Attribution: model.Attribution{Author: model.AuthorHuman},
		}},
	}
	if err := value.WriteState(state); err != nil {
		t.Fatal(err)
	}
	got, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.LastAnnotatedCommit != state.LastAnnotatedCommit || len(got.Pending.Files) != 1 {
		t.Fatalf("ReadState() = %+v", got)
	}
	if data, err := os.ReadFile(value.StatePath()); err != nil || !strings.HasSuffix(string(data), "\n") {
		t.Fatalf("state data = %q, err = %v", data, err)
	}
}

func TestStateErrors(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(value.StatePath(), []byte(`{"version":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := value.ReadState(); err == nil {
		t.Fatal("ReadState accepted unknown version")
	}
	if err := value.WriteState(model.State{}); err == nil {
		t.Fatal("WriteState accepted invalid version")
	}
	for _, state := range []model.State{
		func() model.State { value := model.NewState(); value.LastAnnotatedCommit = "bad"; return value }(),
		func() model.State {
			value := model.NewState()
			value.LastAnnotatedCommit = "abcd1234"
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Pending.Files["file"] = model.PendingFile{Blob: "bad"}
			return value
		}(),
	} {
		if err := value.WriteState(state); err == nil {
			t.Fatalf("WriteState accepted invalid state %+v", state)
		}
	}
}

func mustJSON(value model.Checkpoint) string {
	data := `{"version":1,"kind":"edit","seq":` + string(rune('0'+value.Seq)) +
		`,"ts":"2026-01-01T00:00:00Z","type":"human","files":[{"path":"file.go","exists":true,"blob":"abcd1234"}]}`
	return data
}

func TestStorePaths(t *testing.T) {
	t.Parallel()
	value := New(filepath.Join("repo", ".git"))
	if !strings.HasSuffix(value.CheckpointPath(), filepath.Join("byline", "checkpoints.jsonl")) ||
		!strings.HasSuffix(value.StatePath(), filepath.Join("byline", "state.json")) ||
		!strings.HasSuffix(value.LockPath(), filepath.Join("byline", "operation.lock")) {
		t.Fatalf("unexpected store paths: %+v", value)
	}
}
