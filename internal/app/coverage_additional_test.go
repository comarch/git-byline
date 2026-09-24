package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
)

const (
	coverageGitHub = "github"
	coverageGitAI  = "gitai"
	coverageHead   = "HEAD"
)

type coverageErrorReader struct {
	err error
}

func (r coverageErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type coverageErrorWriter struct {
	err error
}

func (w coverageErrorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func TestCoverageEnvironmentFallbacks(t *testing.T) {
	if (&Env{}).now().IsZero() {
		t.Fatal("default Env.now returned zero")
	}
	t.Setenv("PWD", "")

	// Windows cannot remove a directory the process is inside and its
	// permissions do not block working directory resolution, so both
	// sections below are unix-only behavior.
	if runtime.GOOS == "windows" {
		t.Skip("removed and unreadable working directories are unix behavior")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// A removed current directory makes working directory resolution
	// fail the same way on macOS and Linux. t.Chdir restores the real
	// working directory even when the test aborts, and its cleanup runs
	// before the TempDir cleanup, so parallel tests never inherit a
	// deleted working directory.
	removed := t.TempDir()
	t.Chdir(removed)
	if err := os.RemoveAll(removed); err != nil {
		t.Fatal(err)
	}
	// macOS still resolves a removed working directory, Linux fails
	// with ENOENT, and Git subprocesses fail on both, so only the
	// command outcomes are asserted strictly here.
	if _, err := (&Env{}).workingDir(); err == nil {
		t.Log("workingDir resolved a removed current directory")
	}
	assertRunFailsInBrokenCwd(t, "removed working directory")
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}

	// A working directory longer than PATH_MAX (1024 on macOS, 4096 on
	// Linux) fails getcwd, and more than 341 levels also defeat the
	// os.Getwd fallback, which gives up once its "../" walk reaches 1024
	// bytes. So resolution fails the same way on every unix platform and
	// this section asserts strictly. Long names keep the tree shallow,
	// because path operations slow down with depth on macOS and 2100
	// one-letter levels stalled the cleanup under load.
	deepRoot := t.TempDir()
	t.Chdir(deepRoot)
	const depth = 400
	name := strings.Repeat("x", 16)
	for index := 0; index < depth; index++ {
		if err := os.Mkdir(name, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := (&Env{}).workingDir(); err == nil {
		t.Fatal("workingDir resolved a path deeper than PATH_MAX")
	}
	if _, outputErr := resolveOutputPath(&Env{}, "report.json", "test"); outputErr == nil {
		t.Fatal("resolveOutputPath succeeded in an overlong working directory")
	}
	assertRunFailsInBrokenCwd(t, "overlong working directory")
}

func assertRunFailsInBrokenCwd(t *testing.T, context string) {
	t.Helper()
	for _, args := range [][]string{
		{"ci", "install", "--provider", coverageGitHub},
		{"install-hooks", "--agent", "none", "--git"},
		{"uninstall", "--agent", "none", "--git"},
		{"stats"},
		{"status"},
		{"blame", "file.txt"},
		{"dashboard"},
		{"annotate"},
		{"check"},
		{"verify"},
		{"export", "--format", coverageGitAI},
		{"disclosure"},
		{"rewrite", "--mode", "post-merge", "--hook-input", "stdin"},
	} {
		env := &Env{
			Stdin:  strings.NewReader(""),
			Stdout: io.Discard,
			Stderr: io.Discard,
		}
		if code, runErr := Run(args, env); code != ExitFailure || runErr == nil {
			t.Fatalf("Run(%v) in %s = %d, %v", args, context, code, runErr)
		}
	}
}

func TestCheckCoverageBranches(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\ntwo\nthree\nfour\n")
	appCommit(t, root, "content")
	head := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	writeCheckNote(t, repo, head, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 2, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "droid",
					}},
					{Start: 3, End: 4, Attribution: model.Attribution{Author: model.AuthorUntracked}},
				},
			},
		},
	})

	tests := []struct {
		name string
		dir  string
		args []string
		code int
	}{
		{name: "help", dir: root, args: []string{"check", "--help"}, code: ExitSuccess},
		{name: "bad range", dir: root, args: []string{"check", "a...b"}, code: ExitUsage},
		{name: "outside repository", dir: t.TempDir(), args: []string{"check"}, code: ExitFailure},
		{name: "unknown revision", dir: root, args: []string{"check", "missing..HEAD"}, code: ExitFailure},
		{
			name: "both policy limits",
			dir:  root,
			args: []string{"check", "--max-ai-percent", "0", "--max-untracked-percent", "0"},
			code: ExitFailure,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			code, _, _, err := appRun(test.dir, zeroTime(), nil, test.args...)
			if code != test.code || (test.code != ExitSuccess && err == nil) {
				t.Fatalf("Run(%v) = %d, %v, want %d", test.args, code, err, test.code)
			}
		})
	}
}

