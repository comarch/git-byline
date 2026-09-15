package dashboard

import (
	"errors"
	"html/template"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/report"
)

const (
	coverageCommit    = "abcd1234"
	coverageBlob      = "beef1234"
	coverageAgent     = "droid"
	coverageModel     = "model"
	coverageTimestamp = "2026-01-01T00:00:00Z"
	coverageAI        = "ai"
	coverageHuman     = "human"
	coverageOther     = "other"
	coverageUntracked = "untracked"
	coverageA         = "a"
	coverageB         = "b"
	coverageFew       = "few"
	coverageMany      = "many"
	coverageZ         = "z"
	coverageEmpty     = ""
	coverageExceeds   = "exceeds"
	coverageShared    = "shared warning"
	coverageSeparator = "\x00"
	coverageLineTotal = " line total"
	coverageIdentity  = "john.doe"
)

func TestRenderTemplateGuards(t *testing.T) {
	original := reportTemplate
	defer func() {
		reportTemplate = original
	}()

	reportTemplate = failingTemplate(t, "dashboard-failure")
	_, err := Render(Report{Commit: coverageCommit})
	if err == nil || !strings.Contains(err.Error(), "render dashboard") {
		t.Fatalf("Render template error = %v", err)
	}

	reportTemplate = oversizedTemplate(t, "dashboard-large")
	_, err = Render(Report{Commit: coverageCommit})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("Render size error = %v", err)
	}
}

func TestDashboardSourceMappingAndTones(t *testing.T) {
	tests := []struct {
		name  string
		value model.Attribution
		key   string
		label string
		kind  string
	}{
		{
			name:  "AI unknown model",
			value: model.Attribution{Author: model.AuthorAI, Agent: coverageAgent},
			key:   coverageAI + coverageSeparator + coverageAgent + coverageSeparator + "unknown" + coverageSeparator,
			label: "ai:droid/unknown",
			kind:  coverageAI,
		},
		{
			name:  "human override identity",
			value: model.Attribution{Author: model.AuthorHumanOverride, Identity: coverageIdentity, Agent: coverageAgent, Model: coverageModel},
			key:   "human-override" + coverageSeparator + coverageAgent + coverageSeparator + coverageModel + coverageSeparator + coverageIdentity,
			label: "human-override:john.doe/droid/model",
			kind:  "human-override",
		},
		{
			name:  "human without identity",
			value: model.Attribution{Author: model.AuthorHuman},
			key:   coverageHuman + coverageSeparator,
			label: "human",
			kind:  coverageHuman,
		},
		{
			name:  "untracked fallback",
			value: model.Attribution{},
			key:   coverageUntracked,
			label: coverageUntracked,
			kind:  coverageUntracked,
		},
		{
			name:  "unknown class",
			value: model.Attribution{Author: model.Author(coverageOther)},
			key:   coverageOther,
			label: coverageOther,
			kind:  coverageOther,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			key, label, kind := sourceOf(test.value)
			if key != test.key || label != test.label || kind != test.kind {
				t.Fatalf("sourceOf() = %q, %q, %q", key, label, kind)
			}
		})
	}

	toneTests := []struct {
		agent string
		want  string
	}{
		{agent: coverageAgent, want: "tone-cyan"},
		{agent: "factory", want: "tone-cyan"},
		{agent: "claude", want: "tone-magenta-light"},
		{agent: "codex", want: "tone-cyan-deep"},
		{agent: "gemini", want: "tone-violet-light"},
		{agent: "copilot", want: "tone-blue"},
		{agent: "vscode", want: "tone-blue-light"},
		{agent: "windsurf", want: "tone-blue-light"},
		{agent: "cursor", want: "tone-red-light"},
		{agent: "grok", want: "tone-magenta"},
	}
	for _, test := range toneTests {
		test := test
		t.Run(test.agent, func(t *testing.T) {
			key := coverageAI + coverageSeparator + test.agent + coverageSeparator + coverageModel
			if got := aiTone(key); got != test.want {
				t.Fatalf("aiTone(%q) = %q, want %q", key, got, test.want)
			}
		})
	}
	fallback := aiTone("unstructured")
	if fallback == coverageEmpty {
		t.Fatal("aiTone returned an empty fallback")
	}
	if !containsString(palette, fallback) {
		t.Fatalf("aiTone fallback %q is not in palette", fallback)
	}
}

