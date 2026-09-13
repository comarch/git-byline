package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/provenance"
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
	appWrite(t, root, "second.txt", "manual\n")
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
	dashboardPath := filepath.Join(t.TempDir(), "dashboard.html")
	code, stdout, stderr, err = appRun(root, now, nil, "dashboard", "--output", dashboardPath, "file.txt")
	if code != ExitSuccess || err != nil || stdout != dashboardPath+"\n" || stderr != "" {
		t.Fatalf("dashboard file = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	info, err := os.Stat(dashboardPath)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("dashboard mode = %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(dashboardPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("file.txt")) ||
		!bytes.Contains(data, []byte(">2</strong>")) ||
		!bytes.Contains(data, []byte("attributed lines")) ||
		!bytes.Contains(data, []byte("droid/unknown")) ||
		bytes.Contains(data, []byte("https://")) {
		t.Fatalf("dashboard content missing expected data")
	}
	allPath := filepath.Join(t.TempDir(), "all.html")
	code, stdout, stderr, err = appRun(root, now, nil, "dashboard", "--output", allPath)
	if code != ExitSuccess || err != nil || stdout != allPath+"\n" || stderr != "" {
		t.Fatalf("dashboard all = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	allData, err := os.ReadFile(allPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(allData, []byte("second.txt")) ||
		!bytes.Contains(allData, []byte(">2</strong><span>files")) {
		t.Fatalf("dashboard all content missing second file")
	}
	code, stdout, stderr, err = appRun(root, now, nil, "dashboard", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("dashboard temp = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	tempPath := strings.TrimSpace(stdout)
	t.Cleanup(func() { _ = os.Remove(tempPath) })
	if !strings.HasSuffix(tempPath, ".html") {
		t.Fatalf("dashboard temp path = %q", tempPath)
	}
	if _, err := os.Stat(tempPath); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRefusesExistingOutput(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "content\n")
	appCommit(t, root, "base")
	if code, _, _, err := appRun(root, time.Time{}, nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "dashboard.html")
	const existing = "keep\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err := appRun(root, time.Time{}, nil, "dashboard", "--output", path)
	if code != ExitFailure || err == nil {
		t.Fatalf("dashboard overwrite = %d, %v", code, err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != existing {
		t.Fatalf("existing dashboard changed to %q", data)
	}
	for _, output := range []string{"bad\nname.html", "bad\tname.html"} {
		code, _, _, err := appRun(root, time.Time{}, nil, "dashboard", "--output", output)
		if code != ExitFailure || err == nil {
			t.Fatalf("dashboard output %q = %d, %v", output, code, err)
		}
	}
	code, _, _, err = appRun(root, time.Time{}, nil, "dashboard", "--output", "-")
	if code != ExitFailure || err == nil {
		t.Fatalf("dashboard stdout output = %d, %v", code, err)
	}
}

func TestDashboardRangeModeRendersAggregateWithoutSourceLines(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appWrite(t, root, "older-only.txt", "older-only-source-string\n")
	appCommit(t, root, "one")
	first := appHead(t, root)
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appWrite(t, root, "second.txt", "three\n")
	appCommit(t, root, "two")
	second := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	olderBlob, exists, err := repo.BlobID(first, "older-only.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", olderBlob, exists, err)
	}
	writeDashboardTestNote(t, repo, first, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"older-only.txt": {
				Blob: olderBlob,
				Ranges: []model.Range{{Start: 1, End: 1, Attribution: model.Attribution{
					Author: model.AuthorHuman,
				}}},
			},
		},
	})
	blob, exists, err := repo.BlobID(second, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	secondBlob, exists, err := repo.BlobID(second, "second.txt")
	if err != nil || !exists {
		t.Fatalf("second BlobID = %q, %t, %v", secondBlob, exists, err)
	}
	writeDashboardTestNote(t, repo, second, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "droid", Model: "model",
					}},
				},
			},
			"second.txt": {
				Blob: secondBlob,
				Ranges: []model.Range{{Start: 1, End: 1, Attribution: model.Attribution{
					Author: model.AuthorHumanOverride, Agent: "droid", Model: "model",
				}}},
			},
		},
	})

	path := filepath.Join(t.TempDir(), "range.html")
	code, stdout, stderr, err := appRun(root, zeroTime(), nil,
		"dashboard", "--range", first+".."+second, "--output", path)
	if code != ExitSuccess || err != nil || stdout != path+"\n" || stderr != "" {
		t.Fatalf("dashboard range = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("range dashboard mode = %o", info.Mode().Perm())
	}
	text := string(data)
	for _, want := range []string{
		"Commit trend",
		"Author classes",
		"file.txt",
		"second.txt",
		"droid",
		"model",
		"human-override",
		"range " + first + ".." + second,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("range dashboard missing %q", want)
		}
	}
	if strings.Contains(text, "one\ntwo") || strings.Contains(text, "three\n") {
		t.Fatal("range dashboard rendered source lines")
	}

	repoPath := filepath.Join(t.TempDir(), "repo.html")
	code, stdout, stderr, err = appRun(root, zeroTime(), nil,
		"dashboard", "--repo", "--output", repoPath)
	if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
		t.Fatalf("dashboard repo = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	repoData, err := os.ReadFile(repoPath)
	if err != nil {
		t.Fatal(err)
	}
	repoText := string(repoData)
	if !strings.Contains(repoText, "older-only.txt") {
		t.Fatal("repository dashboard missing older commit content")
	}
	if strings.Contains(repoText, "older-only-source-string") {
		t.Fatal("repository dashboard rendered source lines")
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil,
		"dashboard", "--range", first+".."+second, "file.txt")
	if code != ExitUsage || err == nil || stdout != "" ||
		!strings.Contains(stderr, "cannot be combined") {
		t.Fatalf("dashboard range file = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func writeDashboardTestNote(t *testing.T, repo *gitcmd.Repo, commit string, note model.Note) {
	t.Helper()
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
}

func TestBlameTextRendersHumanOverrideMetadata(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "human replacement\n")
	appCommit(t, root, "override")
	head := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{
						Author: model.AuthorHumanOverride, Agent: "droid", Model: "model",
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil, "blame", "file.txt")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "human-override:droid/model") {
		t.Fatalf("blame = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestDashboardTempOutputRemainsReserved(t *testing.T) {
	t.Parallel()
	file, path, err := createDashboardOutput(&Env{}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	other, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if other != nil {
		_ = other.Close()
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("second create error = %v, want file exists", err)
	}
	if err := writeDashboard(file, path, []byte("report")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "report" {
		t.Fatalf("dashboard data = %q, %v", data, err)
	}
}

func TestHookCommands(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "droid", "--git", "--project")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "updated ") ||
		!strings.Contains(stdout, "attribution notes will be pushed automatically") {
		t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".factory", "hooks.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git", "hooks", "pre-push")); err != nil {
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
		{"checkpoint invalid owner", `{}`, []string{"checkpoint", "droid", "--managed-by", "other", "--type", "ai", "--hook-input", "stdin"}, ExitUsage},
		{"checkpoint unknown flag", `{}`, []string{"checkpoint", "droid", "--bad"}, ExitUsage},
		{"agent v1 explicit type", `{}`, []string{"checkpoint", "agent-v1", "--type", "ai", "--hook-input", "stdin"}, ExitUsage},
		{"annotate argument", "", []string{"annotate", "extra"}, ExitUsage},
		{"blame missing file", "", []string{"blame"}, ExitUsage},
		{"blame unknown flag", "", []string{"blame", "--bad", "file"}, ExitUsage},
		{"status argument", "", []string{"status", "extra"}, ExitUsage},
		{"dashboard too many files", "", []string{"dashboard", "one", "two"}, ExitUsage},
		{"dashboard unknown flag", "", []string{"dashboard", "--bad"}, ExitUsage},
		{"local notes without git", "", []string{"install-hooks", "--agent", "droid", "--local-notes"}, ExitUsage},
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
	for _, args := range [][]string{{"blame", "-h"}, {"status", "--help"}, {"dashboard", "-h"}, {"install-hooks", "-h"}, {"uninstall", "--help"}} {
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

func TestCheckpointInputDeadline(t *testing.T) {
	oldTimeout := checkpointInputTimeoutNanos.Swap(int64(10 * time.Millisecond))
	defer checkpointInputTimeoutNanos.Store(oldTimeout)

	reader, writer := io.Pipe()
	defer writer.Close()
	root := t.TempDir()
	done := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, _, _, err := appRun(root, time.Time{}, reader,
			"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
		done <- struct {
			code int
			err  error
		}{code: code, err: err}
	}()
	select {
	case result := <-done:
		if result.code != ExitFailure || result.err == nil ||
			!strings.Contains(result.err.Error(), "timed out") {
			t.Fatalf("checkpoint deadline = %d, %v", result.code, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("checkpoint input did not time out")
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

func TestAnnotatePrintsSkippedDuringRebase(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	appGit(t, root, "checkout", "-q", "-b", "feat")
	appWrite(t, root, "feat.txt", "feat\n")
	appCommit(t, root, "feat")
	appGit(t, root, "checkout", "-q", "main")
	appWrite(t, root, "main.txt", "main\n")
	appCommit(t, root, "main work")
	appGit(t, root, "checkout", "-q", "feat")
	appGit(t, root, "rebase", "-q", "main")

	code, stdout, stderr, err := appRun(root, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), nil, "annotate")
	if code != ExitSuccess || err != nil {
		t.Fatalf("annotate during rebase = %d, %v", code, err)
	}
	if !strings.Contains(stdout, "skipped ") || strings.Contains(stdout, "annotated ") {
		t.Fatalf("annotate output = %q, want skipped", stdout)
	}
	if !strings.Contains(stderr, "rebase replay") {
		t.Fatalf("annotate warnings = %q, want rebase replay hint", stderr)
	}
}
