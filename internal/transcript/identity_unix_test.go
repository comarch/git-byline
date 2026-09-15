//go:build unix

package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeFileInfo is an os.FileInfo whose Sys() is not a *syscall.Stat_t,
// covering the identity fallback for wrappers that do not expose the
// raw stat.
type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "fake" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return struct{}{} }

// TestFileIdentityOfRejectsUnknownStatShape pins the fallback: an
// FileInfo without a raw stat has no usable identity.
func TestFileIdentityOfRejectsUnknownStatShape(t *testing.T) {
	t.Parallel()
	if _, ok := fileIdentityOf(fakeFileInfo{}); ok {
		t.Fatal("fileIdentityOf accepted an unknown stat shape")
	}
}

func TestFileIdentityOfDistinguishesFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "first.jsonl")
	second := filepath.Join(dir, "second.jsonl")
	if err := os.WriteFile(first, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	pre, err := os.Lstat(first)
	if err != nil {
		t.Fatal(err)
	}
	firstID, ok := fileIdentityOf(pre)
	if !ok {
		t.Fatal("identity of a regular file is unavailable")
	}
	file, err := os.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	openedID, ok := fileIdentityOf(info)
	if !ok || openedID != firstID {
		t.Fatalf("opened identity %+v does not match precheck %+v", openedID, firstID)
	}
	other, err := os.Lstat(second)
	if err != nil {
		t.Fatal(err)
	}
	otherID, _ := fileIdentityOf(other)
	if otherID == firstID {
		t.Fatal("two different files share one identity")
	}
}
