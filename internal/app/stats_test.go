package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/report"
)

func TestStatsCommandReportsDeterministicJSONWithoutBlobReads(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	commit := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: "deadbeef",
				Ranges: []model.Range{{
					Start: 1,
					End:   1,
					Attribution: model.Attribution{
						Author:  model.AuthorAI,
						Agent:   "droid",
						Model:   "model-a",
						Session: "session-a",
					},
				}},
			},
		},
	}
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "stats", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("stats JSON = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got report.Aggregate
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != model.NoteVersion || got.Commits.Total != 1 || got.Commits.Annotated != 1 {
		t.Fatalf("stats metadata = %+v", got)
	}
	if got.Totals.Lines != 1 || got.Totals.AI != 1 || len(got.Files) != 1 ||
		got.Files[0].Path != "file.txt" {
		t.Fatalf("stats totals = %+v", got)
	}
	if len(got.Agents) != 1 || got.Agents[0].Agent != "droid" ||
		len(got.Agents[0].Models) != 1 || got.Agents[0].Models[0].Model != "model-a" {
		t.Fatalf("stats agent totals = %+v", got.Agents)
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil, "stats")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("stats text = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	for _, want := range []string{"Commits: 1 total, 1 annotated", "AI: 1", "file.txt", "droid"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stats text = %q, want %q", stdout, want)
		}
	}
}

func TestStatsCommandRevisionRangeAndUsage(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "one")
	first := appHead(t, root)
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appCommit(t, root, "two")
	second := appHead(t, root)

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "stats", first+".."+second, "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("stats range = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var got report.Aggregate
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if got.Commits.Total != 1 || got.To != second || got.From != first {
		t.Fatalf("stats range result = %+v", got)
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil, "stats", "one", "two")
	if code != ExitUsage || err == nil || stdout != "" || !strings.Contains(stderr, "at most one") {
		t.Fatalf("stats usage = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	code, stdout, stderr, err = appRun(root, zeroTime(), nil, "stats", "--help")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "Usage: git-byline stats") {
		t.Fatalf("stats help = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestWriteStatsTextReportsPeople(t *testing.T) {
	t.Parallel()
	aggregate := report.Aggregate{
		Version: model.NoteVersion,
		From:    "aaaa",
		To:      "bbbb",
		Totals:  report.Totals{Human: 6, HumanOverride: 2, Lines: 8},
		Authors: []report.AuthorTotals{
			{Identity: "", Totals: report.Totals{Human: 2, Lines: 2}},
			{Identity: "john.doe", Totals: report.Totals{Human: 4, HumanOverride: 2, Lines: 6}},
		},
		Commit: []report.CommitTotals{{
			Commit:    "0123456789abcdef0123456789abcdef01234567",
			Timestamp: "2026-01-02T03:04:05Z",
			Totals:    report.Totals{Human: 8, Lines: 8},
		}},
	}
	var out strings.Builder
	writeStatsText(&out, aggregate)
	text := out.String()
	for _, want := range []string{
		"Range: aaaa..bbbb",
		"Authors:",
		"  (unidentified): 2 lines (human 2, human override 0)",
		"  john.doe: 6 lines (human 4, human override 2)",
		"  0123456789ab (2026-01-02T03:04:05Z)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("stats text = %q, want %q", text, want)
		}
	}
	if strings.Contains(text, "0123456789abcdef0123456789abcdef01234567") {
		t.Error("stats text prints the full commit identifier")
	}
}

func TestShortCommitAndDisplayHelpers(t *testing.T) {
	t.Parallel()
	if got := shortCommit("0123456789abcdef"); got != "0123456789ab" {
		t.Fatalf("shortCommit() = %q", got)
	}
	if got := shortCommit("abcd"); got != "abcd" {
		t.Fatalf("shortCommit(short) = %q", got)
	}
	if got := displayIdentity(""); got != "(unidentified)" {
		t.Fatalf("displayIdentity(empty) = %q", got)
	}
	if got := displayIdentity("john.doe"); got != "john.doe" {
		t.Fatalf("displayIdentity() = %q", got)
	}
	for _, test := range []struct {
		from string
		to   string
		want string
	}{
		{"a", "b", "a..b"},
		{"a", "", "a..HEAD"},
		{"", "b", "b"},
		{"", "", "HEAD"},
	} {
		if got := displayRevisionRange(test.from, test.to); got != test.want {
			t.Fatalf("displayRevisionRange(%q, %q) = %q, want %q", test.from, test.to, got, test.want)
		}
	}
}

func appHead(t *testing.T, root string) string {
	t.Helper()
	return strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
}

func zeroTime() (zero time.Time) {
	return zero
}
