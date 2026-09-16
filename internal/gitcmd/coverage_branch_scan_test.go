package gitcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestNormalizeWorktreePathLoop covers the absolute path that cannot be
// resolved because it ends in a symlink loop.
func TestNormalizeWorktreePathLoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink loop creation is not portable on Windows")
	}
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.NormalizeWorktreePath(loop); err == nil ||
		!strings.Contains(err.Error(), "resolve absolute path") {
		t.Fatalf("NormalizeWorktreePath(loop) = %v, want resolve absolute path error", err)
	}
}

// TestBranchScannerContainmentNoise covers the non-empty stderr report,
// the empty output line skip, and the invalid branch reference report
// from branch containment scans.
func TestBranchScannerContainmentNoise(t *testing.T) {
	repo := coverageOutputRepo(t, "refs/heads/feature\n", "noise on stderr", 0)
	if _, _, err := repo.NewBranchScanner().BranchesContaining(coverageOID); err == nil ||
		!strings.Contains(err.Error(), "check branch containment reported") {
		t.Fatalf("BranchesContaining(stderr) = %v, want reported error", err)
	}

	// An interior blank line is skipped instead of failing ref validation.
	repo = coverageOutputRepo(t, "refs/heads/feature\n\nrefs/heads/main\n", "", 0)
	branches, ok, err := repo.NewBranchScanner().BranchesContaining(coverageOID)
	if err != nil || !ok || len(branches) != 2 {
		t.Fatalf("BranchesContaining(blank line) = %v, %t, %v", branches, ok, err)
	}

	repo = coverageOutputRepo(t, "refs/heads/bad..name\n", "", 0)
	if _, _, err := repo.NewBranchScanner().BranchesContaining(coverageOID); err == nil ||
		!strings.Contains(err.Error(), "invalid branch reference") {
		t.Fatalf("BranchesContaining(bad refname) = %v, want invalid branch reference", err)
	}
}
