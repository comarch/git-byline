//go:build darwin || linux

package gitcmd

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func TestTempFileWriteFailure(t *testing.T) {
	dir := t.TempDir()
	var previous syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous); err != nil {
		t.Fatal(err)
	}
	limited := previous
	limited.Cur = 1
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		signal.Reset(syscall.SIGXFSZ)
		t.Fatal(err)
	}
	path, writeErr := tempFile(dir, "limited-", []byte("payload"))
	restoreErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous)
	signal.Reset(syscall.SIGXFSZ)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if writeErr == nil || path != "" {
		t.Fatalf("tempFile() = %q, %v, want a write error", path, writeErr)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal(err)
	}
}
