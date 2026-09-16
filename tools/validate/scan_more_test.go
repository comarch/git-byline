package main

import (
	"os"
	"path/filepath"
	"runtime"
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
		root := unreadableScanRoot(t)
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
	t.Run("CI template path is a file", checkScansCITemplatePathFile)
	t.Run("CI template contract fails", checkScansCITemplateContractFailure)
	t.Run("CI template inspection fails", checkScansCITemplateInspectionFailure)
	t.Run("repository scan fails", checkScansRepositoryFailure)
	t.Run("placeholder finding is rendered", checkScansPlaceholderFinding)
}

func checkScansCITemplatePathFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "internal", "ci", "templates")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create CI parent: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write CI path: %v", err)
	}
	requireCheckScansError(t, root, "checkScans() = nil error, want CI path failure")
}

func checkScansCITemplateContractFailure(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "internal", "ci", "templates"), 0o755); err != nil {
		t.Fatalf("create CI templates: %v", err)
	}
	requireCheckScansError(t, root, "checkScans() = nil error, want CI contract failure")
}

func checkScansCITemplateInspectionFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not enforced on Windows")
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	internal := filepath.Join(root, "internal")
	if err := os.MkdirAll(internal, 0o755); err != nil {
		t.Fatalf("create internal directory: %v", err)
	}
	if err := os.Chmod(internal, 0); err != nil {
		t.Fatalf("chmod internal directory: %v", err)
	}
	requireCheckScansError(t, root, "checkScans() = nil error, want CI inspection failure")
}

func checkScansRepositoryFailure(t *testing.T) {
	root := unreadableScanRoot(t)
	requireCheckScansError(t, root, "checkScans() = nil error, want scan failure")
}

func checkScansPlaceholderFinding(t *testing.T) {
	root := t.TempDir()
	content := "TO" + "DO: finish this\n"
	if err := os.WriteFile(filepath.Join(root, "todo.txt"), []byte(content), 0o600); err != nil {
		t.Fatalf("write placeholder file: %v", err)
	}
	err := requireCheckScansError(t, root, "checkScans() = nil error, want placeholder finding")
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("checkScans() = %v, want placeholder finding", err)
	}
}

func requireCheckScansError(t *testing.T, root, failure string) error {
	t.Helper()
	err := checkScans(root)
	if err == nil {
		t.Fatal(failure)
	}
	return err
}

func unreadableScanRoot(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are not enforced on Windows")
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	root := t.TempDir()
	path := filepath.Join(root, "unreadable.txt")
	if err := os.WriteFile(path, []byte("content\n"), 0o600); err != nil {
		t.Fatalf("write unreadable file: %v", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatalf("chmod unreadable file: %v", err)
	}
	return root
}
