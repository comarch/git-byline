package notes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

func TestEncodeDecode(t *testing.T) {
	t.Parallel()
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"b.go": {Blob: "bbbb", Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}}},
			"a.go": {Blob: "aaaa"},
		},
		Sessions: map[string]model.NoteSession{
			"session-1": {
				Agent: "droid", Model: "model",
				FirstTS: "2026-01-02T03:04:05Z", LastTS: "2026-01-02T03:04:06Z",
				Added: 2, Deleted: 1, Accepted: 1, Overridden: 1,
			},
		},
	}
	first, err := Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || !strings.HasSuffix(string(first), "\n") {
		t.Fatalf("encoding is not canonical: %q and %q", first, second)
	}
	if strings.Index(string(first), `"a.go"`) > strings.Index(string(first), `"b.go"`) {
		t.Fatalf("map keys are not sorted: %s", first)
	}
	got, err := Decode(first)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != note.Version || len(got.Files) != 2 || len(got.Sessions) != 1 {
		t.Fatalf("Decode() = %+v", got)
	}
}

func TestDecodeGoldenVersions(t *testing.T) {
	t.Parallel()
	for _, version := range []int{1, 2} {
		data, err := os.ReadFile(filepath.Join("testdata", fmt.Sprintf("note-v%d.json", version)))
		if err != nil {
			t.Fatalf("read v%d fixture: %v", version, err)
		}
		note, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(v%d) = %v", version, err)
		}
		if note.Version != version || len(note.Files) != 1 {
			t.Fatalf("Decode(v%d) = %+v", version, note)
		}
		file, ok := note.Files["src/example.go"]
		if !ok || len(file.Ranges) != 1 || file.Ranges[0].Author != model.AuthorAI {
			t.Fatalf("Decode(v%d) files = %+v", version, note.Files)
		}
		if version == 2 && note.Sessions["session-1"].Accepted != 1 {
			t.Fatalf("Decode(v2) sessions = %+v", note.Sessions)
		}
	}
}

func TestEncodeV2Golden(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "note-v2.json")
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	note, err := Decode(fixture)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	var compactFixture bytes.Buffer
	if err := json.Compact(&compactFixture, fixture); err != nil {
		t.Fatal(err)
	}
	compactFixture.WriteByte('\n')
	if string(encoded) != compactFixture.String() {
		t.Fatalf("Encode() differs from %s:\n%s", path, encoded)
	}
}

func TestDecodeV2Sessions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		err  bool
	}{
		{
			name: "unknown field",
			data: `{"version":2,"files":{},"sessions":{"session-1":{"agent":"droid","model":"model","first_ts":"2026-01-02T03:04:05Z","last_ts":"2026-01-02T03:04:05Z","added":1,"deleted":0,"accepted":1,"overridden":0,"extra":true}}}`,
			err:  true,
		},
		{
			name: "wrong field type",
			data: `{"version":2,"files":{},"sessions":{"session-1":{"agent":"droid","model":"model","first_ts":"2026-01-02T03:04:05Z","last_ts":"2026-01-02T03:04:05Z","added":"one","deleted":0,"accepted":1,"overridden":0}}}`,
			err:  true,
		},
		{
			name: "null session",
			data: `{"version":2,"files":{},"sessions":{"session-1":null}}`,
			err:  true,
		},
		{
			name: "valid session",
			data: `{"version":2,"files":{},"sessions":{"session-1":{"agent":"droid","model":"model","first_ts":"2026-01-02T03:04:05Z","last_ts":"2026-01-02T03:04:05Z","added":1,"deleted":0,"accepted":1,"overridden":0}}}`,
		},
		{
			name: "negative counter",
			data: `{"version":2,"files":{},"sessions":{"session-1":{"agent":"droid","model":"model","first_ts":"2026-01-02T03:04:05Z","last_ts":"2026-01-02T03:04:05Z","added":-1,"deleted":0,"accepted":1,"overridden":0}}}`,
			err:  true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := Decode([]byte(test.data))
			if (err != nil) != test.err {
				t.Fatalf("Decode() error = %v, want error: %t", err, test.err)
			}
		})
	}
}

