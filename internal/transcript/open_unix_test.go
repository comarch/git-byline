//go:build unix

package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenVerifiedSessionFileRejectsMissingDirectoryAndSymlink covers the
// precheck rejections that need no race: missing paths fail Lstat, and
// directories or symlinks are rejected as non-regular before the open.
func TestOpenVerifiedSessionFileRejectsMissingDirectoryAndSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, _, err := openVerifiedSessionFile(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Fatal("openVerifiedSessionFile accepted a missing file")
	}
	if _, _, err := openVerifiedSessionFile(dir); err == nil {
		t.Fatal("openVerifiedSessionFile accepted a directory")
	}
	regular := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(regular, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openVerifiedSessionFile(symlink); err == nil {
		t.Fatal("openVerifiedSessionFile followed a final symlink")
	}
	unreadable := filepath.Join(dir, "unreadable.jsonl")
	if err := os.WriteFile(unreadable, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o600) })
	file, _, err := openVerifiedSessionFile(unreadable)
	if err == nil {
		_ = file.Close()
		t.Skip("filesystem permits opening mode-zero files")
	}
}

// TestResolveModelSkipsCorruptSidecar covers the settings decode failure:
// a corrupt sidecar is an unresolved model, never a hard error.
func TestResolveModelSkipsCorruptSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	transcript := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(transcript, []byte("{\"message\":{\"role\":\"user\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, err := ResolveModel(transcript)
	if err != nil {
		t.Fatalf("ResolveModel() = %v", err)
	}
	if model != "" {
		t.Fatalf("ResolveModel() = %q, want empty", model)
	}
}
