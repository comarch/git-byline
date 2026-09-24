package store

import (
	"bytes"
	"fmt"
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
		LaneID:  model.CheckpointLaneID(seq),
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

func TestCheckpointRoundTripReadsVersionOneAndBranchContext(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	legacy := validCheckpoint(1)
	legacy.Version = model.CheckpointVersionV1
	legacy.LaneID = ""
	current := validCheckpoint(2)
	current.BranchRef = "refs/heads/feature"
	if err := value.AppendCheckpoint(legacy); err != nil {
		t.Fatal(err)
	}
	if err := value.AppendCheckpoint(current); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(records) != 2 ||
		records[0].BranchRef != "" ||
		records[1].BranchRef != "refs/heads/feature" {
		t.Fatalf("ReadCheckpoints() = %+v, %v", records, warnings)
	}
	legacy.BranchRef = "refs/heads/main"
	if err := value.CheckCheckpointAppend(legacy, len(records)); err == nil {
		t.Fatal("version 1 checkpoint accepted branch context")
	}
	invalidBase := validCheckpoint(3)
	invalidBase.BaseCommit = "main"
	if err := value.CheckCheckpointAppend(invalidBase, len(records)); err == nil {
		t.Fatal("checkpoint accepted symbolic base")
	}
}

func TestRewriteCheckpointBasesUsesBranchContext(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	for _, record := range []model.Checkpoint{
		func() model.Checkpoint {
			record := validCheckpoint(1)
			record.Version = model.CheckpointVersionV1
			record.LaneID = ""
			record.BaseCommit = "aaaa"
			return record
		}(),
		func() model.Checkpoint {
			record := validCheckpoint(2)
			record.BaseCommit = "aaaa"
			record.BranchRef = "refs/heads/feature"
			return record
		}(),
		func() model.Checkpoint {
			record := validCheckpoint(3)
			record.BaseCommit = "aaaa"
			record.BranchRef = "refs/heads/main"
			return record
		}(),
		func() model.Checkpoint {
			record := validCheckpoint(4)
			record.BaseCommit = "cccc"
			record.BranchRef = "refs/heads/feature"
			return record
		}(),
	} {
		if err := value.AppendCheckpoint(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := value.RewriteCheckpointBases(
		"refs/heads/feature",
		map[string]string{"aaaa": "bbbb", "cccc": "bbbb"},
	); err != nil {
		t.Fatal(err)
	}
	records, _, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if records[0].BaseCommit != "bbbb" ||
		records[0].Version != model.CheckpointVersion ||
		records[0].LaneID != model.LegacyCheckpointLaneID("aaaa") ||
		records[1].BaseCommit != "bbbb" ||
		records[1].LaneID != model.CheckpointLaneID(2) ||
		records[2].BaseCommit != "aaaa" ||
		records[3].BaseCommit != "bbbb" ||
		records[3].LaneID != model.CheckpointLaneID(4) {
		t.Fatalf("rewritten records = %+v", records)
	}
}

func TestMoveCheckpointsRewritesListedRecords(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	for seq := uint64(1); seq <= 4; seq++ {
		record := validCheckpoint(seq)
		record.LaneID = model.CheckpointLaneID(1)
		record.BaseCommit = "aaaa"
		record.BranchRef = "refs/heads/main"
		if err := value.AppendCheckpoint(record); err != nil {
			t.Fatal(err)
		}
	}
	if err := value.MoveCheckpoints("refs/heads/main", map[uint64]CheckpointMove{
		1: {BaseCommit: "aaaa", LaneID: model.CheckpointLaneID(3)},
		2: {BaseCommit: "bbbb", LaneID: model.CheckpointLaneID(1)},
		3: {BaseCommit: "aaaa", LaneID: model.CheckpointLaneID(3)},
	}); err != nil {
		t.Fatal(err)
	}
	records, _, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		base string
		lane string
	}{
		{"aaaa", model.CheckpointLaneID(3)},
		{"bbbb", model.CheckpointLaneID(1)},
		{"aaaa", model.CheckpointLaneID(3)},
		{"aaaa", model.CheckpointLaneID(1)},
	}
	for i, expected := range want {
		if records[i].BaseCommit != expected.base || records[i].LaneID != expected.lane ||
			records[i].BranchRef != "refs/heads/main" || records[i].Seq != uint64(i+1) {
			t.Fatalf("record %d = %+v, want base %s lane %s", i+1, records[i], expected.base, expected.lane)
		}
	}

	before, err := os.ReadFile(value.CheckpointPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := value.MoveCheckpoints("refs/heads/main", nil); err != nil {
		t.Fatalf("empty move = %v", err)
	}
	if err := value.MoveCheckpoints("refs/heads/main", map[uint64]CheckpointMove{
		4: {BaseCommit: "aaaa", LaneID: model.CheckpointLaneID(1)},
	}); err != nil {
		t.Fatalf("identity move = %v", err)
	}
	after, err := os.ReadFile(value.CheckpointPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("identity move changed checkpoint log")
	}
}

func TestMoveCheckpointsRejectsInvalidMovesWithoutMutation(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	legacy := validCheckpoint(1)
	legacy.Version = model.CheckpointVersionV1
	legacy.LaneID = ""
	legacy.BaseCommit = "aaaa"
	feature := validCheckpoint(2)
	feature.BaseCommit = "aaaa"
	feature.BranchRef = "refs/heads/feature"
	current := validCheckpoint(3)
	current.BaseCommit = "aaaa"
	current.BranchRef = "refs/heads/main"
	for _, record := range []model.Checkpoint{legacy, feature, current} {
		if err := value.AppendCheckpoint(record); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(value.CheckpointPath())
	if err != nil {
		t.Fatal(err)
	}
	lane := model.CheckpointLaneID(3)
	tests := []struct {
		name   string
		branch string
		moves  map[uint64]CheckpointMove
		want   string
	}{
		{"invalid branch", "main", map[uint64]CheckpointMove{3: {BaseCommit: "bbbb", LaneID: lane}}, "branch"},
		{"symbolic target", "refs/heads/main", map[uint64]CheckpointMove{3: {BaseCommit: "main", LaneID: lane}}, "move target for checkpoint 3"},
		{"invalid lane", "refs/heads/main", map[uint64]CheckpointMove{3: {BaseCommit: "bbbb", LaneID: "lane"}}, "move lane for checkpoint 3"},
		{"legacy lane", "refs/heads/main", map[uint64]CheckpointMove{3: {BaseCommit: "bbbb", LaneID: model.LegacyCheckpointLaneID("aaaa")}}, "move lane for checkpoint 3"},
		{"version one record", "refs/heads/main", map[uint64]CheckpointMove{1: {BaseCommit: "bbbb", LaneID: lane}}, "checkpoint 1 is outside branch context"},
		{"other branch", "refs/heads/main", map[uint64]CheckpointMove{2: {BaseCommit: "bbbb", LaneID: lane}}, "checkpoint 2 is outside branch context"},
		{"missing record", "refs/heads/main", map[uint64]CheckpointMove{3: {BaseCommit: "bbbb", LaneID: lane}, 9: {BaseCommit: "bbbb", LaneID: lane}}, "checkpoint 9 is missing"},
	}
	for _, test := range tests {
		err := value.MoveCheckpoints(test.branch, test.moves)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("%s: MoveCheckpoints() = %v, want %q", test.name, err, test.want)
		}
		after, readErr := os.ReadFile(value.CheckpointPath())
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("%s: rejected move changed checkpoint log", test.name)
		}
	}

	absent := New(t.TempDir())
	if err := absent.MoveCheckpoints("refs/heads/main", map[uint64]CheckpointMove{
		1: {BaseCommit: "bbbb", LaneID: lane},
	}); err == nil {
		t.Fatal("MoveCheckpoints accepted a missing checkpoint log")
	}
	invalid := New(t.TempDir())
	writeStoreFile(t, invalid.CheckpointPath(), checkpointJSON(t, validCheckpoint(0)))
	if err := invalid.MoveCheckpoints("refs/heads/main", map[uint64]CheckpointMove{
		1: {BaseCommit: "bbbb", LaneID: lane},
	}); err == nil {
		t.Fatal("MoveCheckpoints accepted an invalid checkpoint log")
	}
}

func TestDropCheckpointRecordsPreservesOtherLines(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	unknown := `{"version":9,"new_field":true}`
	truncated := `{"version":1`
	data := mustJSON(validCheckpoint(1)) + "\n" +
		unknown + "\n" +
		mustJSON(validCheckpoint(2)) + "\n" +
		truncated
	if err := os.WriteFile(value.CheckpointPath(), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if dropped, err := value.DropCheckpointRecords(nil); err != nil || dropped != 0 {
		t.Fatalf("empty drop = %d, %v", dropped, err)
	}
	if dropped, err := value.DropCheckpointRecords(map[uint64]bool{99: true}); err != nil || dropped != 0 {
		t.Fatalf("missing sequence drop = %d, %v", dropped, err)
	}
	dropped, err := value.DropCheckpointRecords(map[uint64]bool{2: true})
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	want := mustJSON(validCheckpoint(1)) + "\n" + unknown + "\n" + truncated
	got, err := os.ReadFile(value.CheckpointPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("checkpoint log = %q, want %q", got, want)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Seq != 1 || len(warnings) != 2 {
		t.Fatalf("ReadCheckpoints() = %+v, %v", records, warnings)
	}
}

func TestDropCheckpointRecordsEmptyAndInvalidLogs(t *testing.T) {
	t.Parallel()
	empty := New(t.TempDir())
	if err := os.MkdirAll(empty.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty.CheckpointPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if dropped, err := empty.DropCheckpointRecords(map[uint64]bool{1: true}); err != nil || dropped != 0 {
		t.Fatalf("empty log drop = %d, %v", dropped, err)
	}

	invalid := New(t.TempDir())
	if err := os.MkdirAll(invalid.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(invalid.CheckpointPath(), []byte("{bad}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.DropCheckpointRecords(map[uint64]bool{1: true}); err == nil {
		t.Fatal("invalid checkpoint log was rewritten")
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
		func() model.Checkpoint {
			value := validCheckpoint(1)
			value.Kind = model.CheckpointKindShellPre
			value.Type = model.AuthorAI
			value.Files = nil
			return value
		}(),
		func() model.Checkpoint {
			value := validCheckpoint(1)
			value.Kind = model.CheckpointKindShellPost
			value.Type = model.AuthorHuman
			return value
		}(),
	}
	for i, record := range tests {
		if err := (New(t.TempDir())).AppendCheckpoint(record); err == nil {
			t.Fatalf("case %d accepted invalid checkpoint", i)
		}
	}
}

func TestShellCheckpointPathLimit(t *testing.T) {
	t.Parallel()
	for _, count := range []int{500, 501} {
		count := count
		t.Run(fmt.Sprintf("%d files", count), func(t *testing.T) {
			t.Parallel()
			record := validCheckpoint(1)
			record.Kind = model.CheckpointKindShellPre
			record.Files = make([]model.Snapshot, count)
			for index := range record.Files {
				record.Files[index] = model.Snapshot{
					Path:   fmt.Sprintf("file-%03d.go", index),
					Exists: true,
					Blob:   "abcd1234",
				}
			}
			err := New(t.TempDir()).AppendCheckpoint(record)
			if count == 500 && err != nil {
				t.Fatalf("AppendCheckpoint(500) = %v", err)
			}
			if count == 501 && err == nil {
				t.Fatal("AppendCheckpoint(501) accepted oversized shell checkpoint")
			}
		})
	}
}

func TestCheckpointValidationRejectsGitAdministrativePath(t *testing.T) {
	t.Parallel()
	record := validCheckpoint(1)
	record.Files[0].Path = "dir/.git/config"
	if err := New(t.TempDir()).AppendCheckpoint(record); err == nil {
		t.Fatal("AppendCheckpoint accepted path containing .git component")
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

func TestStateRoundTripKeepsLanes(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	state, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	state.LastAnnotatedCommit = "abcd1234"
	state.Pending.BaseCommit = state.LastAnnotatedCommit
	state.Lanes["refs/heads/main"] = map[string]uint64{
		"seq:1": 4,
		"seq:2": 7,
	}
	if err := value.WriteState(state); err != nil {
		t.Fatal(err)
	}
	got, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Lanes) != 1 ||
		got.Lanes["refs/heads/main"]["seq:1"] != 4 ||
		got.Lanes["refs/heads/main"]["seq:2"] != 7 {
		t.Fatalf("ReadState() lanes = %+v", got.Lanes)
	}
	data, err := os.ReadFile(value.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	first := strings.Index(string(data), `"seq:1"`)
	second := strings.Index(string(data), `"seq:2"`)
	if first < 0 || second < 0 || first > second {
		t.Fatalf("state lanes are not serialized in sorted order: %s", data)
	}
}

func TestStateMigratesVersionOneInMemory(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"last_annotated_commit":"abcd1234","last_checkpoint_seq":42,` +
		`"notes_version":3,"pending":{"base_commit":"abcd1234","files":{}}}` + "\n"
	if err := os.WriteFile(value.StatePath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != model.StateVersion || state.LastCheckpointSeq != 42 ||
		state.LastAnnotatedCommit != "abcd1234" || len(state.Lanes) != 0 {
		t.Fatalf("ReadState() = %+v, want current version with the frozen floor", state)
	}
	onDisk, err := os.ReadFile(value.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(onDisk, []byte(legacy)) {
		t.Fatal("ReadState rewrote the state file; migration must stay in memory")
	}
	if err := value.WriteState(state); err != nil {
		t.Fatal(err)
	}
	onDisk, err = os.ReadFile(value.StatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(onDisk), `"version":3`) {
		t.Fatalf("WriteState persisted %q, want version 3", onDisk)
	}
}

func TestStateMigratesVersionTwoLanes(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":2,"last_checkpoint_seq":0,"notes_version":3,` +
		`"pending":{"files":{}},"lanes":{"abcd1234":7}}` + "\n"
	if err := os.WriteFile(value.StatePath(), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, migrated, err := value.ReadStateForUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if !migrated || state.Version != model.StateVersion ||
		state.Lanes[""][model.LegacyCheckpointLaneID("abcd1234")] != 7 {
		t.Fatalf("ReadStateForUpdate() = %+v, %t", state, migrated)
	}
}

func TestStateRejectsLanesInVersionOne(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	invalid := `{"version":1,"last_checkpoint_seq":0,"notes_version":3,` +
		`"pending":{"files":{}},"lanes":{"abcd1234":7}}` + "\n"
	if err := os.WriteFile(value.StatePath(), []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := value.ReadState(); err == nil ||
		!strings.Contains(err.Error(), "version 1 state contains lanes") {
		t.Fatalf("ReadState() error = %v", err)
	}
}

func TestStateRejectsInvalidLanes(t *testing.T) {
	t.Parallel()
	for _, state := range []model.State{
		func() model.State {
			value := model.NewState()
			value.Lanes["refs/heads/main"] = map[string]uint64{"not-a-lane": 4}
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Lanes["refs/heads/main"] = map[string]uint64{"seq:1": 0}
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Lanes["not-a-branch"] = map[string]uint64{"seq:1": 4}
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Lanes["refs/heads/main"] = map[string]uint64{"legacy:abcd1234": 4}
			return value
		}(),
	} {
		if err := New(t.TempDir()).WriteState(state); err == nil {
			t.Fatalf("WriteState accepted invalid lanes %+v", state.Lanes)
		}
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

// TestReadCheckpointsWarnsOnTruncatedFinalLine covers the tail guard:
// a final line cut off without a newline is reported as a warning and
// ignored instead of failing the whole read.
func TestReadCheckpointsWarnsOnTruncatedFinalLine(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := value.AppendCheckpoint(validCheckpoint(1)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		value.CheckpointPath(),
		[]byte(mustJSON(validCheckpoint(1))+"\n"+`{"version":1,"seq"`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("ReadCheckpoints() = %+v, want one record", records)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "ignored truncated final checkpoint line") {
		t.Fatalf("warnings = %v, want truncated final line warning", warnings)
	}
}

// TestAppendCheckpointRejectsUnwritableLog covers the append guard: a
// non-writable log fails the append, not a silent skip. The tail
// inspection opens the log read-write first, so the permission error
// surfaces through the tail guard.
func TestAppendCheckpointRejectsUnwritableLog(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("read-only file modes do not block appends on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	value := New(t.TempDir())
	if err := value.AppendCheckpoint(validCheckpoint(1)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(value.CheckpointPath(), 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(value.CheckpointPath(), 0o600) })
	if err := value.AppendCheckpoint(validCheckpoint(2)); err == nil ||
		!strings.Contains(err.Error(), "read checkpoint tail") {
		t.Fatalf("AppendCheckpoint(unwritable) = %v, want tail open failure", err)
	}
}
