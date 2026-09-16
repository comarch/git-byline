//go:build darwin || linux

package gitcmd

import (
	"os"
	"testing"
)

func TestTempFileWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
	path, writeErr := tempFile(dir, "limited-", []byte("payload"))
	if writeErr == nil || path != "" {
		t.Fatalf("tempFile() = %q, %v, want a write error", path, writeErr)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
