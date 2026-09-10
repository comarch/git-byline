package notes

import (
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
	if got.Version != note.Version || len(got.Files) != 2 {
		t.Fatalf("Decode() = %+v", got)
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
