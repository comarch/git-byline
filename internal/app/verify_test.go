package app

import (
	"encoding/json"
	"errors"
	"flag"
	"io"
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

func TestWriteVerifyText(t *testing.T) {
	t.Parallel()
	var clean verifyResult
	clean.Commits.Total = 3
	clean.Commits.Annotated = 2
	var out strings.Builder
	writeVerifyText(&Env{Stdout: &out, Stderr: io.Discard}, clean)
	if got := out.String(); got != "verified 2 annotated commits (3 commits total)\n" {
		t.Fatalf("clean verify text = %q", got)
	}

	failed := clean
	failed.Issues = []verifyIssue{
		{Commit: "abcd1234", Message: "note is invalid"},
		{Commit: "abcd1234", Path: "src/example.go", Message: "range coverage is incomplete"},
	}
	out.Reset()
	writeVerifyText(&Env{Stdout: &out, Stderr: io.Discard}, failed)
	for _, want := range []string{
		"FAIL abcd1234: note is invalid\n",
		"FAIL abcd1234 src/example.go: range coverage is incomplete\n",
		"verified 2 annotated commits (3 commits total), 2 issue(s)\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("verify text = %q, want %q", out.String(), want)
		}
	}
}

func TestVerifyHelperClassification(t *testing.T) {
	t.Parallel()
	if isTreeEntryIssue(nil) {
		t.Error("isTreeEntryIssue(nil) = true")
	}
	for _, message := range []string{"git returned invalid tree entry", "git returned non-blob tree entry"} {
		if !isTreeEntryIssue(errors.New(message)) {
			t.Errorf("isTreeEntryIssue(%q) = false", message)
		}
	}
	if isTreeEntryIssue(errors.New("some other failure")) {
		t.Error("isTreeEntryIssue(other) = true")
	}

	classes := []struct {
		message string
		want    string
	}{
		{"unsupported note version 9", "unsupported version"},
		{"note version changed during decode", "unsupported version"},
		{"json: unknown field \"extra\"", "unknown field"},
		{"note exceeds 16777216 bytes", "size limit"},
		{"note has more than 500 files", "size limit"},
		{"invalid character 'x'", "malformed JSON"},
		{"unexpected end of JSON input", "malformed JSON"},
		{"multiple JSON values in note", "malformed JSON"},
		{"note file path is not normalized", "validation"},
	}
	for _, test := range classes {
		test := test
		t.Run(test.want+"/"+test.message, func(t *testing.T) {
			t.Parallel()
			if got := noteDecodeErrorClass(errors.New(test.message)); got != test.want {
				t.Fatalf("noteDecodeErrorClass(%q) = %q, want %q", test.message, got, test.want)
			}
		})
	}
	if got := noteDecodeErrorClass(nil); got != "unknown" {
		t.Fatalf("noteDecodeErrorClass(nil) = %q", got)
	}
}

func TestVerifyRangeCoverage(t *testing.T) {
	t.Parallel()
	if count, err := verifyRangeCoverage(nil); count != 0 || err != nil {
		t.Fatalf("verifyRangeCoverage(nil) = %d, %v", count, err)
	}
	complete := []model.Range{
		{Start: 1, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 3, End: 4, Attribution: model.Attribution{Author: model.AuthorUntracked}},
	}
	if count, err := verifyRangeCoverage(complete); count != 4 || err != nil {
		t.Fatalf("verifyRangeCoverage(complete) = %d, %v", count, err)
	}
	gapped := []model.Range{
		{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 3, End: 3, Attribution: model.Attribution{Author: model.AuthorHuman}},
	}
	if count, err := verifyRangeCoverage(gapped); count != 3 || err == nil {
		t.Fatalf("verifyRangeCoverage(gapped) = %d, %v", count, err)
	}
	negative := []model.Range{{Start: 1, End: -1, Attribution: model.Attribution{Author: model.AuthorHuman}}}
	if _, err := verifyRangeCoverage(negative); err == nil {
		t.Fatal("verifyRangeCoverage(negative) returned no error")
	}
}

func TestParseVerifyArgs(t *testing.T) {
	t.Parallel()
	deep, jsonOutput, rest, err := parseVerifyArgs([]string{"--deep", "--json", "HEAD~2..HEAD"})
	if !deep || !jsonOutput || err != nil || len(rest) != 1 || rest[0] != "HEAD~2..HEAD" {
		t.Fatalf("parseVerifyArgs() = %t, %t, %v, %v", deep, jsonOutput, rest, err)
	}
	for _, args := range [][]string{
		{"--deep", "--deep"},
		{"--json", "--json"},
		{"--unknown"},
	} {
		if _, _, _, err := parseVerifyArgs(args); err == nil {
			t.Errorf("parseVerifyArgs(%v) returned no error", args)
		}
	}
	if _, _, _, err := parseVerifyArgs([]string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("parseVerifyArgs(-h) error = %v", err)
	}
}
