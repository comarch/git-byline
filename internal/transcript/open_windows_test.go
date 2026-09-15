//go:build windows

package transcript

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSessionFileDoesNotFollowSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	file, err := openSessionFile(link)
	if err != nil {
		t.Fatalf("openSessionFile(symlink) error = %v", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().IsRegular() {
		t.Fatal("opened symlink reports as a regular file, want a reparse point")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64))
	if err == nil && string(data) == "target" {
		t.Fatalf("openSessionFile followed the symlink and read %q", data)
	}
}
