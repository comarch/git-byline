package report

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

const (
	coverageAgent   = "coverage-agent"
	coverageAgentA  = "agent-a"
	coverageAgentB  = "agent-b"
	coverageFile    = "file"
	coverageFirst   = "first"
	coverageLine    = "line\n"
	coverageModel   = "coverage-model"
	coverageModelA  = "model-a"
	coverageModelZ  = "model-z"
	coverageOther   = "other"
	coverageSecond  = "second"
	coverageSession = "coverage-session"
	coverageNote    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	coverageOne     = "one"
	coverageOneLine = "one\n"
	coverageTwoLine = "one\ntwo\n"
	coverageUnknown = "unknown"
	coverageWarning = "warning"

	errNegative           = "cannot be negative"
	errMalformed          = "malformed JSON"
	errOverflow           = "overflows int"
	errUnsupportedVersion = "unsupported version"
)

func TestCollectRejectsInputLimits(t *testing.T) {
	repo := &gitcmd.Repo{}
	tests := []struct {
		name  string
		limit int
		want  string
	}{
		{name: "negative", limit: -1, want: errNegative},
		{name: "too large", limit: maxAggregateCommits + 1, want: "exceeds maximum"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := Collect(repo, "", "", test.limit)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Collect() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectRejectsNilRepository(t *testing.T) {
	_, err := Collect(nil, "", "", 0)
	if err == nil || !strings.Contains(err.Error(), "repository is nil") {
		t.Fatalf("Collect() error = %v, want nil repository error", err)
	}
}

func TestCollectGitErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		mode string
		want string
	}{
		{name: "rev list", mode: "rev-list-error", want: "collect commit range"},
		{name: "note list", mode: "notes-error", want: "list attribution notes"},
		{name: "read note", mode: "read-note-error", want: "read attribution note"},
		{name: "commit time", mode: "commit-time-error", want: "read commit time"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			root := reportTestRepository(t)
			writeReportFile(t, root, coverageFile, coverageLine)
			head := reportCommit(t, root, test.name)
			repo, err := reportRepositoryWithFakeGit(t, root, test.mode, head)
			if err != nil {
				t.Fatal(err)
			}
			if test.mode == "commit-time-error" {
				writeReportNote(t, repo, head, coverageNoteData())
			}
			_, err = Collect(repo, "", head, 0)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Collect() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectSkipsUnannotatedCommits(t *testing.T) {
	root := reportTestRepository(t)
	writeReportFile(t, root, coverageFile, coverageOneLine)
	first := reportCommit(t, root, coverageFirst)
	writeReportFile(t, root, coverageFile, coverageTwoLine)
	second := reportCommit(t, root, coverageSecond)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, second, coverageNoteData())

	got, err := Collect(repo, "", second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Commits.Total != 2 || got.Commits.Annotated != 1 {
		t.Fatalf("commits = %+v, want two total and one annotated", got.Commits)
	}
	if len(got.Commit) != 1 || got.Commit[0].Commit != second || got.Commit[0].Commit == first {
		t.Fatalf("commit details = %+v", got.Commit)
	}
}

func TestCollectRejectsRangeBeyondLimit(t *testing.T) {
	root := reportTestRepository(t)
	writeReportFile(t, root, coverageFile, coverageOneLine)
	reportCommit(t, root, coverageFirst)
	writeReportFile(t, root, coverageFile, coverageTwoLine)
	head := reportCommit(t, root, coverageSecond)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Collect(repo, "", head, 1)
	if err == nil || !strings.Contains(err.Error(), "exceeds 1 commits") {
		t.Fatalf("Collect() error = %v, want range limit error", err)
	}
}

func TestCollectWarnsWhenListedNoteIsMissing(t *testing.T) {
	root := reportTestRepository(t)
	writeReportFile(t, root, coverageFile, coverageLine)
	head := reportCommit(t, root, "missing note")
	repo, err := reportRepositoryWithFakeGit(t, root, "missing-note", head)
	if err != nil {
		t.Fatal(err)
	}

	got, err := Collect(repo, "", head, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := "attribution note listed for " + head + " but is missing"
	if len(got.Warnings) != 1 || got.Warnings[0] != want {
		t.Fatalf("warnings = %v, want %q", got.Warnings, want)
	}
}

func TestCollectSortsCommitTies(t *testing.T) {
	root := reportTestRepository(t)
	writeReportFile(t, root, coverageFile, coverageOneLine)
	first := reportCommit(t, root, coverageFirst)
	writeReportFile(t, root, coverageFile, coverageTwoLine)
	second := reportCommit(t, root, coverageSecond)
	repo, err := reportRepositoryWithFakeGit(t, root, "duplicate-rev-list", second)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, second, coverageNoteData())

	got, err := Collect(repo, "", second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Commit) != 2 || got.Commit[0].Commit != second || got.Commit[1].Commit != second {
		t.Fatalf("commits = %+v, want duplicate commit tie for %s after %s", got.Commit, second, first)
	}
	if got.Commit[0].Timestamp != got.Commit[1].Timestamp {
		t.Fatalf("timestamps = %q and %q, want equal tie values", got.Commit[0].Timestamp, got.Commit[1].Timestamp)
	}
}

func TestCollectSortsDistinctCommits(t *testing.T) {
	root := reportTestRepository(t)
	writeReportFile(t, root, coverageFile, coverageOneLine)
	first := reportCommit(t, root, coverageFirst)
	writeReportFile(t, root, coverageFile, coverageTwoLine)
	second := reportCommit(t, root, coverageSecond)
	repo, err := reportRepositoryWithFakeGit(t, root, "reverse-rev-list", second)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REPORT_FAKE_COMMIT_OLD", first)
	writeReportNote(t, repo, first, coverageNoteData())
	writeReportNote(t, repo, second, coverageNoteData())

	got, err := Collect(repo, "", second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Commit) != 2 || got.Commit[0].Commit > got.Commit[1].Commit {
		t.Fatalf("commits are not sorted: %+v", got.Commit)
	}
}

func TestNoteDecodeErrorClasses(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", want: coverageUnknown},
		{
			name: "unsupported version",
			err:  reportDecodeError(t, []byte(`{"version":99,"files":{}}`)),
			want: errUnsupportedVersion,
		},
		{
			name: "version changed",
			err:  errors.New("note version changed during decode"),
			want: errUnsupportedVersion,
		},
		{
			name: "unknown field",
			err:  reportDecodeError(t, []byte(`{"version":3,"files":{},"extra":true}`)),
			want: "unknown field",
		},
		{
			name: "encoded size",
			err:  reportDecodeError(t, bytes.Repeat([]byte("x"), notes.MaxEncodedBytes+1)),
			want: "size limit",
		},
		{
			name: "file count",
			err:  reportDecodeError(t, tooManyFilesNote()),
			want: "size limit",
		},
		{
			name: "invalid character",
			err:  reportDecodeError(t, []byte{0x01}),
			want: errMalformed,
		},
		{
			name: "unexpected end",
			err:  reportDecodeError(t, []byte("{")),
			want: errMalformed,
		},
		{
			name: "multiple values",
			err:  reportDecodeError(t, []byte(`{"version":3,"files":{}}{}`)),
			want: errMalformed,
		},
		{
			name: "validation",
			err: reportDecodeError(t, []byte(fmt.Sprintf(
				`{"version":3,"files":{"%s":{"blob":"abcd1234","ranges":[{"start":2,"end":2,"author":"human"}]}}}`,
				coverageFile,
			))),
			want: "validation",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if got := noteDecodeErrorClass(test.err); got != test.want {
				t.Fatalf("noteDecodeErrorClass() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRangeLineCountRejectsBounds(t *testing.T) {
	tests := []model.Range{
		{Start: 0, End: 1},
		{Start: 2, End: 1},
	}
	for _, value := range tests {
		if _, err := rangeLineCount(value); err == nil || !strings.Contains(err.Error(), "invalid range bounds") {
			t.Fatalf("rangeLineCount(%+v) error = %v, want invalid bounds", value, err)
		}
	}
}

func TestAddTotalsRejectsUnknownAuthor(t *testing.T) {
	var totals Totals
	err := addTotals(&totals, model.Attribution{Author: model.Author("unknown")}, 1)
	if err == nil || !strings.Contains(err.Error(), "unsupported author") {
		t.Fatalf("addTotals() error = %v, want unsupported author", err)
	}
}

func TestAddTotalsRejectsCategoryOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	totals := Totals{Lines: 1, Human: maxInt}
	err := addTotals(&totals, model.Attribution{Author: model.AuthorHuman}, 1)
	if err == nil || !strings.Contains(err.Error(), errOverflow) {
		t.Fatalf("addTotals() error = %v, want overflow", err)
	}
}

func TestCheckedAddGuards(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name    string
		current int
		value   int
		want    string
	}{
		{name: "negative current", current: -1, value: 1, want: errNegative},
		{name: "negative value", current: 1, value: -1, want: errNegative},
		{name: "overflow", current: maxInt, value: 1, want: errOverflow},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := checkedAdd(test.current, test.value)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("checkedAdd(%d, %d) error = %v, want %q", test.current, test.value, err, test.want)
			}
		})
	}
}

func TestAddAgentTotalsOverflowPaths(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name    string
		prepare func(map[string]*AgentTotals, map[string]map[string]*ModelTotals, map[string]*SessionTotals)
		attr    model.Attribution
	}{
		{
			name: "agent",
			prepare: func(agents map[string]*AgentTotals, models map[string]map[string]*ModelTotals, _ map[string]*SessionTotals) {
				agents[coverageAgent] = &AgentTotals{Agent: coverageAgent, Totals: Totals{Lines: maxInt}}
				models[coverageAgent] = map[string]*ModelTotals{}
			},
			attr: model.Attribution{Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel},
		},
		{
			name: "model",
			prepare: func(agents map[string]*AgentTotals, models map[string]map[string]*ModelTotals, _ map[string]*SessionTotals) {
				agents[coverageAgent] = &AgentTotals{Agent: coverageAgent}
				models[coverageAgent] = map[string]*ModelTotals{
					coverageModel: {Model: coverageModel, Totals: Totals{Lines: maxInt}},
				}
			},
			attr: model.Attribution{Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel},
		},
		{
			name: "session",
			prepare: func(agents map[string]*AgentTotals, models map[string]map[string]*ModelTotals, sessions map[string]*SessionTotals) {
				agents[coverageAgent] = &AgentTotals{Agent: coverageAgent}
				models[coverageAgent] = map[string]*ModelTotals{
					coverageModel: {Model: coverageModel},
				}
				sessions[reportSessionKey(coverageSession, coverageAgent, coverageModel)] = &SessionTotals{
					Session: coverageSession,
					Agent:   coverageAgent,
					Model:   coverageModel,
					Totals:  Totals{Lines: maxInt},
				}
			},
			attr: model.Attribution{
				Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel, Session: coverageSession,
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			agents := map[string]*AgentTotals{}
			models := map[string]map[string]*ModelTotals{}
			sessions := map[string]*SessionTotals{}
			test.prepare(agents, models, sessions)
			err := addAgentTotals(agents, models, sessions, test.attr, 1)
			if err == nil || !strings.Contains(err.Error(), errOverflow) {
				t.Fatalf("addAgentTotals() error = %v, want overflow", err)
			}
		})
	}
}

func TestAddAgentTotalsUsesUnknownModelAndSkipsEmptySession(t *testing.T) {
	agents := map[string]*AgentTotals{}
	models := map[string]map[string]*ModelTotals{}
	sessions := map[string]*SessionTotals{}
	err := addAgentTotals(
		agents,
		models,
		sessions,
		model.Attribution{Author: model.AuthorAI, Agent: coverageAgent},
		2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 1 || len(models[coverageAgent]) != 1 || len(sessions) != 0 {
		t.Fatalf("agent maps = %#v %#v %#v", agents, models, sessions)
	}
	if got := models[coverageAgent][coverageUnknown].Lines; got != 2 {
		t.Fatalf("unknown model lines = %d, want 2", got)
	}
}

func TestSortedAgentsSortsAgentsAndModels(t *testing.T) {
	values := map[string]*AgentTotals{
		coverageAgentB: {Agent: coverageAgentB},
		coverageAgentA: {Agent: coverageAgentA},
	}
	models := map[string]map[string]*ModelTotals{
		coverageAgentB: {
			coverageModelZ: {Model: coverageModelZ},
			coverageModelA: {Model: coverageModelA},
		},
		coverageAgentA: {
			"model-c": {Model: "model-c"},
		},
	}
	got := sortedAgents(values, models)
	if len(got) != 2 || got[0].Agent != coverageAgentA || got[1].Agent != coverageAgentB {
		t.Fatalf("agents = %+v", got)
	}
	if len(got[1].Models) != 2 || got[1].Models[0].Model != coverageModelA ||
		got[1].Models[1].Model != coverageModelZ {
		t.Fatalf("models = %+v", got[1].Models)
	}
}

func TestSortedSessionsSortsAllTieKeys(t *testing.T) {
	values := map[string]*SessionTotals{
		"one-b-z":   {Session: coverageOne, Agent: coverageAgentB, Model: coverageModelZ},
		"one-a-z":   {Session: coverageOne, Agent: coverageAgentA, Model: coverageModelZ},
		"one-a-a":   {Session: coverageOne, Agent: coverageAgentA, Model: coverageModelA},
		"other-a-a": {Session: coverageOther, Agent: coverageAgentA, Model: coverageModelA},
	}
	got := sortedSessions(values)
	want := []SessionTotals{
		{Session: coverageOne, Agent: coverageAgentA, Model: coverageModelA},
		{Session: coverageOne, Agent: coverageAgentA, Model: coverageModelZ},
		{Session: coverageOne, Agent: coverageAgentB, Model: coverageModelZ},
		{Session: coverageOther, Agent: coverageAgentA, Model: coverageModelA},
	}
	if !sort.SliceIsSorted(got, func(i, j int) bool {
		if got[i].Session != got[j].Session {
			return got[i].Session < got[j].Session
		}
		if got[i].Agent != got[j].Agent {
			return got[i].Agent < got[j].Agent
		}
		return got[i].Model < got[j].Model
	}) {
		t.Fatalf("sessions are not sorted: %+v", got)
	}
	for index := range want {
		if got[index].Session != want[index].Session ||
			got[index].Agent != want[index].Agent ||
			got[index].Model != want[index].Model {
			t.Fatalf("sessions = %+v, want %+v", got, want)
		}
	}
}

func TestUniqueWarningsTrimsDeduplicatesAndSorts(t *testing.T) {
	got := uniqueWarnings([]string{" warning ", "", coverageWarning, coverageOther, "   ", " other "})
	want := []string{coverageOther, coverageWarning}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("uniqueWarnings() = %v, want %v", got, want)
	}
}

func coverageNoteData() model.Note {
	return model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: "abcd1234",
				Ranges: []model.Range{{
					Start: 1,
					End:   1,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			},
		},
	}
}

