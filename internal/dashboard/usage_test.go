package dashboard

import (
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/report"
)

func TestRenderSessionUsage(t *testing.T) {
	t.Parallel()
	data, err := Render(Report{
		Commit: "abcdef123456",
		Status: provenance.StatusResult{
			Head:                "abcdef123456",
			LastAnnotatedCommit: "abcdef123456",
		},
		Files: []provenance.BlameResult{
			{
				Version: model.NoteVersion,
				File:    "a.go",
				Blob:    "aaaa1234",
				Commit:  "abcdef123456",
				Lines: []provenance.BlameLine{
					{
						Number:  1,
						Content: "one",
						Attribution: model.Attribution{
							Author: model.AuthorAI, Agent: "droid", Model: "model-a", Session: "session-a",
						},
					},
				},
			},
		},
		Sessions: map[string]model.NoteSession{
			model.NoteSessionKey("droid", "session-a"): {
				Agent: "droid", Model: "model-a",
				TokensIn: 120, TokensOut: 34, CacheRead: 56, CacheWrite: 78,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		"Session token usage",
		"droid::session-a",
		"model-a",
		"120",
		"34",
		"56",
		"78",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("dashboard missing %q", want)
		}
	}
}

func TestRenderRangeSessionUsage(t *testing.T) {
	t.Parallel()
	data, err := RenderRange(report.Aggregate{
		Version: model.NoteVersion,
		From:    "", To: "",
		Totals: report.Totals{Lines: 1, AI: 1},
		Sessions: []report.SessionTotals{
			{
				Session: "session-a", Agent: "droid", Model: "model-a",
				Totals:     report.Totals{Lines: 1, AI: 1},
				TokensIn:   120,
				TokensOut:  34,
				CacheRead:  56,
				CacheWrite: 78,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)
	for _, want := range []string{
		"Sessions",
		"session-a",
		"droid",
		"model-a",
		"120",
		"78",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("range dashboard missing %q", want)
		}
	}
}

func TestRenderWithoutSessionUsageOmitsSection(t *testing.T) {
	t.Parallel()
	data, err := Render(Report{
		Commit: "abcdef123456",
		Status: provenance.StatusResult{
			Head:                "abcdef123456",
			LastAnnotatedCommit: "abcdef123456",
		},
		Files: []provenance.BlameResult{
			{
				Version: model.NoteVersion,
				File:    "a.go",
				Blob:    "aaaa1234",
				Commit:  "abcdef123456",
				Lines: []provenance.BlameLine{
					{Number: 1, Content: "one", Attribution: model.Attribution{Author: model.AuthorHuman}},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Session token usage") {
		t.Fatal("dashboard rendered a usage section without usage data")
	}
}

func TestRenderRangeWithoutSessionUsageOmitsSection(t *testing.T) {
	t.Parallel()
	data, err := RenderRange(report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{Lines: 1, AI: 1},
		Sessions: []report.SessionTotals{{
			Session: "session-a",
			Agent:   "droid",
			Model:   "model-a",
			Totals:  report.Totals{Lines: 1, AI: 1},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<h2>Sessions</h2>") {
		t.Fatal("range dashboard rendered a session section without usage data")
	}
}

func TestSessionUsageViewsFiltersZeroAndDefaultsModel(t *testing.T) {
	t.Parallel()
	views := sessionUsageViews(map[string]model.NoteSession{
		"droid::zero": {Agent: "droid"},
		"droid::used": {Agent: "droid", TokensIn: 1},
	})
	if len(views) != 1 || views[0].Key != "droid::used" ||
		views[0].Model != "unknown" {
		t.Fatalf("sessionUsageViews() = %+v", views)
	}
}
