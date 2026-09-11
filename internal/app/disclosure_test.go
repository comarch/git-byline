package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestDisclosureCommandWritesAllFormats(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.go", "one\n")
	appCommit(t, root, "one")
	head := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.go": {
				Blob: "deadbeef",
				Ranges: []model.Range{{
					Start: 1,
					End:   1,
					Attribution: model.Attribution{
						Author: model.AuthorAI,
						Agent:  "droid",
						Model:  "model-a",
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

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, format := range []string{"json", "cyclonedx", "spdx"} {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), format+".json")
			code, stdout, stderr, err := appRun(root, now, nil,
				"disclosure", "--range", head, "--format", format, "--output", path)
			if code != ExitSuccess || err != nil || stdout != path+"\n" || stderr != "" {
				t.Fatalf("disclosure %s = %d, %q, %q, %v", format, code, stdout, stderr, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
				t.Fatalf("disclosure mode = %o", info.Mode().Perm())
			}
			output, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var document map[string]any
			if err := json.Unmarshal(output, &document); err != nil {
				t.Fatal(err)
			}
			switch format {
			case "json":
				if document["format"] != "git-byline-disclosure" ||
					document["generated_at"] != now.Format(time.RFC3339) {
					t.Fatalf("native document = %v", document)
				}
			case "cyclonedx":
				if document["bomFormat"] != "CycloneDX" || document["specVersion"] != "1.6" {
					t.Fatalf("CycloneDX document = %v", document)
				}
			case "spdx":
				if document["@context"] != "https://spdx.org/rdf/3.0.1/spdx-context.jsonld" {
					t.Fatalf("SPDX document = %v", document)
				}
				if !strings.Contains(string(output), `"type": "ai_AIPackage"`) ||
					!strings.Contains(string(output), `"relationshipType": "hasDeclaredLicense"`) {
					t.Fatalf("SPDX AI profile fields missing: %s", output)
				}
			}
		})
	}
}

func TestDisclosureCommandWritesDocumentToStdoutWithoutOutputFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("TMPDIR", tempDir)
	root := appRepo(t)
	appWrite(t, root, "file.go", "one\n")
	appCommit(t, root, "one")
	head := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.go": {
				Blob: "deadbeef",
				Ranges: []model.Range{{
					Start: 1,
					End:   1,
					Attribution: model.Attribution{
						Author: model.AuthorAI,
						Agent:  "droid",
						Model:  "model-a",
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

	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"disclosure", "--range", head)
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("disclosure stdout = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("stdout is not the full document: %v", err)
	}
	if document["format"] != "git-byline-disclosure" {
		t.Fatalf("stdout document format = %v", document["format"])
	}
	entries, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("stdout mode created files: %v", entries)
	}
}

func TestDisclosureCommandRefusesOutputReplacementAndInvalidPaths(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.go", "one\n")
	appCommit(t, root, "one")
	path := filepath.Join(t.TempDir(), "disclosure.json")
	const existing = "keep\n"
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err := appRun(root, time.Time{}, nil, "disclosure", "--output", path)
	if code != ExitFailure || err == nil {
		t.Fatalf("disclosure overwrite = %d, %v", code, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != existing {
		t.Fatalf("existing disclosure changed to %q", data)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatalf("failed publish left temporary files: %v", entries)
	}
	for _, output := range []string{"bad\nname.json", "bad\tname.json", "-"} {
		code, _, _, err := appRun(root, time.Time{}, nil,
			"disclosure", "--output", output)
		if code != ExitFailure || err == nil {
			t.Fatalf("disclosure output %q = %d, %v", output, code, err)
		}
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"disclosure", "--format", "unknown")
	if code != ExitUsage || err == nil || stdout != "" ||
		!strings.Contains(stderr, "--format must be") {
		t.Fatalf("disclosure format = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestDisclosureOutputFailureRemovesTemporaryFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "disclosure.json")
	file, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	err = writeDisclosureOutputWithLink(file, path, []byte("document\n"),
		func(string, string) error { return os.ErrPermission })
	if err == nil {
		t.Fatal("failed disclosure publish succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed publish left files: %v", entries)
	}
}