func reportDecodeError(t *testing.T, data []byte) error {
	t.Helper()
	_, err := notes.Decode(data)
	if err == nil {
		t.Fatal("notes.Decode() unexpectedly succeeded")
	}
	return err
}

func tooManyFilesNote() []byte {
	files := make([]string, notes.MaxFiles+1)
	for index := range files {
		files[index] = fmt.Sprintf(
			`"file-%d":{"blob":"abcd1234","ranges":[]}`,
			index,
		)
	}
	return []byte(`{"version":3,"files":{` + strings.Join(files, ",") + `}}`)
}

func reportSessionKey(session, agent, modelName string) string {
	return session + "\x00" + agent + "\x00" + modelName
}

func reportRepositoryWithFakeGit(t *testing.T, root, mode, commit string) (*gitcmd.Repo, error) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	binDir := t.TempDir()
	fakeGit := filepath.Join(binDir, "git")
	script := `#!/bin/sh
if [ "$REPORT_FAKE_MODE" = "rev-list-error" ] && [ "$1" = "rev-list" ]; then
  echo "forced rev-list failure" >&2
  exit 7
fi
if [ "$REPORT_FAKE_MODE" = "duplicate-rev-list" ] && [ "$1" = "rev-list" ]; then
  printf '%s\n%s\n' "$REPORT_FAKE_COMMIT" "$REPORT_FAKE_COMMIT"
  exit 0
fi
if [ "$REPORT_FAKE_MODE" = "reverse-rev-list" ] && [ "$1" = "rev-list" ]; then
  printf '%s\n%s\n' "$REPORT_FAKE_COMMIT" "$REPORT_FAKE_COMMIT_OLD"
  exit 0
fi
if [ "$REPORT_FAKE_MODE" = "notes-error" ] && [ "$1" = "notes" ]; then
  echo "forced notes failure" >&2
  exit 7
fi
if { [ "$REPORT_FAKE_MODE" = "missing-note" ] || [ "$REPORT_FAKE_MODE" = "read-note-error" ]; } && [ "$1" = "notes" ] &&
  [ "$2" = "--ref=refs/notes/byline" ] && [ "$3" = "list" ]; then
  printf '%s %s\n' "$REPORT_FAKE_OBJECT" "$REPORT_FAKE_COMMIT"
  exit 0
fi
if [ "$REPORT_FAKE_MODE" = "missing-note" ] && [ "$1" = "notes" ] &&
  [ "$2" = "--ref=refs/notes/byline" ] && [ "$3" = "show" ]; then
  exit 1
fi
if [ "$REPORT_FAKE_MODE" = "read-note-error" ] && [ "$1" = "notes" ] &&
  [ "$2" = "--ref=refs/notes/byline" ] && [ "$3" = "show" ]; then
  echo "forced note read failure" >&2
  exit 7
fi
if [ "$REPORT_FAKE_MODE" = "commit-time-error" ] && [ "$1" = "show" ] &&
  [ "$2" = "-s" ] && [ "$3" = "--format=%cI" ]; then
  echo "forced commit time failure" >&2
  exit 7
fi
exec "$REPORT_REAL_GIT" "$@"
`
	if err := os.WriteFile(fakeGit, []byte(script), 0o700); err != nil {
		return nil, err
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REPORT_FAKE_MODE", mode)
	t.Setenv("REPORT_FAKE_COMMIT", commit)
	t.Setenv("REPORT_FAKE_COMMIT_OLD", "")
	t.Setenv("REPORT_FAKE_OBJECT", coverageNote)
	t.Setenv("REPORT_REAL_GIT", realGit)
	return gitcmd.Discover(root)
}

// TestCollectRejectsOversizedRangeEndpoints covers the aggregation line
// cap: a note whose range endpoint exceeds the aggregate file line
// limit fails aggregation rather than inflating totals.
func TestCollectRejectsOversizedRangeEndpoints(t *testing.T) {
	t.Parallel()
	root := reportTestRepository(t)
	writeReportFile(t, root, "a.go", "one\n")
	commit := reportCommit(t, root, "first")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeReportNote(t, repo, commit, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"a.go": {Blob: "deadbeef", Ranges: []model.Range{{
				Start: 1, End: maxAggregateFileLines + 1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}}},
		},
	})
	_, err = Collect(repo, "", commit, 0)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Collect() error = %v, want range endpoint limit failure", err)
	}
}
