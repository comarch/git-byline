package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCheckpointRecordsEditsInOtherWorktrees runs the agent hook in the main
// checkout, like after cd "$FACTORY_PROJECT_DIR", while the agent edits a
// file in a linked worktree of the same repository.
func TestCheckpointRecordsEditsInOtherWorktrees(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		place func(t *testing.T, root string) string
	}{
		{"nested", func(_ *testing.T, root string) string { return filepath.Join(root, ".worktrees", "wt") }},
		{"sibling", func(t *testing.T, _ string) string { return filepath.Join(appResolvedDir(t), "wt") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := appRepo(t)
			appWrite(t, root, ".gitignore", ".worktrees/\n")
			appWrite(t, root, "a.txt", "a1\n")
			appCommit(t, root, "chore: init")
			now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			if code, _, _, err := appRun(root, now, nil, "annotate"); code != ExitSuccess || err != nil {
				t.Fatalf("annotate base = %d, %v", code, err)
			}
			worktree := test.place(t, root)
			appGit(t, root, "worktree", "add", "-q", "-b", "feat", worktree)
			payload := factoryEditPayload(filepath.Join(worktree, "a.txt"))

			code, _, stderr, err := appRun(root, now, strings.NewReader(payload),
				"checkpoint", "portable-factory", "--type", "human", "--hook-input", "stdin")
			if code != ExitSuccess || err != nil || stderr != "" {
				t.Fatalf("human checkpoint = %d, %q, %v", code, stderr, err)
			}
			appWrite(t, worktree, "a.txt", "a1\na2 agent\n")
			code, _, stderr, err = appRun(root, now.Add(time.Second), strings.NewReader(payload),
				"checkpoint", "portable-factory", "--type", "ai", "--hook-input", "stdin")
			if code != ExitSuccess || err != nil || stderr != "" {
				t.Fatalf("ai checkpoint = %d, %q, %v", code, stderr, err)
			}

			appGit(t, worktree, "commit", "-qam", "feat: agent line")
			if code, _, _, err := appRun(worktree, now, nil, "annotate"); code != ExitSuccess || err != nil {
				t.Fatalf("annotate = %d, %v", code, err)
			}
			code, stdout, stderr, err := appRun(worktree, now, nil, "blame", "a.txt")
			if code != ExitSuccess || err != nil || stderr != "" {
				t.Fatalf("blame = %d, %q, %v", code, stderr, err)
			}
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			if len(lines) != 2 || !strings.HasPrefix(lines[0], "human:") ||
				!strings.HasPrefix(lines[1], "ai:factory/sim-model") {
				t.Fatalf("blame = %q, want the agent line attributed to factory/sim-model", stdout)
			}
		})
	}
}

func TestCheckpointRejectsPathsOutsideRepositoryWorktrees(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "a.txt", "a1\n")
	appCommit(t, root, "chore: init")
	other := appRepo(t)
	appWrite(t, other, "a.txt", "other\n")
	appCommit(t, other, "chore: init")

	code, _, stderr, err := appRun(root, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		strings.NewReader(factoryEditPayload(filepath.Join(other, "a.txt"))),
		"checkpoint", "portable-factory", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || !strings.Contains(stderr, "absolute path escapes the worktree") {
		t.Fatalf("checkpoint = %d, %q, %v; want a skipped path", code, stderr, err)
	}
	for _, dir := range []string{root, other} {
		if _, err := os.Stat(filepath.Join(dir, ".git", "byline", "checkpoints.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("checkpoint log in %s: %v, want none", dir, err)
		}
	}
}

// factoryEditPayload builds a Factory Edit hook payload for one absolute path.
// Backslashes in a Windows path are not JSON escapes, so the path uses slashes.
func factoryEditPayload(path string) string {
	return `{"session_id":"s1","model":"sim-model","tool_name":"Edit","tool_input":{"file_path":"` +
		filepath.ToSlash(path) + `"}}`
}

func appResolvedDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
