package interop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/store"
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
		want string
	}{
		{"missing divider", "file.go\n  0123456789abcdef 1\n", ""},
		{"unknown schema", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/2.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{}}", ""},
		{"missing prompt", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{}}", ""},
		{"overlap", "file.go\n  0123456789abcdef 1-2\n  0123456789abcdef 2-3\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}", ""},
		{"control path", "bad\x1bpath\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}", ""},
		{"unknown metadata", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{},\"unknown\":true}", ""},
		{"negative counter", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":-1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0}}}", "counters must be nonnegative"},
		{"invalid metadata string", "file.go\n  0123456789abcdef 1\n---\n{\"schema_version\":\"authorship/3.0.0\",\"base_commit_sha\":\"0123456789abcdef0123456789abcdef01234567\",\"prompts\":{\"0123456789abcdef\":{\"agent_id\":{\"tool\":\"cursor\",\"id\":\"c\",\"model\":\"m\"},\"total_additions\":1,\"total_deletions\":0,\"accepted_lines\":1,\"overriden_lines\":0,\"messages_url\":\"\\u0001\"}}}", "invalid metadata string"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeGitAI([]byte(test.data)); err == nil {
				t.Fatal("DecodeGitAI accepted invalid note")
			} else if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeGitAI error = %q, want %q", err, test.want)
			}
		})
	}
}

func TestValidLegacyIDs(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0123456789abcdef", "0123456"} {
		if !validLegacyID(value) {
			t.Fatalf("validLegacyID(%q) = false", value)
		}
	}
	for _, key := range []string{"0123456789abcdef", "0123456"} {
		data := "file.go\n  " + key + " 1\n---\n" +
			`{"schema_version":"authorship/3.0.0","base_commit_sha":"0123456789abcdef0123456789abcdef01234567","prompts":{` +
			fmt.Sprintf("%q", key) +
			`:{"agent_id":{"tool":"cursor","id":"c","model":"m"},"total_additions":1,"total_deletions":0,"accepted_lines":1,"overriden_lines":0}}}`
		note, err := DecodeGitAI([]byte(data))
		if err != nil {
			t.Fatalf("DecodeGitAI(%q): %v", key, err)
		}
		if _, ok := note.Prompts[key]; !ok {
			t.Fatalf("prompt %q missing after decode", key)
		}
	}
}

func TestGitAISessionIDAlwaysDerivesFromConversation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		tool         string
		conversation string
		want         string
	}{
		{
			name:         "ordinary conversation",
			tool:         "cursor",
			conversation: "conversation",
			want:         "s_58b12a0f30f017",
		},
		{
			name:         "prehashed conversation",
			tool:         "claude",
			conversation: "s_0123456789abcd",
			want:         "s_c14046f11a8b9a",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got := gitAISessionID(test.tool, test.conversation)
			if got != test.want {
				t.Fatalf("gitAISessionID() = %q, want %q", got, test.want)
			}
			if test.name == "prehashed conversation" && got == test.conversation {
				t.Fatal("pre-hashed conversation was preserved")
			}
		})
	}
}

func TestDecodeGitAIManyEntries(t *testing.T) {
	t.Parallel()
	const count = 10_000
	var builder strings.Builder
	builder.WriteString("file.go\n")
	for line := 1; line <= count; line++ {
		fmt.Fprintf(&builder, "  0123456789abcdef %d\n", line)
	}
	builder.WriteString("---\n")
	builder.WriteString(`{"schema_version":"authorship/3.0.0","base_commit_sha":"0123456789abcdef0123456789abcdef01234567","prompts":{"0123456789abcdef":{"agent_id":{"tool":"cursor","id":"c","model":"m"},"total_additions":1,"total_deletions":0,"accepted_lines":1,"overriden_lines":0}}}`)
	note, err := DecodeGitAI([]byte(builder.String()))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(note.Files["file.go"]); got != count {
		t.Fatalf("decoded entries = %d, want %d", got, count)
	}
}