func TestCheckRequireNoteSortsMultipleMissingCommits(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	appWrite(t, root, "file.txt", "two\n")
	appCommit(t, root, "two")
	appWrite(t, root, "file.txt", "three\n")
	appCommit(t, root, "three")
	code, stdout, stderr, err := appRun(root, zeroTime(), nil,
		"check", "--require-note", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("missing notes = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var result checkResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Violations) != 3 {
		t.Fatalf("missing note violations = %+v", result.Violations)
	}
	if result.Violations[0].Commit >= result.Violations[1].Commit ||
		result.Violations[1].Commit >= result.Violations[2].Commit {
		t.Fatalf("missing note violations are not sorted: %+v", result.Violations)
	}
}

func TestParseCheckArgsCoverage(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"--json", "--json"},
		{"--require-note", "--require-note"},
		{"--max-ai-percent", "20", "--max-ai-percent", "30"},
		{"--max-ai-percent=20", "--max-ai-percent=30"},
		{"--max-untracked-percent", "20", "--max-untracked-percent", "30"},
		{"--max-untracked-percent=20", "--max-untracked-percent=30"},
		{"--max-untracked-percent"},
		{"--max-untracked-percent", "bad"},
		{"--max-untracked-percent=bad"},
		{"--max-ai-percent", "bad"},
		{"--max-ai-percent=bad"},
		{"--unknown"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			if _, _, err := parseCheckArgs(args); err == nil {
				t.Fatalf("parseCheckArgs(%v) returned nil error", args)
			}
		})
	}
}

func TestCheckOperationalFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missingNoteViolations(repo, "missing revision", coverageHead); err == nil {
		t.Fatal("missingNoteViolations accepted an invalid revision")
	}

	blob, err := appGitObject(t, root, []byte("broken notes ref"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGitObjectRef(t, root, bylineNotesRef, blob); err != nil {
		t.Fatal(err)
	}
	if _, err := missingNoteViolations(repo, "", ""); err == nil {
		t.Fatal("missingNoteViolations accepted a broken notes ref")
	}
}

func TestRunCIAdditionalFailures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		dir  string
	}{
		{name: "flag parse", args: []string{"ci", "run", "--bad"}, dir: t.TempDir()},
		{name: "positional after action", args: []string{"ci", "install", "--provider", coverageGitHub, "extra"}, dir: t.TempDir()},
		{
			name: "invalid revision character",
			args: []string{"ci", "run", "--provider", coverageGitHub, "--base", strings.Repeat("g", 40)},
			dir:  t.TempDir(),
		},
		{
			name: "zero revision",
			args: []string{"ci", "run", "--provider", coverageGitHub, "--base", strings.Repeat("0", 40)},
			dir:  t.TempDir(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			code, _, _, err := appRun(test.dir, zeroTime(), nil, test.args...)
			if code != ExitUsage || err == nil {
				t.Fatalf("Run(%v) = %d, %v, want usage error", test.args, code, err)
			}
		})
	}

	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err := appRun(filePath, zeroTime(), nil, "ci", "install", "--provider", coverageGitHub)
	if code != ExitFailure || err == nil {
		t.Fatalf("ci install on file = %d, %v", code, err)
	}

	code, _, _, err = appRun(t.TempDir(), zeroTime(), nil,
		"ci", "run", "--provider", coverageGitHub, "--source", strings.Repeat("1", 40))
	if code != ExitFailure || err == nil {
		t.Fatalf("ci run outside repository = %d, %v", code, err)
	}

	root = appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	code, _, _, err = appRun(root, zeroTime(), nil, "ci", "run", "--provider", coverageGitHub)
	if code != ExitFailure || err == nil {
		t.Fatalf("ci run without source = %d, %v", code, err)
	}
}

