package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadCheckpointsWarnsOnTruncatedRecord covers the strict decode
// warning for a final line whose header decodes but whose body was cut
// short without a trailing newline.
func TestReadCheckpointsWarnsOnTruncatedRecord(t *testing.T) {
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := value.CheckpointPath()
	// The line starts a version-1 checkpoint record so the header decodes,
	// but the body is cut mid-string and there is no trailing newline.
	if err := os.WriteFile(path, []byte(`{"version":1,"ts":"`), 0o600); err != nil {
		t.Fatal(err)
	}
	records, warnings, err := value.ReadCheckpoints()
	if err != nil {
		t.Fatalf("ReadCheckpoints() = %v, want truncated warning", err)
	}
	if len(records) != 0 || len(warnings) != 1 ||
		!strings.Contains(warnings[0], "ignored truncated final checkpoint line 1") {
		t.Fatalf("ReadCheckpoints() = %v, %v", records, warnings)
	}
	if stat, statErr := os.Stat(filepath.Join(value.Dir, "checkpoints.jsonl")); statErr != nil || stat.Size() == 0 {
		t.Fatalf("checkpoint log survived = %v, %v", stat, statErr)
	}
}
