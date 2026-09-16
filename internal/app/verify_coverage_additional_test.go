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
	"strconv"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestRunVerifyTextAndArgumentFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	assertVerifyArgumentFailures(t, root)
	assertVerifyTextSuccess(t, root)
	assertVerifyOutsideRepositoryFailure(t)
}

func assertVerifyArgumentFailures(t *testing.T, root string) {
	t.Helper()
	tests := [][]string{
		{"verify", "--help"},
		{"verify", "--bad"},
		{"verify", "a...b"},
		{"verify", "one", "two"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			assertVerifyArgumentCase(t, root, args)
		})
	}
}

func assertVerifyArgumentCase(t *testing.T, root string, args []string) {
	t.Helper()
	code, _, _, err := appRun(root, zeroTime(), nil, args...)
	if args[1] == "--help" {
		if code != ExitSuccess || err != nil {
			t.Fatalf("Run(%v) = %d, %v", args, code, err)
		}
		return
	}
	if code != ExitUsage || err == nil {
		t.Fatalf("Run(%v) = %d, %v, want usage error", args, code, err)
	}
}

func assertVerifyTextSuccess(t *testing.T, root string) {
	t.Helper()
	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "verified 0 annotated commits") {
		t.Fatalf("verify text = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func assertVerifyOutsideRepositoryFailure(t *testing.T) {
	t.Helper()
	code, _, _, err := appRun(t.TempDir(), zeroTime(), nil, "verify")
	if code != ExitFailure || err == nil {
		t.Fatalf("verify outside repository = %d, %v", code, err)
	}
}

func TestVerifyRepositoryDefensiveFailures(t *testing.T) {
	t.Parallel()
	if _, err := verifyRepository(nil, "", "", false); err == nil {
		t.Fatal("verifyRepository(nil) succeeded")
	}
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRepository(repo, "missing revision", "HEAD", false); err == nil {
		t.Fatal("verifyRepository accepted an invalid revision")
	}
}

func TestVerifyAndCheckRejectLongRanges(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	createLongHistory(t, root, verifyMaxCommits+1)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := verifyRepository(repo, "", "long", false); err == nil ||
		!strings.Contains(err.Error(), "verification range exceeds") {
		t.Fatalf("verify long history = %v", err)
	}
	if _, err := missingNoteViolations(repo, "", "long"); err == nil ||
		!strings.Contains(err.Error(), "policy range exceeds") {
		t.Fatalf("check long history = %v", err)
	}
}

func TestVerifySortsIssuePathsAndMessages(t *testing.T) {
	t.Parallel()
	root, repo, commit, blob := appRepoWithCommittedFile(t, "file.txt", "one\n")
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file-a.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"file-b.txt": {
				Blob: strings.Repeat("0", 40),
				Ranges: []model.Range{{
					Start: 1, End: verifyMaxLines + 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"file-c.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
	appWrite(t, root, "second.txt", "two\n")
	appCommit(t, root, "second")
	second := appHead(t, root)
	if err := repo.WriteNote(second, data); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("verify sorted issues = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var result verifyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) < 3 {
		t.Fatalf("verify issues = %+v", result.Issues)
	}
}

func TestVerifyReadNoteFailure(t *testing.T) {
	t.Parallel()
	root, repo, commit, blob := appRepoWithCommittedFile(t, "file.txt", "one\n")
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
	list := strings.Fields(appGit(t, root, "notes", "--ref="+bylineNotesRef, "list"))
	if len(list) != 2 {
		t.Fatalf("notes list = %v", list)
	}
	noteObject := list[0]
	objectPath := filepath.Join(repo.GitDir, "objects", noteObject[:2], noteObject[2:])
	if err := os.Remove(objectPath); err != nil {
		t.Skipf("note object was not loose: %v", err)
	}
	code, _, _, err := appRunVerifyJSON(root)
	if code != ExitFailure || err == nil {
		t.Fatalf("verify missing note object = %d, %v", code, err)
	}
}

func appRunVerifyJSON(root string) (int, string, string, error) {
	return appRun(root, zeroTime(), nil, "verify", "--json")
}

func TestVerifyListedMissingNoteAndNoteListFailure(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	commit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	noteObject, err := appGitObject(t, root, []byte("note object"))
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGitObjectRef(t, root, bylineNotesRef, noteObject); err != nil {
		t.Fatal(err)
	}
	code, _, _, err := appRun(root, zeroTime(), nil, "verify")
	if code != ExitFailure || err == nil {
		t.Fatalf("verify broken note list = %d, %v", code, err)
	}

	if err := os.Remove(filepath.Join(repo.GitDir, "refs", "notes", "byline")); err != nil &&
		!os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(repo.GitDir, "refs", "notes", "byline")); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, []byte("garbage")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("verify garbage note = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var result verifyResult
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 1 || result.Issues[0].Commit != commit ||
		!strings.Contains(result.Issues[0].Message, "decode note") {
		t.Fatalf("garbage note issues = %+v", result.Issues)
	}
	if err := os.Remove(filepath.Join(repo.GitDir, "refs", "notes", "byline")); err != nil &&
		!os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestVerifyNoteMissingPathAndLargeCoverage(t *testing.T) {
	t.Parallel()
	_, repo, commit, blob := appRepoWithCommittedFile(t, "file.txt", "one\n")
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"missing.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"large-a.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 100000,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"large-b.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 100000,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := verifyNote(repo, commit, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVerifyIssue(issues, "missing.txt", "attributed path is missing from commit") {
		t.Fatalf("verify issues = %+v", issues)
	}
	if !hasVerifyIssue(issues, "large-b.txt", "note exceeds") {
		t.Fatalf("large coverage issues = %+v", issues)
	}
}

func TestVerifyDeepBinaryAndLineLimitFailures(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	binaryPath := filepath.Join(root, "binary.bin")
	if err := os.WriteFile(binaryPath, []byte{'a', 0, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}
	appCommit(t, root, "binary")
	binaryCommit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	binaryBlob, exists, err := repo.BlobID(binaryCommit, "binary.bin")
	if err != nil || !exists {
		t.Fatalf("binary BlobID = %q, %t, %v", binaryBlob, exists, err)
	}
	binaryNote := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"binary.bin": {
				Blob: binaryBlob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(binaryNote)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := verifyNote(repo, binaryCommit, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVerifyIssue(issues, "binary.bin", "read blob lines") {
		t.Fatalf("binary verify issues = %+v", issues)
	}

	linePath := filepath.Join(root, "many-lines.txt")
	if err := os.WriteFile(linePath, []byte(strings.Repeat("x\n", verifyMaxLines+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	appCommit(t, root, "many lines")
	lineCommit := appHead(t, root)
	lineBlob, exists, err := repo.BlobID(lineCommit, "many-lines.txt")
	if err != nil || !exists {
		t.Fatalf("line BlobID = %q, %t, %v", lineBlob, exists, err)
	}
	lineNote := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"many-lines.txt": {
				Blob: lineBlob,
				Ranges: []model.Range{{
					Start: 1, End: verifyMaxLines + 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err = notes.Encode(lineNote)
	if err != nil {
		t.Fatal(err)
	}
	issues, err = verifyNote(repo, lineCommit, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVerifyIssue(issues, "many-lines.txt", "blob lines exceed") {
		t.Fatalf("line limit issues = %+v", issues)
	}
}

func TestVerifyDeepContentLimit(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	path := filepath.Join(root, "large.txt")
	content := strings.Repeat("x", verifyMaxBytes+1)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	appCommit(t, root, "large")
	commit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "large.txt")
	if err != nil || !exists {
		t.Fatalf("large BlobID = %q, %t, %v", blob, exists, err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"large.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	issues, err := verifyNote(repo, commit, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if !hasVerifyIssue(issues, "large.txt", "note content exceeds") {
		t.Fatalf("large content issues = %+v", issues)
	}
}

func TestVerifyJSONWriteFailure(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "content")
	sentinel := errors.New("verify output failed")
	env := &Env{
		Stdin:  strings.NewReader(""),
		Stdout: coverageErrorWriter{err: sentinel},
		Stderr: io.Discard,
		Dir:    root,
	}
	code, err := Run([]string{"verify", "--json"}, env)
	if code != ExitFailure || err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("verify JSON write = %d, %v", code, err)
	}
}

func TestVerifyGitBoundaryFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := t.TempDir()
	fakeGit := filepath.Join(root, "git")
	script := `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_VERIFY_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_VERIFY_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_VERIFY_ROOT" ;;
  *"rev-list"*) printf '%s\n' "$FAKE_VERIFY_COMMIT" ;;
  *"show -s"*) printf '2026-01-02T03:04:05+00:00\n' ;;
  *"notes --ref=refs/notes/byline list"*) printf '%s %s\n' "$FAKE_VERIFY_NOTE" "$FAKE_VERIFY_COMMIT" ;;
  *"notes --ref=refs/notes/byline show"*)
    if [ "$FAKE_VERIFY_MODE" = "note-missing" ]; then exit 1; fi
    printf '%s\n' "$FAKE_VERIFY_NOTE_DATA"
    ;;
  *"ls-tree"*)
    if [ "$FAKE_VERIFY_MODE" = "tree" ]; then exit 2; fi
    if [ "$FAKE_VERIFY_MODE" = "missing" ]; then exit 0; fi
    printf '100644 blob %s\tfile.txt\0' "$FAKE_VERIFY_BLOB"
    ;;
  *"cat-file -s"*)
    if [ "$FAKE_VERIFY_MODE" = "size" ]; then exit 1; fi
    printf '1\n'
    ;;
  *"cat-file blob"*)
    if [ "$FAKE_VERIFY_MODE" = "read" ]; then exit 1; fi
    if [ "$FAKE_VERIFY_MODE" = "size-change" ]; then printf 'one\n'; else printf 'one'; fi
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("1", 40)
	blob := strings.Repeat("2", 40)
	t.Setenv("FAKE_VERIFY_ROOT", root)
	t.Setenv("FAKE_VERIFY_COMMIT", commit)
	t.Setenv("FAKE_VERIFY_NOTE", strings.Repeat("3", 40))
	t.Setenv("FAKE_VERIFY_BLOB", blob)
	t.Setenv("FAKE_VERIFY_NOTE_DATA",
		`{"version":3,"files":{"file.txt":{"blob":"`+blob+`","ranges":[{"start":1,"end":1,"author":"human"}]}}}`)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, mode := range []string{"note-missing", "tree", "missing", "size", "read", "size-change"} {
		t.Setenv("FAKE_VERIFY_MODE", mode)
		code, _, _, err := appRun(root, zeroTime(), nil, "verify", "--deep")
		if code != ExitFailure || err == nil {
			t.Fatalf("verify fake Git mode %s = %d, %v", mode, code, err)
		}
	}
}

func hasVerifyIssue(issues []verifyIssue, path, message string) bool {
	for _, issue := range issues {
		if issue.Path == path && strings.Contains(issue.Message, message) {
			return true
		}
	}
	return false
}

func createLongHistory(t *testing.T, root string, count int) {
	t.Helper()
	var input bytes.Buffer
	for index := 1; index <= count; index++ {
		input.WriteString("commit refs/heads/long\n")
		input.WriteString("mark :" + strconv.Itoa(index) + "\n")
		input.WriteString("author Test User <test@example.invalid> 0 +0000\n")
		input.WriteString("committer Test User <test@example.invalid> 0 +0000\n")
		input.WriteString("data 2\nx\n")
		if index > 1 {
			input.WriteString("from :" + strconv.Itoa(index-1) + "\n")
		}
	}
	command := exec.Command("git", "fast-import")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	command.Stdin = bytes.NewReader(input.Bytes())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, output)
	}
}
