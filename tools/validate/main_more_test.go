package main

import "testing"

func TestRepoRootFailurePaths(t *testing.T) {
	t.Run("go command fails", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := repoRoot(); err == nil {
			t.Fatal("repoRoot() = nil error, want go command failure")
		}
	})

	t.Run("module is disabled", func(t *testing.T) {
		t.Setenv("GO111MODULE", "off")
		root, err := repoRoot()
		if err == nil {
			t.Fatalf("repoRoot() = %q, want module error", root)
		}
		if root != "" {
			t.Errorf("repoRoot() root = %q, want empty root", root)
		}
	})
}
