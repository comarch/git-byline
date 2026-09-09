package lock

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAcquireReleaseAndContention(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state", "operation.lock")
	first, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "pid ") {
		t.Fatalf("String() = %q", first.String())
	}
	if _, err := Acquire(path, 50*time.Millisecond); err == nil {
		t.Fatal("second Acquire succeeded while lock was held")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if first.String() != "released" {
		t.Fatalf("String() after release = %q", first.String())
	}
	second, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) == "" {
		t.Fatal("lock metadata is empty")
	}
}

func TestAcquireInvalidParent(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(filepath.Join(parent, "lock"), time.Second); err == nil {
		t.Fatal("Acquire succeeded below a regular file")
	}
	var file *File
	if err := file.Release(); err != nil {
		t.Fatal(err)
	}
}
