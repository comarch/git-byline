package disclosure

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/report"
)

func TestRenderMatchesGoldens(t *testing.T) {
	t.Parallel()
	generatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	aggregate := disclosureFixture()
	for _, format := range []string{"json", "cyclonedx", "spdx"} {
		format := format
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			got, err := Render(format, aggregate, generatedAt, "v0.1.0-test")
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("testdata", format+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s output differs from golden:\n%s", format, got)
			}
		})
	}
}

func TestRenderEscapesReportStringsAndDefinesAIShare(t *testing.T) {
	t.Parallel()
	aggregate := disclosureFixture()
	aggregate.Agents[0].Agent = `agent"quoted<>`
	aggregate.Files[0].Path = `src/"quoted.go`
	aggregate.Warnings = []string{`warning "quoted"`}
	data, err := Render("json", aggregate, time.Time{}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"agent":"agent"quoted`)) ||
		bytes.Contains(data, []byte(`"path":"src/"quoted`)) {
		t.Fatalf("report strings were not JSON escaped: %s", data)
	}
	var document struct {
		AIShareDefinition string `json:"ai_share_definition"`
		Totals            struct {
			AIShare float64 `json:"ai_share_percent"`
		} `json:"totals"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.AIShareDefinition != AIShareDefinition {
		t.Fatalf("AI share definition = %q", document.AIShareDefinition)
	}
	if document.Totals.AIShare != 62.5 {
		t.Fatalf("AI share = %v, want 62.5", document.Totals.AIShare)
	}
}

func TestRenderRejectsUnsupportedFormatAndNegativeTotals(t *testing.T) {
	t.Parallel()
	if _, err := Render("unknown", report.Aggregate{}, time.Time{}, "dev"); err == nil ||
		!strings.Contains(err.Error(), "unsupported disclosure format") {
		t.Fatalf("unknown format error = %v", err)
	}
	aggregate := disclosureFixture()
	aggregate.Totals.AI = -1
	if _, err := Render("json", aggregate, time.Time{}, "dev"); err == nil ||
		!strings.Contains(err.Error(), "negative totals") {
		t.Fatalf("negative total error = %v", err)
	}
}

func TestRenderRejectsInvalidNestedTotals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		mutate   func(*report.Aggregate)
		wantName string
	}{
		{
			name: "aggregate negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Totals.AI = -1
			},
			wantName: "aggregate",
		},
		{
			name: "aggregate mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Totals.Lines++
			},
			wantName: "aggregate",
		},
		{
			name: "agent negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Agents[0].Totals.AI = -1
			},
			wantName: `agent "droid"`,
		},
		{
			name: "agent mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Agents[0].Totals.Lines++
			},
			wantName: `agent "droid"`,
		},
		{
			name: "model negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Agents[0].Models[0].Totals.AI = -1
			},
			wantName: `agent "droid" model "model-a"`,
		},
		{
			name: "model mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Agents[0].Models[0].Totals.Lines++
			},
			wantName: `agent "droid" model "model-a"`,
		},
		{
			name: "file negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Files[0].Totals.Human = -1
			},
			wantName: `file "file.go"`,
		},
		{
			name: "file mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Files[0].Totals.Lines++
			},
			wantName: `file "file.go"`,
		},
		{
			name: "session negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Sessions[0].Totals.AI = -1
			},
			wantName: `session "session-a"`,
		},
		{
			name: "session mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Sessions[0].Totals.Lines++
			},
			wantName: `session "session-a"`,
		},
		{
			name: "commit negative",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Commit[0].Totals.AI = -1
			},
			wantName: `commit "bbbb"`,
		},
		{
			name: "commit mismatch",
			mutate: func(aggregate *report.Aggregate) {
				aggregate.Commit[0].Totals.Lines++
			},
			wantName: `commit "bbbb"`,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			aggregate := disclosureFixture()
			tt.mutate(&aggregate)
			if _, err := Render("json", aggregate, time.Time{}, "dev"); err == nil ||
				!strings.Contains(err.Error(), tt.wantName) {
				t.Fatalf("Render() error = %v, want entry %q", err, tt.wantName)
			}
		})
	}
	if _, err := Render("json", disclosureFixture(), time.Time{}, "dev"); err != nil {
		t.Fatalf("valid aggregate rejected: %v", err)
	}
}

func disclosureFixture() report.Aggregate {
	return report.Aggregate{
		Version: 2,
		From:    "base",
		To:      "head",
		Commits: struct {
			Total     int `json:"total"`
			Annotated int `json:"annotated"`
		}{Total: 2, Annotated: 2},
		Totals: report.Totals{
			Human:         2,
			AI:            3,
			Untracked:     1,
			HumanOverride: 2,
			Lines:         8,
		},
		Agents: []report.AgentTotals{
			{
				Agent: "droid",
				Totals: report.Totals{
					AI:            3,
					HumanOverride: 2,
					Lines:         5,
				},
				Models: []report.ModelTotals{{
					Model: "model-a",
					Totals: report.Totals{
						AI:            3,
						HumanOverride: 2,
						Lines:         5,
					},
				}},
			},
		},
		Files: []report.FileTotals{
			{
				Path: "file.go",
				Totals: report.Totals{
					Human:         2,
					AI:            3,
					Untracked:     1,
					HumanOverride: 2,
					Lines:         8,
				},
			},
		},
		Sessions: []report.SessionTotals{{
			Session: "session-a",
			Agent:   "droid",
			Model:   "model-a",
			Totals: report.Totals{
				AI:            3,
				HumanOverride: 2,
				Lines:         5,
			},
		}},
		Commit: []report.CommitTotals{
			{
				Commit:    "bbbb",
				Timestamp: "2026-01-02T03:04:05Z",
				Totals:    report.Totals{AI: 2, Lines: 2},
			},
			{
				Commit:    "aaaa",
				Timestamp: "2026-01-01T03:04:05Z",
				Totals:    report.Totals{Human: 2, Untracked: 1, HumanOverride: 2, AI: 1, Lines: 6},
			},
		},
		Warnings: []string{"warning \"quoted\""},
	}
}
