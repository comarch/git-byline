package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestVerifyFastAndDeepAcceptBlobPinnedNote(t *testing.T) {
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
	writeVerifyNote(t, repo, commit, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start:       1,
					End:         2,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	})

	for _, args := range [][]string{{"verify", "--json"}, {"verify", "--deep", "--json"}} {
		code, stdout, stderr, err := appRun(root, zeroTime(), nil, args...)
		if code != ExitSuccess || err != nil || stderr != "" {
			t.Fatalf("%v = %d, %q, %q, %v", args, code, stdout, stderr, err)
		}
		var got verifyResult
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatal(err)
		}
		if got.Commits.Total != 1 || got.Commits.Annotated != 1 || len(got.Issues) != 0 {
			t.Fatalf("%v result = %+v", args, got)
		}
	}
}

func TestVerifyReportsBlobMismatchAndInvalidRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		ranges []model.Range
		want   string
		deep   bool
	}{
		{
			name: "blob mismatch",
			ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}},
			want: "does not match commit blob",
		},
		{
			name: "deep line mismatch",
			ranges: []model.Range{{
				Start: 1, End: 2,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}},
			deep: true,
			want: "range coverage is 2 lines, blob has 1",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			root := appRepo(t)
			appWrite(t, root, "file.txt", "one\n")
			appCommit(t, root, test.name)
			commit := appHead(t, root)
			repo, err := gitcmd.Discover(root)
			if err != nil {
				t.Fatal(err)
			}
			blob := "deadbeef"
			if test.deep {
				var exists bool
				blob, exists, err = repo.BlobID(commit, "file.txt")
				if err != nil || !exists {
					t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
				}
			}
			writeVerifyNote(t, repo, commit, model.Note{
				Version: model.NoteVersion,
				Files: map[string]model.NoteFile{
					"file.txt": {Blob: blob, Ranges: test.ranges},
				},
			})
			args := []string{"verify", "--json"}
			if test.deep {
				args = append(args, "--deep")
			}
			code, stdout, stderr, err := appRun(root, zeroTime(), nil, args...)
			if code != ExitFailure || err == nil || stderr != "" {
				t.Fatalf("verify = %d, %q, %q, %v", code, stdout, stderr, err)
			}
			var got verifyResult
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Issues) != 1 || !strings.Contains(got.Issues[0].Message, test.want) {
				t.Fatalf("issues = %+v, want %q", got.Issues, test.want)
			}
		})
	}
}

func TestVerifyReportsNonBlobTreeEntry(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	commit := appHead(t, root)
	appGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+commit+",submodule")
	appGit(t, root, "commit", "-m", "gitlink")
	commit = appHead(t, root)

	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	writeVerifyNote(t, repo, commit, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
			"submodule": {
				Blob: commit,
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}},
			},
		},
	})

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("verify gitlink = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got verifyResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Commits.Annotated != 1 || len(got.Issues) != 1 ||
		got.Issues[0].Path != "submodule" ||
		got.Issues[0].Message != "non-blob tree entry" {
		t.Fatalf("gitlink issues = %+v", got.Issues)
	}
}

func TestVerifyRejectsUnnormalizedPathAndCommitRangeLimit(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
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
	raw := `{"version":1,"files":{"./file.txt":{"blob":"` + blob + `","ranges":[{"start":1,"end":1,"author":"human"}]}}}`
	if err := repo.WriteNote(commit, []byte(raw)); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("verify path = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got verifyResult
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Issues) != 1 || !strings.Contains(got.Issues[0].Message, "decode note") {
		t.Fatalf("path issues = %+v", got.Issues)
	}
}

func TestVerifyDecodeDiagnosticsDoNotEchoNoteContent(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	commit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	garbage := "garbage-note-content-that-must-not-be-echoed"
	if err := repo.WriteNote(commit, []byte(garbage)); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "verify", "--json")
	if code != ExitFailure || err == nil || stderr != "" {
		t.Fatalf("verify garbage = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if strings.Contains(stdout, garbage) || strings.Contains(stderr, garbage) ||
		strings.Contains(err.Error(), garbage) {
		t.Fatalf("garbage note content leaked: stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
	if !strings.Contains(stdout, "malformed JSON") {
		t.Fatalf("garbage issue = %q", stdout)
	}
}

func writeVerifyNote(t *testing.T, repo *gitcmd.Repo, commit string, note model.Note) {
	t.Helper()
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
}
