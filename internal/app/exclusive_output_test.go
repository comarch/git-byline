package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExclusiveOutputParentFailures covers the dashboard and export
// output paths whose parent exists as a regular file.
func TestExclusiveOutputParentFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	parent := filepath.Join(root, "parent-file")
	if err := os.WriteFile(parent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "--output", filepath.Join(parent, "out.html")); code != ExitFailure || err == nil {
		t.Fatalf("dashboard output under file parent = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "export", "--format", "agent-trace", "--output", filepath.Join(parent, "trace.json")); code != ExitFailure || err == nil {
		t.Fatalf("export output under file parent = %d, %v", code, err)
	}
}
