package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"1.18.1", "1.18.1"},
		{"promptscript 1.18.1 darwin-arm64 node/v24.0.0", "1.18.1"},
		{"v2.0.0-rc.1", "2.0.0"},
		{"no version here", ""},
	}
	for _, tt := range tests {
		if got := parseSemver(tt.in); got != tt.want {
			t.Errorf("parseSemver(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestCheckPromptScriptOnRepo runs the full PromptScript stage against the
// real repository: pinned CLI, strict validation, and generated-file
// drift. It skips on machines without the CLI so the test suite stays
// green for contributors who have not installed it.
func TestCheckPromptScriptOnRepo(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if err := checkPromptScript(root); err != nil {
		t.Errorf("checkPromptScript(repo) = %v, want nil", err)
	}
}

func TestCheckPromptScriptMissingFiles(t *testing.T) {
	t.Parallel()
	err := checkPromptScript(t.TempDir())
	if err == nil {
		t.Fatal("checkPromptScript(empty dir) = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want it to name the missing file", err)
	}
}

// TestCheckDriftDetectsMutation proves a modified source that no longer
// matches committed generated instructions fails the drift check.
func TestCheckDriftDetectsMutation(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	bin, err := exec.LookPath("promptscript")
	if err != nil {
		t.Fatalf("look up promptscript: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(root, ".promptscript", "project.prs")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read project.prs: %v", err)
	}
	mutated := strings.Replace(string(data), "local AI code attribution tool",
		"mutated AI code attribution tool", 1)
	if mutated == string(data) {
		t.Fatal("mutation target text not found in project.prs")
	}
	srcDir := filepath.Join(dir, ".promptscript")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", srcDir, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".promptscript"))
	if err != nil {
		t.Fatalf("read PromptScript sources: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "project.prs" || filepath.Ext(entry.Name()) != ".prs" {
			continue
		}
		fragment, err := os.ReadFile(filepath.Join(root, ".promptscript", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, entry.Name()), fragment, 0o644); err != nil {
			t.Fatalf("write %s: %v", entry.Name(), err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "project.prs"), []byte(mutated), 0o644); err != nil {
		t.Fatalf("write mutated project.prs: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(root, "promptscript.yaml"))
	if err != nil {
		t.Fatalf("read promptscript.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "promptscript.yaml"), cfg, 0o644); err != nil {
		t.Fatalf("write promptscript.yaml: %v", err)
	}
	committed, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), committed, 0o644); err != nil {
		t.Fatalf("write AGENTS.md: %v", err)
	}

	err = checkDrift(bin, dir)
	if err == nil {
		t.Fatal("checkDrift(mutated source) = nil, want drift error")
	}
	if !strings.Contains(err.Error(), "out of sync") {
		t.Errorf("error = %v, want it to report the drift", err)
	}
}

func TestCheckDriftMissingGeneratedFile(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	bin, err := exec.LookPath("promptscript")
	if err != nil {
		t.Fatalf("look up promptscript: %v", err)
	}
	dir := t.TempDir()
	err = checkDrift(bin, dir)
	if err == nil {
		t.Fatal("checkDrift(empty dir) = nil, want error")
	}
}
