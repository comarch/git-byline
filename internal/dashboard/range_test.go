package dashboard

import (
	"bytes"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/report"
)

func TestRenderRange(t *testing.T) {
	t.Parallel()
	aggregate := report.Aggregate{
		Version: model.NoteVersion,
		From:    "HEAD~2",
		To:      "HEAD",
		Totals: report.Totals{
			Human:         2,
			AI:            3,
			HumanOverride: 1,
			Untracked:     4,
			Lines:         10,
		},
		Agents: []report.AgentTotals{
			{
				Agent:  "droid",
				Totals: report.Totals{AI: 2, HumanOverride: 1, Lines: 3},
				Models: []report.ModelTotals{
					{Model: "model-b", Totals: report.Totals{HumanOverride: 1, Lines: 1}},
					{Model: "model-a", Totals: report.Totals{AI: 2, Lines: 2}},
				},
			},
			{
				Agent:  "claude",
				Totals: report.Totals{AI: 1, Lines: 1},
				Models: []report.ModelTotals{
					{Model: "model-a", Totals: report.Totals{AI: 1, Lines: 1}},
				},
			},
		},
		Files: []report.FileTotals{
			{Path: "z.go", Totals: report.Totals{AI: 1, Untracked: 3, Lines: 4}},
			{Path: "a.go", Totals: report.Totals{Human: 2, AI: 2, HumanOverride: 1, Untracked: 1, Lines: 6}},
		},
		Commit: []report.CommitTotals{
			{
				Commit:    "bbbbbbbb",
				Timestamp: "2026-01-02T03:04:05Z",
				Totals:    report.Totals{Human: 1, AI: 1, Lines: 2},
			},
			{
				Commit:    "aaaaaaaa",
				Timestamp: "2026-01-01T03:04:05Z",
				Totals:    report.Totals{Human: 1, AI: 2, HumanOverride: 1, Untracked: 4, Lines: 8},
			},
		},
		Warnings: []string{"warning <tag>"},
	}
	first, err := RenderRange(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := RenderRange(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("RenderRange is not deterministic")
	}
	text := string(first)
	for _, expected := range []string{
		"git-byline attribution dashboard",
		"range HEAD~2..HEAD",
		"Commit trend",
		"2026-01-01T03:04:05Z",
		"Author classes",
		"human-override",
		"a.go",
		"claude",
		"model-a",
		"warning &lt;tag&gt;",
		"Content-Security-Policy",
		"connect-src 'none'",
		"No source lines are rendered in range mode.",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("range HTML missing %q", expected)
		}
	}
	for _, forbidden := range []string{"https://", "http://", "<tag>"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("range HTML contains unsafe value %q", forbidden)
		}
	}
	if strings.Index(text, "2026-01-01T03:04:05Z") > strings.Index(text, "2026-01-02T03:04:05Z") {
		t.Fatal("trend is not ordered by timestamp")
	}
}

func TestBuildRangeViewAggregatesModelsAndAuthors(t *testing.T) {
	t.Parallel()
	aggregate := report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{AI: 3, Lines: 3},
		Agents: []report.AgentTotals{
			{
				Agent:  "z-agent",
				Totals: report.Totals{AI: 2, Lines: 2},
				Models: []report.ModelTotals{{Model: "model", Totals: report.Totals{AI: 2, Lines: 2}}},
			},
			{
				Agent:  "a-agent",
				Totals: report.Totals{AI: 1, Lines: 1},
				Models: []report.ModelTotals{{Model: "model", Totals: report.Totals{AI: 1, Lines: 1}}},
			},
		},
	}
	view, err := buildRangeView(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Authors) != 4 || view.Authors[0].Name != "human" ||
		view.Authors[1].Name != "human-override" || view.Authors[2].Name != "ai" {
		t.Fatalf("authors = %+v", view.Authors)
	}
	if len(view.Agents) != 2 || view.Agents[0].Name != "a-agent" ||
		view.Agents[1].Name != "z-agent" {
		t.Fatalf("agents = %+v", view.Agents)
	}
	if len(view.Models) != 1 || view.Models[0].Name != "model" || view.Models[0].Lines != 3 {
		t.Fatalf("models = %+v", view.Models)
	}
}

func TestBuildRangeViewValidation(t *testing.T) {
	t.Parallel()
	valid := report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{Human: 1, Lines: 1},
		Commit: []report.CommitTotals{{
			Commit:    "abcd1234",
			Timestamp: "2026-01-01T00:00:00Z",
			Totals:    report.Totals{Human: 1, Lines: 1},
		}},
		Commits: struct {
			Total     int `json:"total"`
			Annotated int `json:"annotated"`
		}{Total: 1, Annotated: 1},
	}
	tests := []struct {
		name   string
		change func(*report.Aggregate)
	}{
		{name: "version", change: func(value *report.Aggregate) { value.Version = 1 }},
		{name: "commit count", change: func(value *report.Aggregate) { value.Commits.Annotated = 2 }},
		{name: "totals", change: func(value *report.Aggregate) { value.Totals.Lines = 2 }},
		{name: "commit ID", change: func(value *report.Aggregate) { value.Commit[0].Commit = "bad" }},
		{name: "timestamp", change: func(value *report.Aggregate) {
			value.Commit[0].Timestamp = "not-a-timestamp"
		}},
		{name: "control path", change: func(value *report.Aggregate) {
			value.Files = []report.FileTotals{{Path: "bad\npath", Totals: report.Totals{Human: 1, Lines: 1}}}
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.Commit = append([]report.CommitTotals(nil), valid.Commit...)
			test.change(&value)
			if _, err := RenderRange(value); err == nil {
				t.Fatal("RenderRange accepted invalid aggregate")
			}
		})
	}
}
