//go:build windows

package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenVerifiedSessionFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.jsonl")
	if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openVerifiedSessionFile(link); err == nil {
		t.Fatal("openVerifiedSessionFile(symlink) succeeded, want rejection")
	}
}
