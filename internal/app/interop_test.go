package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestInteropCommands(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "human\nai\n")
	appCommit(t, root, "content")
	head := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.txt")
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	if err := repo.WriteNote(head, mustAppInteropNote(t, blob)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroAppTime(), nil, "export", "--format", "gitai")
	if code != ExitSuccess || err != nil || stderr != "" || !strings.Contains(stdout, "authorship/3.0.0") {
		t.Fatalf("gitai export = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	tracePath := filepath.Join(t.TempDir(), "trace.json")
	code, stdout, stderr, err = appRun(root, zeroAppTime(), nil,
		"export", "--format", "agent-trace", "--output", tracePath)
	if code != ExitSuccess || err != nil || stdout != tracePath+"\n" || stderr != "" {
		t.Fatalf("trace export = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	traceData, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatal(err)
	}
	var trace map[string]any
	if err := json.Unmarshal(traceData, &trace); err != nil || trace["version"] != "0.1.0" {
		t.Fatalf("trace output = %s, %v", traceData, err)
	}
	if code, _, _, err := appRun(root, zeroAppTime(), nil,
		"export", "--format", "agent-trace", "--output", tracePath); code != ExitFailure || err == nil {
		t.Fatalf("export overwrite = %d, %v", code, err)
	}
}

func TestImportCommand(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appCommit(t, root, "content")
	head := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	data := "file.txt\n  0123456789abcdef 2\n---\n" +
		"{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"" + head +
		"\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"model\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}\n"
	if err := repo.WriteNoteRef("refs/notes/ai", head, []byte(data)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroAppTime(), nil,
		"import", "--format", "gitai", "--dry-run")
	if code != ExitSuccess || err != nil || stdout != "would import 1 Git AI notes\n" || stderr != "" {
		t.Fatalf("dry import = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if _, found, err := repo.ReadNote(head); err != nil || found {
		t.Fatalf("dry import wrote byline note: %t, %v", found, err)
	}
	code, stdout, stderr, err = appRun(root, zeroAppTime(), nil, "import", "--format", "gitai")
	if code != ExitSuccess || err != nil || stdout != "imported 1 Git AI notes\n" || stderr != "" {
		t.Fatalf("import = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestInteropCommandUsage(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	tests := [][]string{
		{"export"},
		{"export", "--format", "other"},
		{"export", "--format", "gitai", "extra"},
		{"import"},
		{"import", "--format", "agent-trace"},
		{"import", "--format", "gitai", "extra"},
	}
	for _, args := range tests {
		args := args
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			t.Parallel()
			code, _, _, err := appRun(root, zeroAppTime(), nil, args...)
			if code != ExitUsage || err == nil {
				t.Fatalf("Run(%v) = %d, %v, want usage error", args, code, err)
			}
		})
	}
	for _, args := range [][]string{
		{"export", "-h"},
		{"import", "--help"},
	} {
		code, stdout, stderr, err := appRun(root, zeroAppTime(), nil, args...)
		if code != ExitSuccess || err != nil || stdout == "" || stderr != "" {
			t.Fatalf("help %v = %d, %q, %q, %v", args, code, stdout, stderr, err)
		}
	}
	for _, path := range []string{"bad\npath", "bad\tpath"} {
		code, _, _, err := appRun(root, zeroAppTime(), nil,
			"export", "--format", "gitai", "--output", path)
		if code != ExitFailure || err == nil {
			t.Fatalf("bad output path %q = %d, %v", path, code, err)
		}
	}
}

func mustAppInteropNote(t *testing.T, blob string) []byte {
	t.Helper()
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {Blob: blob, Ranges: []model.Range{{
				Start: 1, End: 2,
				Attribution: model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model", Session: "session"},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func zeroAppTime() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}
