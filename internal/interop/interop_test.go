package interop

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestDecodeGitAIGolden(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "gitai-v3.txt"))
	if err != nil {
		t.Fatal(err)
	}
	note, err := DecodeGitAI(data)
	if err != nil {
		t.Fatal(err)
	}
	if note.SchemaVersion != "authorship/3.0.0" || note.BaseCommit == "" {
		t.Fatalf("note header = %+v", note)
	}
	entries := note.Files["src/example.go"]
	if len(entries) != 3 || entries[0].Key != "s_0123456789abcd::t_abcdef01234567" {
		t.Fatalf("entries = %+v", entries)
	}
	if note.Sessions["s_0123456789abcd"].AgentID.Tool != "claude" ||
		note.Humans["h_89abcdef012345"].Author == "" ||
		note.Prompts["0123456789abcdef"].AgentID.Tool != "cursor" {
		t.Fatalf("metadata = %+v", note)
	}
}

func TestAgentTraceGoldenFixture(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "agent-trace-v0.1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record AgentTraceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Version != "0.1.0" || record.VCS.Type != "git" ||
		len(record.Files) != 1 || len(record.Files[0].Conversations) != 1 {
		t.Fatalf("record = %+v", record)
	}
}

func TestDecodeGitAIErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
	}{
		{"missing divider", "file.go\n  0123456789abcdef 1\n"},
		{"unknown schema", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/2.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{}}"},
		{"missing prompt", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{}}"},
		{"overlap", "file.go\n  0123456789abcdef 1-2\n  0123456789abcdef 2-3\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}"},
		{"control path", "bad\x1bpath\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}"},
		{"unknown metadata", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{},\"unknown\":true}"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeGitAI([]byte(test.data)); err == nil {
				t.Fatal("DecodeGitAI accepted invalid note")
			}
		})
	}
}

func TestToBylineNoteMapsAllThreeStates(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\nfour\nfive\nsix\n")
	commit := commitInterop(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	note := GitAINote{
		SchemaVersion: "authorship/3.0.0",
		BaseCommit:    commit,
		Files: map[string][]GitAIAttestation{
			"file.txt": {
				{Key: "s_0123456789abcd::t_abcdef01234567", Ranges: []GitAILineRange{{Start: 1, End: 1}}},
				{Key: "h_89abcdef012345", Ranges: []GitAILineRange{{Start: 3, End: 3}}},
				{Key: "0123456789abcdef", Ranges: []GitAILineRange{{Start: 5, End: 5}}},
			},
		},
		Sessions: map[string]GitAISession{
			"s_0123456789abcd": {AgentID: GitAIAgentID{Tool: "claude", ID: "c", Model: "model"}},
		},
		Humans: map[string]GitAIHuman{"h_89abcdef012345": {Author: "human"}},
		Prompts: map[string]GitAIPrompt{
			"0123456789abcdef": {AgentID: GitAIAgentID{Tool: "cursor", ID: "p", Model: "legacy"}},
		},
	}
	converted, err := note.ToBylineNote(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	ranges := converted.Files["file.txt"].Ranges
	if len(ranges) != 6 ||
		ranges[0].Author != model.AuthorAI || ranges[0].Agent != "claude" ||
		ranges[1].Author != model.AuthorUntracked ||
		ranges[2].Author != model.AuthorHuman ||
		ranges[4].Author != model.AuthorAI || ranges[4].Agent != "cursor" ||
		ranges[5].Author != model.AuthorUntracked {
		t.Fatalf("converted ranges = %+v", ranges)
	}
}

func TestExportGitAIAndAgentTraceAreDeterministic(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "human\nai\nunknown\n")
	commit := commitInterop(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "file.txt")
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "cursor", Model: "anthropic/model", Session: "conversation",
					}},
					{Start: 3, End: 3, Attribution: model.Attribution{Author: model.AuthorUntracked}},
				},
			},
		},
	}
	encoded, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, encoded); err != nil {
		t.Fatal(err)
	}
	first, err := ExportGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ExportGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("Git AI export is not deterministic:\n%s\n%s", first, second)
	}
	parsed, err := DecodeGitAI(first)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Files["file.txt"]) != 2 ||
		!strings.HasPrefix(parsed.Files["file.txt"][1].Key, "s_") {
		t.Fatalf("Git AI export = %+v", parsed)
	}
	traceFirst, err := ExportAgentTrace(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	traceSecond, err := ExportAgentTrace(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if string(traceFirst) != string(traceSecond) {
		t.Fatal("Agent Trace export is not deterministic")
	}
	var trace AgentTraceRecord
	if err := json.Unmarshal(traceFirst, &trace); err != nil {
		t.Fatal(err)
	}
	if trace.Version != "0.1.0" || trace.VCS.Revision != commit || len(trace.Files) != 1 {
		t.Fatalf("trace = %+v", trace)
	}
}

func TestImportGitAISkipsDifferentBylineNote(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\n")
	commit := commitInterop(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	gitAINote := "file.txt\n  0123456789abcdef 2\n---\n" +
		"{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"" + commit +
		"\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"model\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}\n"
	if err := repo.WriteNoteRef(GitAINotesRef, commit, []byte(gitAINote)); err != nil {
		t.Fatal(err)
	}
	result, err := ImportGitAI(repo, "", false)
	if err != nil || result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("ImportGitAI = %+v, %v", result, err)
	}
	result, err = ImportGitAI(repo, "", false)
	if err != nil || result.Imported != 0 {
		t.Fatalf("repeat ImportGitAI = %+v, %v", result, err)
	}
	// Replace the existing note through a second commit so the refusal path
	// can be tested without bypassing gitcmd's note safety.
	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\nfour\n")
	second := commitInterop(t, root, "second")
	blob, _, err := repo.BlobID(second, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(second, mustInteropNote(t, blob)); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNoteRef(GitAINotesRef, second, []byte(strings.Replace(gitAINote, commit, second, 1))); err != nil {
		t.Fatal(err)
	}
	result, err = ImportGitAI(repo, second, false)
	if err != nil || result.Skipped != 1 || len(result.Warnings) != 1 {
		t.Fatalf("different note import = %+v, %v", result, err)
	}
}

func mustInteropNote(t *testing.T, blob string) []byte {
	t.Helper()
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {Blob: blob, Ranges: []model.Range{{
				Start: 1, End: 4,
				Attribution: model.Attribution{Author: model.AuthorUntracked},
			}}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func interopRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runInteropGit(t, root, "init", "-b", "main")
	runInteropGit(t, root, "config", "user.name", "Test User")
	runInteropGit(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func writeInteropFile(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commitInterop(t *testing.T, root, message string) string {
	t.Helper()
	runInteropGit(t, root, "add", "-A")
	runInteropGit(t, root, "commit", "-m", message)
	return strings.TrimSpace(runInteropGit(t, root, "rev-parse", "HEAD"))
}

func runInteropGit(t *testing.T, root string, args ...string) string {
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
