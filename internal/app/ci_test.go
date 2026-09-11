package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCIInstall(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Dir:    root,
	}
	code, err := Run([]string{"ci", "install", "--provider", "github"}, env)
	if code != ExitSuccess || err != nil {
		t.Fatalf("ci install = %d, %v\n%s", code, err, stderr.String())
	}
	path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("installed workflow: %v", err)
	}
	stdout.Reset()
	code, err = Run([]string{"ci", "install", "--provider", "github"}, env)
	if code != ExitSuccess || err != nil {
		t.Fatalf("repeat ci install = %d, %v", code, err)
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Fatalf("repeat output = %q", stdout.String())
	}
}

func TestRunCIRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runTestGit(t, root, "init", "--initial-branch=main")
	runTestGit(t, root, "config", "user.name", "Test User")
	runTestGit(t, root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "base")
	base := strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", ".")
	runTestGit(t, root, "commit", "-m", "source")
	source := strings.TrimSpace(runTestGit(t, root, "rev-parse", "HEAD"))
	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Dir:    root,
	}
	code, err := Run([]string{
		"ci", "run", "--provider", "github",
		"--base", base, "--source", source, "--target", source, "--mode", "squash",
	}, env)
	if code != ExitSuccess || err != nil {
		t.Fatalf("ci run = %d, %v\n%s", code, err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "reconstructed squash attribution") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunCIUsageErrors(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"ci"},
		{"ci", "install"},
		{"ci", "install", "--provider", "unknown"},
		{"ci", "bogus", "--provider", "github"},
		{"ci", "install", "--provider", "github", "--mode", "squash"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			env := &Env{
				Stdin:  strings.NewReader(""),
				Stdout: &stdout,
				Stderr: &stderr,
				Dir:    t.TempDir(),
			}
			code, err := Run(args, env)
			if code != ExitUsage || err == nil {
				t.Fatalf("Run(%q) = %d, %v, want usage error", args, code, err)
			}
		})
	}
}

func runTestGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
