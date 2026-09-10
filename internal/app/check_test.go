package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestCheckPolicyViolationReturnsExitOneAndJSONDetails(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appCommit(t, root, "one")
	commit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	writeCheckNote(t, repo, commit, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: "droid",
					}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author: model.AuthorUntracked,
					}},
				},
			},
		},
	})

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "check", "--max-ai-percent", "40", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("check violation = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got checkResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Violations) != 1 || got.Violations[0].Rule != "max-ai-percent" ||
		got.Violations[0].ActualPercent != 50 || got.Violations[0].LimitPercent != 40 {
		t.Fatalf("violations = %+v", got.Violations)
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil,
		"check", "--max-ai-percent=50", "--max-untracked-percent=50", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("check exact limits = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestCheckRequireNoteAndFlagValidation(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	first := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(first, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	writeCheckNote(t, repo, first, model.Note{
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
	})
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appCommit(t, root, "two")
	second := appHead(t, root)

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "check", "--require-note", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("require-note = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got checkResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Violations) != 1 || got.Violations[0].Rule != "require-note" ||
		got.Violations[0].Commit != second {
		t.Fatalf("require-note violations = %+v", got.Violations)
	}

	for _, args := range [][]string{
		{"check", "--max-ai-percent", "101"},
		{"check", "--max-untracked-percent", "-1"},
		{"check", "--max-ai-percent"},
		{"check", "--max-ai-percent", "50", "--max-ai-percent", "60"},
	} {
		code, stdout, stderr, err := appRun(root, zeroTime(), nil, args...)
		if code != ExitUsage || err == nil || stdout != "" ||
			!strings.Contains(stderr, "git-byline check:") {
			t.Fatalf("check args %v = %d, %q, %q, %v", args, code, stdout, stderr, err)
		}
	}
}

func writeCheckNote(t *testing.T, repo *gitcmd.Repo, commit string, note model.Note) {
	t.Helper()
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
}
