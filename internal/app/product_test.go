package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrwogu/git-byline/internal/model"
	"github.com/mrwogu/git-byline/internal/provenance"
)

func TestProductCommandFlow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	code, stdout, stderr, err := appRun(root, now, nil, "annotate")
	if code != ExitSuccess || err != nil || !strings.Contains(stdout, "annotated ") || stderr != "" {
		t.Fatalf("annotate = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	humanPayload := `{"session_id":"s","tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, stdout, stderr, err = appRun(root, now, strings.NewReader(humanPayload),
		"checkpoint", "droid", "--type", "human", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("human checkpoint = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	appWrite(t, root, "file.txt", "base\nai\n")
	code, _, stderr, err = appRun(root, now.Add(time.Second), strings.NewReader(humanPayload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("ai checkpoint = %d, %q, %v", code, stderr, err)
	}
	appCommit(t, root, "ai")
	if code, _, _, err = appRun(root, now, nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatalf("second annotate = %d, %v", code, err)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "blame", "file.txt", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame = %d, %q, %v", code, stderr, err)
	}
	var blame provenance.BlameResult
	if err := json.Unmarshal([]byte(stdout), &blame); err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 2 || blame.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("blame = %+v", blame)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "status", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("status = %d, %q, %v", code, stderr, err)
	}
	var status provenance.StatusResult
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatal(err)
	}
	if status.Head == "" || status.Head != status.LastAnnotatedCommit {
		t.Fatalf("status = %+v", status)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "blame", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "ai:droid/unknown") {
		t.Fatalf("text blame = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, stdout, stderr, err = appRun(root, now, nil, "status")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "Pending checkpoints: 0") {
		t.Fatalf("text status = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestHookCommands(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "droid", "--git", "--project")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "updated ") {
		t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".factory", "hooks.json")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err = appRun(root, time.Time{}, nil,
		"uninstall", "--agent", "droid", "--git", "--project")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "updated ") {
		t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestProductCommandUsageAndFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	tests := []struct {
		name  string
		input string
		args  []string
		code  int
	}{
		{"checkpoint missing preset", "", []string{"checkpoint"}, ExitUsage},
		{"checkpoint missing type", `{}`, []string{"checkpoint", "droid", "--hook-input", "stdin"}, ExitUsage},
		{"checkpoint invalid input source", `{}`, []string{"checkpoint", "droid", "--type", "ai"}, ExitUsage},
		{"checkpoint unknown flag", `{}`, []string{"checkpoint", "droid", "--bad"}, ExitUsage},
		{"agent v1 explicit type", `{}`, []string{"checkpoint", "agent-v1", "--type", "ai", "--hook-input", "stdin"}, ExitUsage},
		{"annotate argument", "", []string{"annotate", "extra"}, ExitUsage},
		{"blame missing file", "", []string{"blame"}, ExitUsage},
		{"blame unknown flag", "", []string{"blame", "--bad", "file"}, ExitUsage},
		{"status argument", "", []string{"status", "extra"}, ExitUsage},
		{"install conflicting scope", "", []string{"install-hooks", "--user", "--project"}, ExitUsage},
		{"install unknown agent", "", []string{"install-hooks", "--agent", "other"}, ExitUsage},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			code, _, _, err := appRun(root, time.Time{}, strings.NewReader(test.input), test.args...)
			if code != test.code || err == nil {
				t.Fatalf("Run(%v) = %d, %v, want %d, error", test.args, code, err, test.code)
			}
		})
	}
	for _, args := range [][]string{{"blame", "-h"}, {"status", "--help"}, {"install-hooks", "-h"}, {"uninstall", "--help"}} {
		code, stdout, stderr, err := appRun(root, time.Time{}, nil, args...)
		if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
			t.Fatalf("Run(%v) help = %d, %q, %q, %v", args, code, stdout, stderr, err)
		}
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, strings.NewReader("{}"),
		"checkpoint", "droid", "-h")
	if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
		t.Fatalf("checkpoint help = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, _, _, err = appRun(t.TempDir(), time.Time{}, nil, "status")
	if code != ExitFailure || err == nil {
		t.Fatalf("status outside repo = %d, %v", code, err)
	}
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, stdout, stderr, err = appRun(t.TempDir(), time.Time{}, strings.NewReader(payload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("checkpoint outside repo = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	empty := appRepo(t)
	code, stdout, stderr, err = appRun(empty, time.Time{}, nil, "status")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "HEAD: (none)") {
		t.Fatalf("empty status = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func appRun(root string, now time.Time, input io.Reader, args ...string) (int, string, string, error) {
	if input == nil {
		input = strings.NewReader("")
	}
	var stdout, stderr bytes.Buffer
	env := &Env{
		Stdin:  input,
		Stdout: &stdout,
		Stderr: &stderr,
		Dir:    root,
		Now:    func() time.Time { return now },
	}
	code, err := Run(args, env)
	return code, stdout.String(), stderr.String(), err
}

func appRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "init", "-b", "main")
	appGit(t, root, "config", "user.name", "Test User")
	appGit(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func appWrite(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appCommit(t *testing.T, root, message string) {
	t.Helper()
	appGit(t, root, "add", "-A")
	appGit(t, root, "commit", "-m", message)
}

func appGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