func TestDashboardSourceOrderingAndWarnings(t *testing.T) {
	orderTests := []struct {
		name   string
		values map[string]*sourceCount
		want   []string
	}{
		{
			name: "rank",
			values: map[string]*sourceCount{
				coverageHuman: {key: coverageHuman, kind: coverageHuman, label: coverageHuman, lines: 1},
				coverageAI:    {key: coverageAI, kind: coverageAI, label: coverageAI, lines: 1},
			},
			want: []string{coverageHuman, coverageAI},
		},
		{
			name: "line count",
			values: map[string]*sourceCount{
				coverageFew:  {key: coverageFew, kind: coverageAI, label: coverageAI + ":few", lines: 1},
				coverageMany: {key: coverageMany, kind: coverageAI, label: coverageAI + ":many", lines: 2},
			},
			want: []string{coverageMany, coverageFew},
		},
		{
			name: "label",
			values: map[string]*sourceCount{
				coverageZ: {key: coverageZ, kind: coverageAI, label: coverageAI + ":z", lines: 1},
				coverageA: {key: coverageA, kind: coverageAI, label: coverageAI + ":a", lines: 1},
			},
			want: []string{coverageA, coverageZ},
		},
		{
			name: "key",
			values: map[string]*sourceCount{
				coverageB: {key: coverageB, kind: coverageAI, label: coverageAI + ":same", lines: 1},
				coverageA: {key: coverageA, kind: coverageAI, label: coverageAI + ":same", lines: 1},
			},
			want: []string{coverageA, coverageB},
		},
	}
	for _, test := range orderTests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			ordered := sortedSourceCounts(test.values)
			if len(ordered) != len(test.want) {
				t.Fatalf("sorted source count length = %d, want %d", len(ordered), len(test.want))
			}
			for index, want := range test.want {
				if ordered[index].key != want {
					t.Errorf("ordered[%d].key = %q, want %q", index, ordered[index].key, want)
				}
			}
		})
	}
	if got := sourceRank(coverageOther); got != 3 {
		t.Fatalf("sourceRank(unknown) = %d, want 3", got)
	}

	warnings := Report{
		Commit: coverageCommit,
		Status: provenance.StatusResult{
			Head:                coverageCommit,
			LastAnnotatedCommit: coverageCommit,
			Warnings:            []string{"invalid attribution note", coverageShared},
		},
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion,
			File:    "source.go",
			Blob:    coverageBlob,
			Commit:  coverageCommit,
			Warnings: []string{
				"missing attribution note",
				coverageShared,
			},
			Lines: []provenance.BlameLine{{
				Number:      1,
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}},
		}},
	}
	data, err := Render(warnings)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, warning := range []string{"invalid attribution note", "missing attribution note", coverageShared} {
		if !strings.Contains(text, warning) {
			t.Errorf("rendered warnings missing %q", warning)
		}
	}
	if strings.Count(text, coverageShared) != 2 {
		t.Fatalf("shared warning count = %d", strings.Count(text, coverageShared))
	}

	got := uniqueStrings([]string{" warning ", coverageEmpty, "warning", "error", " "})
	want := []string{"error", "warning"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("uniqueStrings() = %#v, want %#v", got, want)
	}
}

func TestRenderRangeTemplateGuards(t *testing.T) {
	original := rangeReportTemplate
	defer func() {
		rangeReportTemplate = original
	}()

	rangeReportTemplate = failingTemplate(t, "range-failure")
	_, err := RenderRange(validRangeAggregate())
	if err == nil || !strings.Contains(err.Error(), "render dashboard range") {
		t.Fatalf("RenderRange template error = %v", err)
	}

	rangeReportTemplate = oversizedTemplate(t, "range-large")
	_, err = RenderRange(validRangeAggregate())
	if err == nil || !strings.Contains(err.Error(), coverageExceeds) {
		t.Fatalf("RenderRange size error = %v", err)
	}
}

