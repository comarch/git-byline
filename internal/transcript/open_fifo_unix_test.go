//go:build unix

package transcript

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestOpenVerifiedSessionFileRejectsFIFO covers the non-regular rejection
// for a named pipe swapped in as a session file. The nonblocking open
// succeeds on a FIFO so the regular-file check is what must fire.
func TestOpenVerifiedSessionFileRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.log")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	file, _, err := openVerifiedSessionFile(path)
	if file != nil {
		_ = file.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("openVerifiedSessionFile(fifo) = %v, want not a regular file", err)
	}
}