func TestEncodeDecodeErrors(t *testing.T) {
	t.Parallel()
	if _, err := Encode(model.Note{Version: 9}); err == nil {
		t.Fatal("Encode accepted unknown version")
	}
	if _, err := Encode(model.Note{
		Version: model.NoteVersion,
		Files:   map[string]model.NoteFile{"file": {Blob: "bad"}},
	}); err == nil {
		t.Fatal("Encode accepted an invalid file")
	}
	for _, data := range []string{
		`{`,
		`{"version":9}`,
		`{"version":1,"unknown":true}`,
		`{"version":2,"sessions":[]}`,
		`{"version":2,"files":{},"sessions":{"":{"agent":"droid","model":"","first_ts":"","last_ts":"","added":0,"deleted":0,"accepted":0,"overridden":0}}}`,
		`{"version":1,"files":{"file":{"blob":"bad"}}}`,
		"{\"version\":1,\"files\":{\"bad\\npath\":{\"blob\":\"abcd\"}}}",
		`{"version":1,"files":{"file":{"blob":"abcd","ranges":[{"start":2,"end":2,"author":"human"}]}}}`,
		"{\"version\":1}\n{}",
	} {
		if _, err := Decode([]byte(data)); err == nil {
			t.Fatalf("Decode accepted %q", data)
		}
	}
}

func TestCheckFileCount(t *testing.T) {
	t.Parallel()
	data := []byte(`{"version":1,"files":{"a":{"blob":"aaaa"},"b":{"blob":"bbbb"}}}`)
	if err := CheckFileCount(data, 2); err != nil {
		t.Fatal(err)
	}
	if err := CheckFileCount(data, 1); err == nil {
		t.Fatal("CheckFileCount accepted too many files")
	}
	for _, data := range [][]byte{
		[]byte(`[]`),
		[]byte(`{"files":[]}`),
		[]byte(`{"files":{"a":`),
	} {
		if err := CheckFileCount(data, 2); err == nil {
			t.Fatalf("CheckFileCount accepted %s", data)
		}
	}
}

func TestNoteLimits(t *testing.T) {
	t.Parallel()
	files := make(map[string]model.NoteFile, MaxFiles+1)
	var encoded strings.Builder
	encoded.WriteString(`{"version":1,"files":{`)
	for index := 0; index <= MaxFiles; index++ {
		path := fmt.Sprintf("file-%03d", index)
		files[path] = model.NoteFile{Blob: "aaaa"}
		if index > 0 {
			encoded.WriteByte(',')
		}
		fmt.Fprintf(&encoded, "%q:{\"blob\":\"aaaa\"}", path)
	}
	encoded.WriteString("}}")
	if _, err := Encode(model.Note{Version: model.NoteVersion, Files: files}); err == nil {
		t.Fatal("Encode accepted too many files")
	}
	if _, err := Decode([]byte(encoded.String())); err == nil {
		t.Fatal("Decode accepted too many files")
	}
	largePath := strings.Repeat("a", MaxEncodedBytes)
	if _, err := Encode(model.Note{
		Version: model.NoteVersion,
		Files:   map[string]model.NoteFile{largePath: {Blob: "aaaa"}},
	}); err == nil {
		t.Fatal("Encode accepted an oversized note")
	}
	if _, err := Decode(make([]byte, MaxEncodedBytes+1)); err == nil {
		t.Fatal("Decode accepted an oversized note")
	}
}

func TestFindFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	notesGit(t, root, "init", "-b", "main")
	notesGit(t, root, "config", "user.name", "Test User")
	notesGit(t, root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notesGit(t, root, "add", "file")
	notesGit(t, root, "commit", "-m", "one")
	head := strings.TrimSpace(notesGit(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, _, err := repo.BlobID(head, "file")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, warnings, err := FindFile(repo, "", "file", blob); err != nil || found || len(warnings) != 0 {
		t.Fatalf("FindFile(empty) = %t, %v, %v", found, warnings, err)
	}
	if _, found, _, err := FindFile(repo, head, "file", blob); err != nil || found {
		t.Fatalf("FindFile(no note) = %t, %v", found, err)
	}
	data, err := Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file": {
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
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	file, found, warnings, err := FindFile(repo, head, "file", blob)
	if err != nil || !found || len(warnings) != 0 || file.Blob != blob {
		t.Fatalf("FindFile(valid) = %+v, %t, %v, %v", file, found, warnings, err)
	}
	if _, found, warnings, err := FindFile(repo, head, "file", "aaaa"); err != nil || found || len(warnings) != 1 {
		t.Fatalf("FindFile(mismatch) = %t, %v, %v", found, warnings, err)
	}
}

func notesGit(t *testing.T, root string, args ...string) string {
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