func TestValidateCIRevisionCoverage(t *testing.T) {
	t.Parallel()
	valid := strings.Repeat("a", 40)
	tests := []struct {
		name  string
		value string
		valid bool
	}{
		{name: "empty", value: "", valid: true},
		{name: "full sha one", value: valid, valid: true},
		{name: "full sha two", value: strings.Repeat("b", 64), valid: true},
		{name: "short", value: "abc"},
		{name: "invalid character", value: strings.Repeat("g", 40)},
		{name: "zero", value: strings.Repeat("0", 40)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateCIRevision("source", test.value)
			if (err == nil) != test.valid {
				t.Fatalf("validateCIRevision(%q) error = %v, valid = %t", test.value, err, test.valid)
			}
		})
	}
}

func TestUseColorTerminalBranches(t *testing.T) {
	oldNoColor, hadNoColor := os.LookupEnv("NO_COLOR")
	oldTerm, hadTerm := os.LookupEnv("TERM")
	t.Cleanup(func() {
		if hadNoColor {
			_ = os.Setenv("NO_COLOR", oldNoColor)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
		if hadTerm {
			_ = os.Setenv("TERM", oldTerm)
		} else {
			_ = os.Unsetenv("TERM")
		}
	})
	_ = os.Unsetenv("NO_COLOR")
	_ = os.Setenv("TERM", "xterm")

	if useColor(colorAuto, new(bytes.Buffer)) {
		t.Fatal("buffer unexpectedly enabled automatic color")
	}
	regular, err := os.CreateTemp(t.TempDir(), "regular")
	if err != nil {
		t.Fatal(err)
	}
	defer regular.Close()
	if useColor(colorAuto, regular) {
		t.Fatal("regular file unexpectedly enabled automatic color")
	}
	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if useColor(colorAuto, closed) {
		t.Fatal("closed file unexpectedly enabled automatic color")
	}
	if runtime.GOOS != "windows" {
		terminal, openErr := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
		if openErr == nil {
			defer terminal.Close()
			if info, statErr := terminal.Stat(); statErr == nil &&
				info.Mode()&os.ModeCharDevice != 0 &&
				!useColor(colorAuto, terminal) {
				t.Fatal("character device did not enable automatic color")
			}
		}
	}
}

func TestOutputPathCoverage(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	env := &Env{Dir: root}
	relative, err := resolveOutputPath(env, "reports/out.json", "test")
	if err != nil || relative != filepath.Join(root, "reports", "out.json") {
		t.Fatalf("relative output path = %q, %v", relative, err)
	}
	absolutePath := filepath.Join(root, "nested", "..", "out.json")
	absolute, err := resolveOutputPath(env, absolutePath, "test")
	if err != nil || absolute != filepath.Join(root, "out.json") {
		t.Fatalf("absolute output path = %q, %v", absolute, err)
	}
	for _, requested := range []string{"bad\x00name", "bad\nname"} {
		if _, err := resolveOutputPath(env, requested, "test"); err == nil {
			t.Fatalf("resolveOutputPath(%q) succeeded", requested)
		}
	}

	filePath := filepath.Join(root, "file")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := createExclusiveOutput(env, filePath, "test"); err == nil {
		t.Fatal("createExclusiveOutput replaced an existing file")
	}
}

func TestRunStatsVerifyAndDisclosureWriteFailures(t *testing.T) {
	t.Parallel()
	root, repo, head, blob := appRepoWithCommittedFile(t, "file.txt", "one\ntwo\n")
	if err := repo.WriteNote(head, mustAppInteropNote(t, blob)); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("stdout failed")
	tests := [][]string{
		{"stats", "--json"},
		{"verify", "--json"},
		{"disclosure", "--range", head},
		{"export", "--format", coverageGitAI},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			env := &Env{
				Stdin:  strings.NewReader(""),
				Stdout: coverageErrorWriter{err: sentinel},
				Stderr: io.Discard,
				Dir:    root,
			}
			code, err := Run(args, env)
			if code != ExitFailure || err == nil || !errors.Is(err, sentinel) {
				t.Fatalf("Run(%v) = %d, %v, want stdout failure", args, code, err)
			}
		})
	}
}

func TestRunStatusAndCheckWriteFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	sentinel := errors.New("stdout failed")
	for _, args := range [][]string{
		{"status", "--json"},
		{"check", "--json"},
	} {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			env := &Env{
				Stdin:  strings.NewReader(""),
				Stdout: coverageErrorWriter{err: sentinel},
				Stderr: io.Discard,
				Dir:    root,
			}
			code, err := Run(args, env)
			if code != ExitFailure || err == nil || !errors.Is(err, sentinel) {
				t.Fatalf("Run(%v) = %d, %v, want stdout failure", args, code, err)
			}
		})
	}
}

func TestRunCommandArgumentErrors(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	tests := [][]string{
		{"stats", "--json", "--json"},
		{"stats", "..HEAD"},
		{"status", "--json", "--json"},
		{"disclosure", "--range", coverageHead, "--range", coverageHead},
		{"disclosure", "--bad"},
		{"disclosure", "extra"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			code, _, _, err := appRun(root, zeroTime(), nil, args...)
			if code != ExitUsage || err == nil {
				t.Fatalf("Run(%v) = %d, %v, want usage error", args, code, err)
			}
		})
	}
}

func TestStatsOperationalFailures(t *testing.T) {
	t.Parallel()
	if code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "stats"); code != ExitFailure || err == nil {
		t.Fatalf("stats outside repository = %d, %v", code, err)
	}
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	if code, _, _, err := appRun(root, zeroTime(), nil, "stats", "missing..HEAD"); code != ExitFailure || err == nil {
		t.Fatalf("stats unknown revision = %d, %v", code, err)
	}
}

func TestDisclosureOutputHelperFailures(t *testing.T) {
	t.Parallel()
	env := &Env{Dir: t.TempDir()}
	if _, _, err := createDisclosureOutput(env, ""); err == nil {
		t.Fatal("empty disclosure path succeeded")
	}
	filePath := filepath.Join(env.Dir, "parent")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := createDisclosureOutput(env, filepath.Join(filePath, "out.json")); err == nil {
		t.Fatal("disclosure path under a file succeeded")
	}
}

func TestDisclosurePublishedStatAndPermissionFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disclosure.json")
	file, err := os.CreateTemp(dir, ".disclosure-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0o600); err != nil {
		t.Fatal(err)
	}
	err = writeDisclosureOutputWithLink(file, path, []byte("document\n"), func(temp, target string) error {
		if err := os.Link(temp, target); err != nil {
			return err
		}
		return os.Remove(target)
	})
	if err == nil || !strings.Contains(err.Error(), "stat published disclosure") {
		t.Fatalf("stat failure = %v", err)
	}

	file, err = os.CreateTemp(dir, ".disclosure-*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	err = writeDisclosureOutputWithLink(file, path, []byte("document\n"), func(temp, target string) error {
		if err := os.Link(temp, target); err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			return os.Chmod(target, 0o644)
		}
		return nil
	})
	if runtime.GOOS != "windows" && (err == nil || !strings.Contains(err.Error(), "permissions")) {
		t.Fatalf("permission failure = %v", err)
	}
}

