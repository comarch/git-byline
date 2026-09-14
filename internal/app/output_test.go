package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failingOutputFile injects deterministic write, sync, and close failures.
type failingOutputFile struct {
	writeErr error
	syncErr  error
	closeErr error
}

func (f *failingOutputFile) Write([]byte) (int, error) { return 0, f.writeErr }
func (f *failingOutputFile) Sync() error               { return f.syncErr }
func (f *failingOutputFile) Close() error              { return f.closeErr }

func TestWriteExclusiveOutputFailuresRemoveFile(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("injected failure")
	tests := []struct {
		name   string
		file   *failingOutputFile
		prefix string
	}{
		{"write failure", &failingOutputFile{writeErr: sentinel}, "write dashboard "},
		{"sync failure", &failingOutputFile{syncErr: sentinel}, "sync dashboard "},
		{"close failure", &failingOutputFile{closeErr: sentinel}, "close dashboard "},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "dashboard.html")
			if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := writeExclusiveOutput(test.file, path, []byte("report"), "dashboard")
			if !errors.Is(err, sentinel) {
				t.Fatalf("writeExclusiveOutput error = %v, want wrapped sentinel", err)
			}
			if !strings.HasPrefix(err.Error(), test.prefix) {
				t.Errorf("error = %q, want prefix %q", err, test.prefix)
			}
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("output file remains after failure: %v", statErr)
			}
		})
	}
}