func TestDecodeGitAIRejectsTooManyEntries(t *testing.T) {
	t.Parallel()
	var builder strings.Builder
	builder.WriteString("file.go\n")
	for line := 1; line <= maxGitAIEntries+1; line++ {
		fmt.Fprintf(&builder, "  0123456789abcdef %d\n", line)
	}
	builder.WriteString("---\n")
	builder.WriteString(`{"schema_version":"authorship/3.0.0","base_commit_sha":"0123456789abcdef0123456789abcdef01234567","prompts":{"0123456789abcdef":{"agent_id":{"tool":"cursor","id":"c","model":"m"},"total_additions":1,"total_deletions":0,"accepted_lines":1,"overriden_lines":0}}}`)
	if _, err := DecodeGitAI([]byte(builder.String())); err == nil ||
		!strings.Contains(err.Error(), "more than 100000 attestations") {
		t.Fatalf("DecodeGitAI error = %v", err)
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
	if err := store.New(repo.GitDir).AppendCheckpoint(model.Checkpoint{
		Version: model.CheckpointVersion,
		Kind:    "edit",
		Seq:     1,
		TS:      "2026-01-02T03:04:05Z",
		Type:    model.AuthorAI,
		Session: "conversation",
		Agent:   "cursor",
		Model:   "anthropic/model",
		Files:   []model.Snapshot{{Path: "file.txt", Exists: true, Blob: blob}},
	}); err != nil {
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
	traceExport, err := Export(repo, "agent-trace", commit)
	if err != nil {
		t.Fatal(err)
	}
	traceAlias, err := EncodeAgentTrace(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(traceExport, traceFirst) || !bytes.Equal(traceAlias, traceFirst) {
		t.Fatal("Agent Trace aliases differ from direct export")
	}
	var trace AgentTraceRecord
	if err := json.Unmarshal(traceFirst, &trace); err != nil {
		t.Fatal(err)
	}
	if trace.Version != "0.1.0" || trace.VCS.Revision != commit || len(trace.Files) != 1 {
		t.Fatalf("trace = %+v", trace)
	}
}

func TestExportGitAIUsesGoldenBytes(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "a.go", "ai-a\nhuman-a\n")
	writeInteropFile(t, root, "z.go", "human-z\nai-z\nuntracked-z\n")
	commit := commitInteropWithDate(t, root, "golden", "2026-01-02T03:04:05Z")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blobs := map[string]string{}
	for _, path := range []string{"a.go", "z.go"} {
		blob, exists, err := repo.BlobID(commit, path)
		if err != nil || !exists {
			t.Fatalf("blob %q = %q, %t, %v", path, blob, exists, err)
		}
		blobs[path] = blob
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"a.go": {
				Blob: blobs["a.go"],
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "cursor", Model: "model", Session: "conversation",
					}},
					{Start: 2, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
				},
			},
			"z.go": {
				Blob: blobs["z.go"],
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "cursor", Model: "model", Session: "conversation",
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
	if err := store.New(repo.GitDir).AppendCheckpoint(model.Checkpoint{
		Version: model.CheckpointVersion,
		Kind:    "edit",
		Seq:     7,
		TS:      "2026-01-02T03:04:05Z",
		Type:    model.AuthorAI,
		Session: "conversation",
		Agent:   "cursor",
		Model:   "model",
		Files: []model.Snapshot{
			{Path: "a.go", Exists: true, Blob: blobs["a.go"]},
			{Path: "z.go", Exists: true, Blob: blobs["z.go"]},
		},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Export(repo, "gitai", commit)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "gitai-export.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("Git AI export differs from golden:\n%s", got)
	}
	alias, err := EncodeGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(alias, want) {
		t.Fatalf("Git AI alias differs from golden:\n%s", alias)
	}
}

func TestExportRejectsInvalidFormat(t *testing.T) {
	t.Parallel()
	if _, err := Export(nil, "gitai", ""); err == nil {
		t.Fatal("Export accepted a nil repository")
	}
	root := interopRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Export(repo, "unknown", ""); err == nil ||
		!strings.Contains(err.Error(), "unsupported export format") {
		t.Fatalf("Export unknown format error = %v", err)
	}
}

func TestReadAndWriteGitAINote(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\nfour\n")
	commit := commitInterop(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "file.txt")
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	if note, found, err := ReadGitAINote(repo, commit); err != nil || found || len(note.Files) != 0 {
		t.Fatalf("ReadGitAINote before write = %+v, %t, %v", note, found, err)
	}
	byline, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 4,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, byline); err != nil {
		t.Fatal(err)
	}
	if err := WriteGitAINote(repo, commit); err != nil {
		t.Fatal(err)
	}
	note, found, err := ReadGitAINote(repo, commit)
	if err != nil || !found {
		t.Fatalf("ReadGitAINote after write = %+v, %t, %v", note, found, err)
	}
	if note.BaseCommit != commit || len(note.Files["file.txt"]) != 1 ||
		len(note.Humans) != 1 {
		t.Fatalf("Git AI note = %+v", note)
	}
	if err := WriteGitAINote(repo, commit); err != nil {
		t.Fatalf("idempotent WriteGitAINote: %v", err)
	}
}

func TestExportGitAIUnmatchedAIIsUntracked(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "unmatched\nhuman\n")
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
					{Start: 1, End: 1, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "cursor", Model: "model", Session: "missing",
					}},
					{Start: 2, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
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
	data, err := ExportGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := DecodeGitAI(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Files["file.txt"]; len(got) != 1 ||
		got[0].Key != gitAIHumanID(syntheticHumanAuthor) ||
		got[0].Ranges[0] != (GitAILineRange{Start: 2, End: 2}) {
		t.Fatalf("exported attestations = %+v", got)
	}
	converted, err := parsed.ToBylineNote(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	ranges := converted.Files["file.txt"].Ranges
	if len(ranges) != 2 || ranges[0].Author != model.AuthorUntracked ||
		ranges[1].Author != model.AuthorHuman {
		t.Fatalf("converted unmatched attribution = %+v", ranges)
	}
}

func TestGitAIPathLeadingQuoteRoundTrip(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	path := `"quoted.txt`
	writeInteropFile(t, root, path, "line\n")
	commit := commitInterop(t, root, "quoted path")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, path)
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	encoded, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			path: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, encoded); err != nil {
		t.Fatal(err)
	}
	data, err := ExportGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"\\\"quoted.txt\"") {
		t.Fatalf("export did not quote leading-quote path: %s", data)
	}
	parsed, err := DecodeGitAI(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed.Files[path]; !ok {
		t.Fatalf("parsed paths = %+v", parsed.Files)
	}
	converted, err := parsed.ToBylineNote(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := converted.Files[path]; !ok {
		t.Fatalf("converted paths = %+v", converted.Files)
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

func TestImportGitAIDryRunAndMalformedNote(t *testing.T) {
	t.Parallel()
	root := interopRepo(t)
	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\nfour\n")
	commit := commitInterop(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "file.txt")
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	if err := repo.WriteNote(commit, mustInteropNote(t, blob)); err != nil {
		t.Fatal(err)
	}
	if err := WriteGitAINote(repo, commit); err != nil {
		t.Fatal(err)
	}
	runInteropGit(t, root, "notes", "--ref=refs/notes/byline", "remove", commit)
	result, err := ImportGitAI(repo, commit, true)
	if err != nil || result.Imported != 1 || result.Skipped != 0 {
		t.Fatalf("dry-run import = %+v, %v", result, err)
	}
	if _, found, err := repo.ReadNote(commit); err != nil || found {
		t.Fatalf("dry-run wrote byline note: found=%t, err=%v", found, err)
	}
	result, err = ImportGitAI(repo, commit, false)
	if err != nil || result.Imported != 1 {
		t.Fatalf("real import = %+v, %v", result, err)
	}

	writeInteropFile(t, root, "file.txt", "one\ntwo\nthree\n")
	malformed := commitInterop(t, root, "malformed")
	if err := repo.WriteNoteRef(GitAINotesRef, malformed, []byte("not a Git AI note\n")); err != nil {
		t.Fatal(err)
	}
	result, err = ImportGitAI(repo, malformed, false)
	if err != nil || result.Skipped != 1 || len(result.Warnings) != 1 {
		t.Fatalf("malformed import = %+v, %v", result, err)
	}
}

func TestInteropValidationHelpers(t *testing.T) {
	t.Parallel()
	t.Run("sameStringMap", func(t *testing.T) {
		tests := []struct {
			name        string
			left, right map[string]string
			want        bool
		}{
			{name: "both nil", want: true},
			{name: "same values", left: map[string]string{"one": "1"}, right: map[string]string{"one": "1"}, want: true},
			{name: "different length", left: map[string]string{"one": "1"}, want: false},
			{name: "different value", left: map[string]string{"one": "1"}, right: map[string]string{"one": "2"}, want: false},
			{name: "missing key", left: map[string]string{"one": "1"}, right: map[string]string{"two": "1"}, want: false},
		}
		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				if got := sameStringMap(test.left, test.right); got != test.want {
					t.Fatalf("sameStringMap() = %t, want %t", got, test.want)
				}
			})
		}
	})
	t.Run("validateGitAIMetadata", func(t *testing.T) {
		valid := &gitAIAgentIDWire{Tool: "cursor", ID: "conversation", Model: "model"}
		tests := []struct {
			name    string
			agent   *gitAIAgentIDWire
			human   string
			custom  map[string]string
			wantErr bool
		}{
			{name: "valid", agent: valid},
			{name: "valid custom", agent: valid, custom: map[string]string{"source": "editor"}},
			{name: "missing agent", human: "human", wantErr: true},
			{name: "empty tool", agent: &gitAIAgentIDWire{ID: "id", Model: "model"}, wantErr: true},
			{name: "control id", agent: &gitAIAgentIDWire{Tool: "tool", ID: "\x01", Model: "model"}, wantErr: true},
			{name: "control human", agent: valid, human: "\x01", wantErr: true},
			{name: "control custom key", agent: valid, custom: map[string]string{"\x01": "value"}, wantErr: true},
			{name: "control custom value", agent: valid, custom: map[string]string{"key": "\x01"}, wantErr: true},
		}
		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				if err := validateGitAIMetadata(test.agent, test.human, test.custom); (err != nil) != test.wantErr {
					t.Fatalf("validateGitAIMetadata() error = %v, want error: %t", err, test.wantErr)
				}
			})
		}
	})
	t.Run("different note conflict", func(t *testing.T) {
		tests := []struct {
			name string
			err  error
			want bool
		}{
			{name: "nil", want: false},
			{name: "matching", err: errors.New("commit already has a different attribution note"), want: true},
			{name: "other", err: errors.New("other error"), want: false},
		}
		for _, test := range tests {
			test := test
			t.Run(test.name, func(t *testing.T) {
				if got := isDifferentNoteConflict(test.err); got != test.want {
					t.Fatalf("isDifferentNoteConflict() = %t, want %t", got, test.want)
				}
			})
		}
	})
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

func commitInteropWithDate(t *testing.T, root, message, date string) string {
	t.Helper()
	runInteropGit(t, root, "add", "-A")
	runInteropGitWithEnv(t, root, []string{
		"GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_DATE=" + date,
	}, "commit", "-m", message)
	return strings.TrimSpace(runInteropGit(t, root, "rev-parse", "HEAD"))
}

func runInteropGit(t *testing.T, root string, args ...string) string {
	return runInteropGitWithEnv(t, root, nil, args...)
}

func runInteropGitWithEnv(t *testing.T, root string, extraEnv []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	command.Env = append(command.Env, extraEnv...)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
