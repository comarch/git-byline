package report

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestCollectAggregatesNotesWithoutBlobReads(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "a.go", "one\n")
	first := reportCommit(t, root, "first")
	writeReportFile(t, root, "a.go", "one\ntwo\n")
	writeReportFile(t, root, "b.go", "three\n")
	second := reportCommit(t, root, "second")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, first, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"z.go": {Blob: "deadbeef", Ranges: []model.Range{{
				Start: 1, End: 2,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}}},
			"a.go": {Blob: "badcafe1", Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "z-agent", Model: "model-z", Session: "session-z",
				},
			}}},
		},
	})
	writeReportNote(t, repo, second, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"b.go": {Blob: "faceb00c", Ranges: []model.Range{{
				Start: 1, End: 3,
				Attribution: model.Attribution{Author: model.AuthorUntracked},
			}}},
			"a.go": {Blob: "facea00c", Ranges: []model.Range{{
				Start: 1, End: 1,
				Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "a-agent", Model: "model-a", Session: "session-a",
				},
			}}},
		},
	})

	got, err := Collect(repo, first, second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commits.Total != 1 || got.Commits.Annotated != 1 {
		t.Fatalf("commits = %+v, want one commit in exclusive range", got.Commits)
	}
	if got.Totals.Lines != 4 || got.Totals.AI != 1 || got.Totals.Untracked != 3 ||
		got.Totals.Human != 0 || got.Totals.HumanOverride != 0 {
		t.Fatalf("totals = %+v", got.Totals)
	}
	if len(got.Files) != 2 || got.Files[0].Path != "a.go" || got.Files[1].Path != "b.go" {
		t.Fatalf("files are not sorted: %+v", got.Files)
	}
	if len(got.Agents) != 1 || got.Agents[0].Agent != "a-agent" ||
		len(got.Agents[0].Models) != 1 || got.Agents[0].Models[0].Model != "model-a" {
		t.Fatalf("agents are not deterministic: %+v", got.Agents)
	}
	if len(got.Sessions) != 1 || got.Sessions[0].Session != "session-a" {
		t.Fatalf("sessions = %+v", got.Sessions)
	}
	if len(got.Commit) != 1 || got.Commit[0].Commit != second || got.Commit[0].Timestamp == "" {
		t.Fatalf("commit details = %+v", got.Commit)
	}
}

func TestCollectRangeLimitIsActionable(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "file", "one\n")
	head := reportCommit(t, root, "first")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{maxAggregateCommits + 1} {
		_, err := Collect(repo, "", head, limit)
		if err == nil || !strings.Contains(err.Error(), "narrow") {
			t.Fatalf("Collect(limit=%d) error = %v, want actionable range error", limit, err)
		}
	}
}

func TestCollectRejectsInvalidCoverage(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "file", "one\n")
	head := reportCommit(t, root, "first")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":1,"files":{"file":{"blob":"abcd","ranges":[{"start":2,"end":2,"author":"human"}]}}}`)
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}
	got, err := Collect(repo, "", head, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "ignored attribution note") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestCollectRangeCounts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		end       int
		wantLines int
		wantErr   bool
	}{
		{name: "normal", end: 2, wantLines: 2},
		{name: "absurd endpoint", end: int(^uint(0) >> 1), wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			root := reportTestRepository(t)
			writeReportFile(t, root, "file", "one\ntwo\n")
			head := reportCommit(t, root, test.name)
			repo, err := gitcmd.Discover(root)
			if err != nil {
				t.Fatal(err)
			}
			data := []byte(fmt.Sprintf(
				`{"version":1,"files":{"file":{"blob":"abcd","ranges":[{"start":1,"end":%d,"author":"human"}]}}}`,
				test.end,
			))
			if err := repo.WriteNote(head, data); err != nil {
				t.Fatal(err)
			}
			got, err := Collect(repo, "", head, 0)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), head) || !strings.Contains(err.Error(), "file") {
					t.Fatalf("Collect() error = %v, want commit and file context", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Totals.Lines != test.wantLines || got.Totals.Human != test.wantLines {
				t.Fatalf("totals = %+v, want %d human lines", got.Totals, test.wantLines)
			}
		})
	}
}

func writeReportNote(t *testing.T, repo *gitcmd.Repo, commit string, note model.Note) {
	t.Helper()
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, data); err != nil {
		t.Fatal(err)
	}
}

func reportTestRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reportRunGit(t, root, "init", "-b", "main")
	reportRunGit(t, root, "config", "user.name", "Test User")
	reportRunGit(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func reportCommit(t *testing.T, root, message string) string {
	t.Helper()
	reportRunGit(t, root, "add", "-A")
	reportRunGit(t, root, "commit", "-m", message)
	return strings.TrimSpace(reportRunGit(t, root, "rev-parse", "HEAD"))
}

func writeReportFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func reportRunGit(t *testing.T, root string, args ...string) string {
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