func TestBuildRangeViewRejectsUncoveredInputs(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	tests := []struct {
		name   string
		change func(*report.Aggregate)
		want   string
	}{
		{
			name: "too many commits",
			change: func(value *report.Aggregate) {
				value.Commits.Total = maxRangeCommits + 1
			},
			want: coverageExceeds,
		},
		{
			name: "invalid from revision",
			change: func(value *report.Aggregate) {
				value.From = string([]byte{0xff})
			},
			want: "not valid UTF-8",
		},
		{
			name: "control character in to revision",
			change: func(value *report.Aggregate) {
				value.To = "bad\x00revision"
			},
			want: "control character",
		},
		{
			name: "file totals",
			change: func(value *report.Aggregate) {
				value.Files = []report.FileTotals{{
					Path:   "file.go",
					Totals: report.Totals{Human: 1, Lines: 2},
				}}
			},
			want: "file file.go" + coverageLineTotal,
		},
		{
			name: "empty agent",
			change: func(value *report.Aggregate) {
				value.Agents = []report.AgentTotals{{}}
			},
			want: "agent is empty",
		},
		{
			name: "agent totals",
			change: func(value *report.Aggregate) {
				value.Agents = []report.AgentTotals{{
					Agent:  coverageAgent,
					Totals: report.Totals{Human: 1, Lines: 2},
				}}
			},
			want: "agent " + coverageAgent + coverageLineTotal,
		},
		{
			name: "empty model",
			change: func(value *report.Aggregate) {
				value.Agents = []report.AgentTotals{{
					Agent:  coverageAgent,
					Models: []report.ModelTotals{{}},
				}}
			},
			want: "model is empty",
		},
		{
			name: "model totals",
			change: func(value *report.Aggregate) {
				value.Agents = []report.AgentTotals{{
					Agent: coverageAgent,
					Models: []report.ModelTotals{{
						Model:  coverageModel,
						Totals: report.Totals{Human: 1, Lines: 2},
					}},
				}}
			},
			want: "model " + coverageAgent + "/" + coverageModel + coverageLineTotal,
		},
		{
			name: "author totals",
			change: func(value *report.Aggregate) {
				value.Authors = []report.AuthorTotals{{
					Identity: coverageIdentity,
					Totals:   report.Totals{Human: 1, Lines: 2},
				}}
			},
			want: "author " + coverageIdentity + coverageLineTotal,
		},
		{
			name: "model aggregation overflow",
			change: func(value *report.Aggregate) {
				modelTotals := report.Totals{AI: maxInt, Lines: maxInt}
				value.Agents = []report.AgentTotals{
					{
						Agent:  coverageAgent,
						Totals: modelTotals,
						Models: []report.ModelTotals{{Model: coverageModel, Totals: modelTotals}},
					},
					{
						Agent:  "factory",
						Totals: modelTotals,
						Models: []report.ModelTotals{{Model: coverageModel, Totals: modelTotals}},
					},
				}
			},
			want: "aggregate model",
		},
		{
			name: "missing timestamp",
			change: func(value *report.Aggregate) {
				value.Commit[0].Timestamp = coverageEmpty
			},
			want: "no timestamp",
		},
		{
			name: "commit totals",
			change: func(value *report.Aggregate) {
				value.Commit[0].Totals = report.Totals{Human: 1, Lines: 2}
			},
			want: "commit " + coverageCommit + coverageLineTotal,
		},
		{
			name: "range totals overflow",
			change: func(value *report.Aggregate) {
				value.Totals = report.Totals{Human: maxInt, AI: 1, Lines: maxInt}
			},
			want: "overflows",
		},
		{
			name: "invalid warning",
			change: func(value *report.Aggregate) {
				value.Warnings = []string{"bad\x00warning"}
			},
			want: "control character",
		},
		{
			name: "source size",
			change: func(value *report.Aggregate) {
				value.From = strings.Repeat("x", maxSourceBytes+1)
			},
			want: "content and metadata",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			_, err := buildRangeView(testAggregateChanged(test.change))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("buildRangeView() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBuildRangeViewSortsEqualTimestampsByCommit(t *testing.T) {
	aggregate := validRangeAggregate()
	aggregate.Totals = report.Totals{Human: 2, Lines: 2}
	aggregate.Commits = struct {
		Total     int `json:"total"`
		Annotated int `json:"annotated"`
	}{Total: 2, Annotated: 2}
	aggregate.Commit = []report.CommitTotals{
		{
			Commit:    "bbbb1234",
			Timestamp: coverageTimestamp,
			Totals:    report.Totals{Human: 1, Lines: 1},
		},
		{
			Commit:    coverageCommit,
			Timestamp: coverageTimestamp,
			Totals:    report.Totals{Human: 1, Lines: 1},
		},
	}
	view, err := buildRangeView(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Trend) != 2 || view.Trend[0].Commit != coverageCommit ||
		view.Trend[1].Commit != "bbbb1234" {
		t.Fatalf("trend order = %+v", view.Trend)
	}
}

func TestRangeArithmeticOverflow(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	dst := report.Totals{Human: maxInt, Lines: maxInt}
	err := addRangeTotals(&dst, report.Totals{Human: 1, Lines: 1})
	if err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("addRangeTotals() error = %v", err)
	}
}

func validRangeAggregate() report.Aggregate {
	return report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{Human: 1, Lines: 1},
		Commit: []report.CommitTotals{{
			Commit:    coverageCommit,
			Timestamp: coverageTimestamp,
			Totals:    report.Totals{Human: 1, Lines: 1},
		}},
		Commits: struct {
			Total     int `json:"total"`
			Annotated int `json:"annotated"`
		}{Total: 1, Annotated: 1},
	}
}

func testAggregateChanged(change func(*report.Aggregate)) report.Aggregate {
	value := validRangeAggregate()
	change(&value)
	return value
}

func failingTemplate(t *testing.T, name string) *template.Template {
	t.Helper()
	return template.Must(template.New(name).Funcs(template.FuncMap{
		"fail": func() (string, error) {
			return coverageEmpty, errors.New("template failure")
		},
	}).Parse("{{fail}}"))
}

func oversizedTemplate(t *testing.T, name string) *template.Template {
	t.Helper()
	return template.Must(template.New(name).Funcs(template.FuncMap{
		"large": func() string {
			return strings.Repeat("x", maxHTMLBytes+1)
		},
	}).Parse("{{large}}"))
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