func TestExportAndImportOperationalFailures(t *testing.T) {
	t.Parallel()
	code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "export", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("export outside repository = %d, %v", code, err)
	}
	unborn := appRepo(t)
	code, _, _, err = appRun(unborn, zeroTime(), nil, "export", "--format", coverageGitAI)
	if code != ExitFailure || err == nil || !strings.Contains(err.Error(), "unborn") {
		t.Fatalf("export unborn = %d, %v", code, err)
	}
	invalidHead := appRepo(t)
	if err := os.WriteFile(filepath.Join(invalidHead, ".git", "HEAD"), []byte("ref: refs/heads/missing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalidHead, ".git", "refs", "heads", "missing"), []byte("not-an-object\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err = appRun(invalidHead, zeroTime(), nil, "export", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("export invalid HEAD = %d, %v", code, err)
	}

	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	head := appHead(t, root)
	if err := repo.WriteNoteRef("refs/notes/ai", head, []byte("invalid Git AI note")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "import", "--format", coverageGitAI)
	if code != ExitSuccess || err != nil ||
		!strings.Contains(stderr, "skipped Git AI note") ||
		stdout != "imported 0 Git AI notes\n" {
		t.Fatalf("import malformed note = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, _, _, err = appRun(t.TempDir(), zeroTime(), nil, "import", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("import outside repository = %d, %v", code, err)
	}

	broken := appRepo(t)
	appWrite(t, broken, "file.txt", "one\n")
	appCommit(t, broken, "content")
	brokenRepo, err := gitcmd.Discover(broken)
	if err != nil {
		t.Fatal(err)
	}
	brokenHead := appHead(t, broken)
	brokenObject, err := appGitObject(t, broken, []byte("broken Git AI notes ref"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGitObjectRef(t, broken, "refs/notes/ai", brokenObject); err != nil {
		t.Fatal(err)
	}
	code, _, _, err = appRun(broken, zeroTime(), nil, "import", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("import broken notes ref = %d, %v", code, err)
	}
	if _, _, err := brokenRepo.ReadNoteRef("refs/notes/ai", brokenHead); err == nil {
		t.Fatal("broken Git AI notes ref unexpectedly readable")
	}
}

func TestCheckpointAdditionalFailures(t *testing.T) {
	t.Parallel()
	outside := t.TempDir()
	readPayload := `{"tool_name":"Read","tool_input":{"file_path":"file.txt"}}`
	code, stdout, stderr, err := appRun(outside, zeroTime(), strings.NewReader(readPayload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("ignored checkpoint = %d, %q, %q, %v", code, stdout, stderr, err)
	}

	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader("{"),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("malformed checkpoint = %d, %v", code, err)
	}
	missingPayload := `{"tool_name":"Edit","tool_input":{"file_path":"missing.txt"}}`
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(missingPayload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil {
		t.Fatalf("missing checkpoint path = %d, %v", code, err)
	}
	validAgentPayload := `{"type":"ai_agent","agent_name":"droid","edited_filepaths":["file.txt"]}`
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(validAgentPayload),
		"checkpoint", "agent-v1", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil {
		t.Fatalf("agent-v1 checkpoint = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(validAgentPayload),
		"checkpoint", "droid", "extra", "--type", "ai", "--hook-input", "stdin")
	if code != ExitUsage || err == nil {
		t.Fatalf("checkpoint positional argument = %d, %v", code, err)
	}
}

func TestCheckpointHelperFailures(t *testing.T) {
	t.Parallel()
	if _, err := readCheckpointInput(nil); err == nil {
		t.Fatal("readCheckpointInput(nil) succeeded")
	}
	readerError := errors.New("reader failed")
	if _, err := readCheckpointInput(coverageErrorReader{err: readerError}); !errors.Is(err, readerError) {
		t.Fatalf("readCheckpointInput reader error = %v", err)
	}
	large := strings.NewReader(strings.Repeat("x", preset.MaxInputBytes+1))
	if _, err := readCheckpointInput(large); err == nil {
		t.Fatal("readCheckpointInput accepted oversized input")
	}
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(transcriptPath,
		[]byte(`{"type":"message","message":{"role":"assistant","modelId":"bad\nmodel"}}`+"\n"),
		0o600); err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Type:           model.AuthorAI,
		Agent:          "droid",
		Model:          preset.FallbackModel,
		TranscriptPath: transcriptPath,
	}
	if got := resolveEventModel(event); got.Model != preset.FallbackModel {
		t.Fatalf("resolveEventModel accepted invalid model %q", got.Model)
	}
}

func TestDashboardAndHookOperationalFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	assertDashboardArgumentFailures(t, root)
	assertDashboardOutputFailures(t, root)
	assertBrokenStateFailures(t, root)
	assertHookFailures(t)
}

func assertDashboardArgumentFailures(t *testing.T, root string) {
	t.Helper()
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "--range", coverageHead, "--range", coverageHead); code != ExitUsage || err == nil {
		t.Fatalf("duplicate dashboard range = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "--range", coverageHead, "one", "two"); code != ExitUsage || err == nil {
		t.Fatalf("dashboard range with two files = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "missing.txt"); code != ExitFailure || err == nil {
		t.Fatalf("dashboard missing file = %d, %v", code, err)
	}
	if code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "dashboard"); code != ExitFailure || err == nil {
		t.Fatalf("dashboard outside repository = %d, %v", code, err)
	}
}

func assertDashboardOutputFailures(t *testing.T, root string) {
	t.Helper()
	outputParent := filepath.Join(root, "output-parent")
	if err := os.WriteFile(outputParent, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard",
		"--output", filepath.Join(outputParent, "dashboard.html")); code != ExitFailure || err == nil {
		t.Fatalf("dashboard output create failure = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "--range", coverageHead,
		"--output", filepath.Join(outputParent, "range.html")); code != ExitFailure || err == nil {
		t.Fatalf("dashboard range output create failure = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "export", "--format", coverageGitAI,
		"--commit", "missing"); code != ExitFailure || err == nil {
		t.Fatalf("export missing commit = %d, %v", code, err)
	}
}

func assertBrokenStateFailures(t *testing.T, root string) {
	t.Helper()
	statePath := filepath.Join(root, ".git", "byline", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "annotate"); code != ExitFailure || err == nil {
		t.Fatalf("annotate broken state = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "status"); code != ExitFailure || err == nil {
		t.Fatalf("status broken state = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "dashboard"); code != ExitFailure || err == nil {
		t.Fatalf("dashboard broken state = %d, %v", code, err)
	}
}

func assertHookFailures(t *testing.T) {
	t.Helper()
	fileRoot := t.TempDir()
	filePath := filepath.Join(fileRoot, "root-file")
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _, err := appRun(filePath, zeroTime(), nil, "install-hooks", "--agent", "none", "--git"); code != ExitFailure || err == nil {
		t.Fatalf("install hooks on file = %d, %v", code, err)
	}
	if code, _, _, err := appRun(filePath, zeroTime(), nil, "uninstall", "--agent", "none", "--git"); code != ExitFailure || err == nil {
		t.Fatalf("uninstall hooks on file = %d, %v", code, err)
	}
}

func TestDashboardTempOutputFailure(t *testing.T) {
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	tempRoot := t.TempDir()
	tempFile := filepath.Join(tempRoot, "not-a-directory")
	if err := os.WriteFile(tempFile, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	tempEnv := "TMPDIR"
	if runtime.GOOS == "windows" {
		tempEnv = "TMP"
	}
	t.Setenv(tempEnv, tempFile)
	if _, _, err := createDashboardOutput(&Env{}, ""); err == nil {
		t.Fatal("dashboard temp output unexpectedly succeeded")
	}
	code, _, _, err := appRun(root, zeroTime(), nil, "dashboard", "file.txt")
	if code != ExitFailure || err == nil {
		t.Fatalf("dashboard temp output = %d, %v", code, err)
	}
}

func TestBlameAndAnnotateFailures(t *testing.T) {
	t.Parallel()
	if code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "blame", "file.txt"); code != ExitFailure || err == nil {
		t.Fatalf("blame outside repository = %d, %v", code, err)
	}
	if code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "annotate"); code != ExitFailure || err == nil {
		t.Fatalf("annotate outside repository = %d, %v", code, err)
	}
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	if code, _, _, err := appRun(root, zeroTime(), nil, "annotate"); code != ExitSuccess || err != nil {
		t.Fatalf("annotate setup = %d, %v", code, err)
	}
	if code, _, _, err := appRun(root, zeroTime(), nil, "blame", "missing.txt"); code != ExitFailure || err == nil {
		t.Fatalf("blame missing file = %d, %v", code, err)
	}
	sentinel := errors.New("blame output failed")
	env := &Env{
		Stdin:  strings.NewReader(""),
		Stdout: coverageErrorWriter{err: sentinel},
		Stderr: io.Discard,
		Dir:    root,
	}
	code, err := Run([]string{"blame", "--json", "file.txt"}, env)
	if code != ExitFailure || err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("blame JSON output = %d, %v", code, err)
	}
}

func TestDisclosureWriteFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	readOnlyPath := filepath.Join(dir, "read-only")
	if err := os.WriteFile(readOnlyPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	readOnly, err := os.Open(readOnlyPath)
	if err != nil {
		t.Fatal(err)
	}
	err = writeDisclosureOutputWithLink(readOnly, filepath.Join(dir, "output"), []byte("data"),
		func(string, string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "write disclosure temp file") {
		t.Fatalf("disclosure write failure = %v", err)
	}

	// FlushFileBuffers blocks forever on a pipe handle on Windows, so
	// the sync failure scenario is unix-only.
	if runtime.GOOS != "windows" {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		err = writeDisclosureOutputWithLink(writer, filepath.Join(dir, "pipe-output"), []byte("data"),
			func(string, string) error { return nil })
		if err == nil || !strings.Contains(err.Error(), "sync disclosure temp file") {
			t.Fatalf("disclosure sync failure = %v", err)
		}
	}
}

func TestCheckpointCaptureStorageFailure(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	if err := os.WriteFile(filepath.Join(root, ".git", "byline"), []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{"tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(payload),
		"checkpoint", "droid", "--type", "ai", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("checkpoint storage failure = %d, %v", code, err)
	}
}

func TestProductArgumentHelpers(t *testing.T) {
	t.Parallel()
	command := lookup("install-hooks")
	options, err := parseHookOptions(command, []string{"--agent", "none", "--git"})
	if err != nil || !options.Git || options.Agent != "none" || options.User {
		t.Fatalf("parseHookOptions default scope = %+v, %v", options, err)
	}
	for _, args := range [][]string{
		{"extra"},
		{"--bad"},
		{"--template"},
		{"--agent", "other"},
	} {
		if _, err := parseHookOptions(command, args); err == nil {
			t.Fatalf("parseHookOptions(%v) returned nil error", args)
		}
	}
	for _, args := range [][]string{
		{"--json", "--json"},
		{"--unknown"},
	} {
		if _, _, err := parseJSONFlag(args); err == nil {
			t.Fatalf("parseJSONFlag(%v) returned nil error", args)
		}
	}
	if _, _, err := parseJSONFlag([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseJSONFlag help error = %v", err)
	}
}

func TestRewriteAdditionalFailurePaths(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	head := appHead(t, root)
	assertRewriteInitialFailures(t, root)
	assertRewriteSuccessfulTransaction(t, root, head)
	assertRewritePostRewriteFailures(t, root)
	assertRewritePostCheckoutFailure(t, root, head)
}

func assertRewriteInitialFailures(t *testing.T, root string) {
	t.Helper()
	code, _, _, err := appRun(root, zeroTime(), nil, "rewrite", "--bad")
	if code != ExitUsage || err == nil {
		t.Fatalf("rewrite bad flag = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), nil, "rewrite", "--mode", "ref-txn", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn without phase = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader("bad\n"),
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "unknown")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn unknown phase = %d, %v", code, err)
	}
	relevant := strings.Repeat("0", 40) + " " + strings.Repeat("1", 40) + " refs/heads/main\n"
	code, _, _, err = appRun(t.TempDir(), zeroTime(), strings.NewReader(relevant),
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn outside repository = %d, %v", code, err)
	}
}

func assertRewriteSuccessfulTransaction(t *testing.T, root, head string) {
	t.Helper()
	appWrite(t, root, "second.txt", "two\n")
	appCommit(t, root, "second")
	next := appHead(t, root)
	relevant := head + " " + next + " refs/heads/main\n"
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(relevant),
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn success = %d, %v", code, err)
	}
}

func assertRewritePostRewriteFailures(t *testing.T, root string) {
	t.Helper()
	code, _, _, err := appRun(root, zeroTime(), coverageErrorReader{err: errors.New("input failed")},
		"rewrite", "--mode", "post-rewrite", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("post-rewrite input failure = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader("bad\n"),
		"rewrite", "--mode", "post-rewrite", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("post-rewrite malformed input = %d, %v", code, err)
	}
	code, _, _, err = appRun(t.TempDir(), zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-rewrite", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("post-rewrite outside repository = %d, %v", code, err)
	}
}

func assertRewritePostCheckoutFailure(t *testing.T, root, head string) {
	t.Helper()
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin",
		head, strings.Repeat("g", 40), "0")
	if code != ExitUsage || err == nil {
		t.Fatalf("post-checkout invalid new object = %d, %v", code, err)
	}
}

func TestRewriteFailOpenStateErrors(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	head := appHead(t, root)
	statePath := filepath.Join(root, ".git", "byline", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	relevant := strings.Repeat("0", 40) + " " + strings.Repeat("1", 40) + " refs/heads/main\n"
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(relevant),
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn state error = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin",
		head, head, "0")
	if code != ExitSuccess || err != nil {
		t.Fatalf("post-checkout state error = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-merge", "--hook-input", "stdin")
	if code != ExitSuccess || err != nil {
		t.Fatalf("post-merge state error = %d, %v", code, err)
	}
	stashObject, err := appGitObject(t, root, []byte("broken stash notes ref"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGitObjectRef(t, root, "refs/notes/byline-stash", stashObject); err != nil {
		t.Fatal(err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(head+" 1\n"),
		"rewrite", "--mode", "stash-apply", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("stash apply state error = %d, %v", code, err)
	}
}

func appGitObject(t *testing.T, root string, content []byte) (string, error) {
	t.Helper()
	command := exec.Command("git", "hash-object", "-w", "--stdin")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	command.Stdin = bytes.NewReader(content)
	output, err := command.Output()
	return strings.TrimSpace(string(output)), err
}

func TestRewriteFailOpenBranches(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	head := appHead(t, root)
	code, _, _, err := appRun(t.TempDir(), zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin",
		head, head, "0")
	if code != ExitSuccess || err != nil {
		t.Fatalf("post-checkout outside repository = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin", head)
	if code != ExitUsage || err == nil {
		t.Fatalf("post-checkout argument count = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-merge", "--hook-input", "stdin", "0", "1")
	if code != ExitUsage || err == nil {
		t.Fatalf("post-merge argument count = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), coverageErrorReader{err: errors.New("ref input failed")},
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed")
	if code != ExitSuccess || err != nil {
		t.Fatalf("ref-txn input failure = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), coverageErrorReader{err: errors.New("stash input failed")},
		"rewrite", "--mode", "stash-apply", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("stash input failure = %d, %v", code, err)
	}
}

func TestRewriteObjectFormatFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	head := appHead(t, root)
	configPath := filepath.Join(root, ".git", "config")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, append(config, []byte("\ninvalid config line\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin",
		head, head, "0")
	if code != ExitSuccess || err != nil {
		t.Fatalf("post-checkout object format failure = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(head+" 1\n"),
		"rewrite", "--mode", "stash-apply", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("stash object format failure = %d, %v", code, err)
	}
}

func TestRewriteGitBoundaryFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := t.TempDir()
	fakeGit := filepath.Join(root, "git")
	script := `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_GIT_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_GIT_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_GIT_ROOT" ;;
  *"--show-object-format=storage"*) printf 'unsupported\n' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_GIT_ROOT", root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	head := strings.Repeat("1", 40)
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-merge", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("post-merge Git failure = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(""),
		"rewrite", "--mode", "post-checkout", "--hook-input", "stdin",
		head, head, "0")
	if code != ExitSuccess || err != nil {
		t.Fatalf("post-checkout object-format failure = %d, %v", code, err)
	}
	code, _, _, err = appRun(root, zeroTime(), strings.NewReader(head+" 1\n"),
		"rewrite", "--mode", "stash-apply", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("stash object-format failure = %d, %v", code, err)
	}
}

func writeGitObjectRef(t *testing.T, root, ref, object string) error {
	t.Helper()
	command := exec.Command("git", "update-ref", ref, object)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	if output, err := command.CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(string(output)))
	}
	return nil
}

func TestRunImportUsesWarningsOutput(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	code, stdout, stderr, err := appRun(root, zeroTime(), nil,
		"import", "--format", "gitai", "--dry-run")
	if code != ExitSuccess || err != nil || stdout != "would import 0 Git AI notes\n" || stderr != "" {
		t.Fatalf("empty import = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}
