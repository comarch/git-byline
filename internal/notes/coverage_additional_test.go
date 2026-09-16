package notes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

const (
	coverageBlob       = "abcd1234"
	coverageFile       = "file"
	coverageMissing    = "missing"
	coverageNotesRef   = "refs/notes/byline"
	coverageSessionKey = "droid::session-1"
	coverageAgent      = "droid"
	coverageModel      = "model"
	coverageTimestamp  = "2026-01-02T03:04:05Z"
	invalidNameMessage = "invalid name"
)

func TestDecodeAdditionalBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
	}{
		{
			name: "missing files",
			data: `{"version":2,"sessions":{}}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			note, err := Decode([]byte(test.data))
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if note.Files == nil {
				t.Fatal("Decode() returned nil files")
			}
		})
	}
}

func TestCheckFileCountAdditionalErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		data  []byte
		limit int
		want  string
	}{
		{
			name:  "negative limit",
			data:  []byte(`{}`),
			limit: -1,
			want:  "note file limit is negative",
		},
		{
			name:  "invalid start",
			data:  []byte{},
			limit: 1,
			want:  "decode note start",
		},
		{
			name:  "malformed non-files field",
			data:  []byte(`{"version":}`),
			limit: 1,
			want:  `decode note field "version"`,
		},
		{
			name:  "malformed files value",
			data:  []byte(`{"files":`),
			limit: 1,
			want:  "decode note files",
		},
		{
			name:  "non-string file name",
			data:  []byte(`{"files":{1:"value"}}`),
			limit: 1,
			want:  "decode note file name",
		},
		{
			name:  "invalid files closing token",
			data:  []byte(`{"files":{"a":"value"]}`),
			limit: 1,
			want:  "decode note files end",
		},
		{
			name:  "invalid note closing token",
			data:  []byte(`{"files":{"a":"value"}]`),
			limit: 1,
			want:  "decode note end",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := CheckFileCount(test.data, test.limit)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("CheckFileCount() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateNoteAdditionalBranches(t *testing.T) {
	t.Parallel()

	validSession := coverageNoteSession()
	tests := []struct {
		name string
		note model.Note
		want string
	}{
		{
			name: "sessions require version two",
			note: model.Note{
				Version:  model.NoteVersionV1,
				Sessions: map[string]model.NoteSession{coverageSessionKey: validSession},
			},
			want: "sessions require version 2",
		},
		{
			name: "invalid session is returned",
			note: model.Note{
				Version:  model.NoteVersion,
				Sessions: map[string]model.NoteSession{coverageSessionKey: {}},
			},
			want: "agent must not be empty",
		},
		{
			name: "path must be normalized",
			note: model.Note{
				Version: model.NoteVersion,
				Files: map[string]model.NoteFile{
					"dir/../file": {Blob: coverageBlob},
				},
			},
			want: "is not normalized",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateNote(test.note)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateNote() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestValidateSessionAdditionalBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sessionName string
		session     model.NoteSession
		want        string
	}{
		{
			name:        "empty name",
			sessionName: "",
			session:     coverageNoteSession(),
			want:        "name must not be empty",
		},
		{
			name:        "long name",
			sessionName: strings.Repeat("a", 1025),
			session:     coverageNoteSession(),
			want:        invalidNameMessage,
		},
		{
			name:        "invalid utf eight name",
			sessionName: string([]byte{0xff}),
			session:     coverageNoteSession(),
			want:        invalidNameMessage,
		},
		{
			name:        "control name",
			sessionName: coverageSessionKey + "\x01",
			session:     coverageNoteSession(),
			want:        invalidNameMessage,
		},
		{
			name:        "empty agent",
			sessionName: coverageSessionKey,
			session:     model.NoteSession{},
			want:        "agent must not be empty",
		},
		{
			name:        "separator in agent",
			sessionName: coverageSessionKey,
			session:     model.NoteSession{Agent: "droid::other"},
			want:        "reserved separator",
		},
		{
			name:        "long model",
			sessionName: coverageSessionKey,
			session: model.NoteSession{
				Agent: coverageAgent,
				Model: strings.Repeat("m", 1025),
			},
			want: "model exceeds 1024 bytes",
		},
		{
			name:        "invalid utf eight model",
			sessionName: coverageSessionKey,
			session: model.NoteSession{
				Agent: coverageAgent,
				Model: string([]byte{0xff}),
			},
			want: "model is not valid UTF-8",
		},
		{
			name:        "control model",
			sessionName: coverageSessionKey,
			session: model.NoteSession{
				Agent: coverageAgent,
				Model: "model\x01",
			},
			want: "model contains a control character",
		},
		{
			name:        "invalid timestamp",
			sessionName: coverageSessionKey,
			session: model.NoteSession{
				Agent:   coverageAgent,
				FirstTS: "not-a-timestamp",
			},
			want: "first_ts is not RFC3339",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateSession(test.sessionName, test.session)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateSession() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestFindFileHistoryError(t *testing.T) {
	t.Parallel()

	fixture := newNotesRepository(t)
	_, found, warnings, err := FindFile(fixture.repo, coverageBlob, coverageFile, fixture.blob)
	if err == nil || found || len(warnings) != 0 {
		t.Fatalf("FindFile(history error) = %t, %v, %v", found, warnings, err)
	}
}

func TestFindFileReadNoteError(t *testing.T) {
	t.Parallel()

	fixture := newNotesRepository(t)
	notesGit(t, fixture.root, "update-ref", coverageNotesRef, fixture.blob)
	_, found, warnings, err := FindFile(fixture.repo, fixture.head, coverageFile, fixture.blob)
	if err == nil || found || len(warnings) != 0 {
		t.Fatalf("FindFile(read note error) = %t, %v, %v", found, warnings, err)
	}
}

func TestFindFileWarningsAndMissingFile(t *testing.T) {
	t.Parallel()

	fixture := newNotesRepository(t)
	if err := fixture.repo.WriteNote(fixture.head, []byte(`{"version":9}`)); err != nil {
		t.Fatal(err)
	}
	_, found, warnings, err := FindFile(fixture.repo, fixture.head, coverageFile, fixture.blob)
	if err != nil || found || len(warnings) != 1 {
		t.Fatalf("FindFile(invalid note) = %t, %v, %v", found, warnings, err)
	}
	if err := fixture.repo.DeleteNoteRef(coverageNotesRef, fixture.head); err != nil {
		t.Fatal(err)
	}
	data, err := Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {Blob: fixture.blob},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.repo.WriteNote(fixture.head, data); err != nil {
		t.Fatal(err)
	}
	_, found, warnings, err = FindFile(fixture.repo, fixture.head, coverageMissing, fixture.blob)
	if err != nil || found || len(warnings) != 0 {
		t.Fatalf("FindFile(missing file) = %t, %v, %v", found, warnings, err)
	}
}

type notesRepositoryFixture struct {
	root string
	repo *gitcmd.Repo
	head string
	blob string
}

func newNotesRepository(t *testing.T) notesRepositoryFixture {
	t.Helper()

	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	notesGit(t, root, "init", "-b", "main")
	notesGit(t, root, "config", "user.name", "Test User")
	notesGit(t, root, "config", "user.email", "test@example.invalid")
	if err := os.WriteFile(filepath.Join(root, coverageFile), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	notesGit(t, root, "add", coverageFile)
	notesGit(t, root, "commit", "-m", "one")
	head := strings.TrimSpace(notesGit(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, _, err := repo.BlobID(head, coverageFile)
	if err != nil {
		t.Fatal(err)
	}
	return notesRepositoryFixture{root: root, repo: repo, head: head, blob: blob}
}

func coverageNoteSession() model.NoteSession {
	return model.NoteSession{
		Agent:   coverageAgent,
		Model:   coverageModel,
		FirstTS: coverageTimestamp,
		LastTS:  coverageTimestamp,
	}
}
