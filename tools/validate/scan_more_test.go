package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanRepositoryFailures(t *testing.T) {
	t.Run("walk root fails", func(t *testing.T) {
		if _, err := scanRepo(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("scanRepo() = nil error, want walk failure")
		}
	})

	t.Run("file read fails", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "unreadable.txt")
		if err := os.WriteFile(path, []byte("content\n"), 0o600); err != nil {
			t.Fatalf("write unreadable file: %v", err)
		}
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("chmod unreadable file: %v", err)
		}
		if _, err := scanRepo(root); err == nil {
			t.Fatal("scanRepo() = nil error, want file read failure")
		}
	})

	t.Run("binary file is skipped", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "binary.bin")
		if err := os.WriteFile(path, []byte("text\x00secret"), 0o600); err != nil {
			t.Fatalf("write binary file: %v", err)
		}
		found, err := scanFile("binary.bin", path)
		if err != nil {
			t.Fatalf("scanFile() = %v, want nil error", err)
		}
		if len(found) != 0 {
			t.Fatalf("scanFile(binary) = %v, want no findings", found)
		}
	})
}

func TestCheckScansFailures(t *testing.T) {
	t.Run("CI template path is a file", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "internal", "ci", "templates")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create CI parent: %v", err)
		}
		if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
			t.Fatalf("write CI path: %v", err)
		}
		if err := checkScans(root); err == nil {
			t.Fatal("checkScans() = nil error, want CI path failure")
		}
	})

	t.Run("CI template contract fails", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "internal", "ci", "templates"), 0o755); err != nil {
			t.Fatalf("create CI templates: %v", err)
		}
		if err := checkScans(root); err == nil {
			t.Fatal("checkScans() = nil error, want CI contract failure")
		}
	})

	t.Run("CI template inspection fails", func(t *testing.T) {
		root := t.TempDir()
		internal := filepath.Join(root, "internal")
		if err := os.MkdirAll(internal, 0o755); err != nil {
			t.Fatalf("create internal directory: %v", err)
		}
		if err := os.Chmod(internal, 0); err != nil {
			t.Fatalf("chmod internal directory: %v", err)
		}
		if err := checkScans(root); err == nil {
			t.Fatal("checkScans() = nil error, want CI inspection failure")
		}
	})

	t.Run("repository scan fails", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "unreadable.txt")
		if err := os.WriteFile(path, []byte("content\n"), 0o600); err != nil {
			t.Fatalf("write unreadable file: %v", err)
		}
		if err := os.Chmod(path, 0); err != nil {
			t.Fatalf("chmod unreadable file: %v", err)
		}
		if err := checkScans(root); err == nil {
			t.Fatal("checkScans() = nil error, want scan failure")
		}
	})

	t.Run("placeholder finding is rendered", func(t *testing.T) {
		root := t.TempDir()
		content := "TO" + "DO: finish this\n"
		if err := os.WriteFile(filepath.Join(root, "todo.txt"), []byte(content), 0o600); err != nil {
			t.Fatalf("write placeholder file: %v", err)
		}
		err := checkScans(root)
		if err == nil || !strings.Contains(err.Error(), "placeholder") {
			t.Fatalf("checkScans() = %v, want placeholder finding", err)
		}
	})
}
