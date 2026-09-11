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
	if !strings.Contains(text, "No human lines in this range.") {
		t.Error("range HTML does not report an empty people table")
	}
}

func TestRenderRangeListsPeople(t *testing.T) {
	t.Parallel()
	aggregate := report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{Human: 3, HumanOverride: 1, Lines: 4},
		Authors: []report.AuthorTotals{
			{Identity: "john.doe", Totals: report.Totals{Human: 2, HumanOverride: 1, Lines: 3}},
			{Identity: "", Totals: report.Totals{Human: 1, Lines: 1}},
		},
	}
	data, err := RenderRange(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{"People", "john.doe", "(unidentified)"} {
		if !strings.Contains(text, expected) {
			t.Errorf("range HTML missing %q", expected)
		}
	}
	view, err := buildRangeView(aggregate)
	if err != nil {
		t.Fatal(err)
	}
	if view.PeopleCount != 2 || view.People[0].Name != "(unidentified)" ||
		view.People[1].Name != "john.doe" || view.People[1].Lines != 3 {
		t.Fatalf("people = %+v", view.People)
	}
	aggregate.Authors = []report.AuthorTotals{{Identity: "John Doe", Totals: report.Totals{Human: 4, Lines: 4}}}
	if _, err := buildRangeView(aggregate); err == nil ||
		!strings.Contains(err.Error(), "author") {
		t.Fatalf("buildRangeView(invalid identity) error = %v", err)
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
	if len(view.Classes) != 4 || view.Classes[0].Name != "human" ||
		view.Classes[1].Name != "human-override" || view.Classes[2].Name != "ai" {
		t.Fatalf("classes = %+v", view.Classes)
	}
	if len(view.People) != 0 || view.PeopleCount != 0 {
		t.Fatalf("people = %+v", view.People)
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

func TestRangeTotalsArithmeticAndDisplay(t *testing.T) {
	t.Parallel()
	maxInt := int(^uint(0) >> 1)
	if _, err := checkedRangeAdd(-1, 1); err == nil {
		t.Error("checkedRangeAdd accepted a negative value")
	}
	if _, err := checkedRangeAdd(maxInt, 1); err == nil {
		t.Error("checkedRangeAdd accepted an overflow")
	}
	if got, err := checkedRangeAdd(2, 3); got != 5 || err != nil {
		t.Fatalf("checkedRangeAdd(2, 3) = %d, %v", got, err)
	}

	var totals report.Totals
	if err := addRangeTotals(&totals, report.Totals{Human: 1, AI: 2, Lines: 3}); err != nil {
		t.Fatal(err)
	}
	if err := addRangeTotals(&totals, report.Totals{Untracked: 1, Lines: 1}); err != nil {
		t.Fatal(err)
	}
	if totals.Lines != 4 || totals.Human != 1 || totals.AI != 2 || totals.Untracked != 1 {
		t.Fatalf("totals = %+v", totals)
	}
	if err := addRangeTotals(&totals, report.Totals{Human: 1, Lines: 2}); err == nil {
		t.Error("addRangeTotals accepted inconsistent totals")
	}
	if err := validateRangeTotals("negative", report.Totals{Human: -1, Lines: -1}); err == nil {
		t.Error("validateRangeTotals accepted negative lines")
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
		if got := displayRange(test.from, test.to); got != test.want {
			t.Fatalf("displayRange(%q, %q) = %q, want %q", test.from, test.to, got, test.want)
		}
	}
	if got := rangeIdentityName(""); got != "(unidentified)" {
		t.Fatalf("rangeIdentityName(empty) = %q", got)
	}
	if got := rangeIdentityName("john.doe"); got != "john.doe" {
		t.Fatalf("rangeIdentityName() = %q", got)
	}
	if err := validateRangeText("warning", "", true); err != nil {
		t.Fatalf("validateRangeText(empty allowed) = %v", err)
	}
	if err := validateRangeText("path", "", false); err == nil {
		t.Error("validateRangeText accepted an empty required value")
	}
	if err := validateRangeText("path", "one\x00two", false); err == nil {
		t.Error("validateRangeText accepted a control character")
	}
}
