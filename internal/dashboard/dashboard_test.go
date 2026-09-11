package dashboard

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/report"
)

func TestRender(t *testing.T) {
	t.Parallel()
	report := Report{
		Commit: "abcdef123456",
		Status: provenance.StatusResult{
			Head:                "abcdef123456",
			LastAnnotatedCommit: "abcdef123456",
			Warnings:            []string{"status <warning>"},
		},
		Files: []provenance.BlameResult{
			{
				Version: model.NoteVersion,
				File:    "b.go",
				Blob:    "bbbb1234",
				Commit:  "abcdef123456",
				Lines: []provenance.BlameLine{
					{Number: 1, Content: "human", Attribution: model.Attribution{Author: model.AuthorHuman}},
				},
			},
			{
				Version: model.NoteVersion,
				File:    `a<script>.go`,
				Blob:    "aaaa1234",
				Commit:  "abcdef123456",
				Lines: []provenance.BlameLine{
					{
						Number:  1,
						Content: `<script>alert("x")</script>`,
						Attribution: model.Attribution{
							Author: model.AuthorAI,
							Agent:  `agent"></td><script>`,
							Model:  "model",
						},
					},
					{Number: 2, Content: "unknown", Attribution: model.Attribution{Author: model.AuthorUntracked}},
				},
			},
		},
	}
	first, err := Render(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("Render is not deterministic")
	}
	text := string(first)
	for _, expected := range []string{
		"git-byline attribution dashboard",
		"33.3%",
		`a&lt;script&gt;.go`,
		`&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;`,
		`agent&#34;&gt;&lt;/td&gt;&lt;script&gt;/model`,
		"tone-human",
		`id="file-0"`,
		`id="file-1"`,
		"Content-Security-Policy",
		"connect-src 'none'",
		"status &lt;warning&gt;",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("HTML missing %q", expected)
		}
	}
	for _, forbidden := range []string{
		`<script>alert("x")</script>`,
		`agent"></td><script>`,
		"https://",
		"http://",
	} {
		if strings.Contains(text, forbidden) {
			t.Errorf("HTML contains unsafe value %q", forbidden)
		}
	}
	if strings.Index(text, `a&lt;script&gt;.go`) > strings.Index(text, "b.go") {
		t.Fatal("files are not sorted")
	}
}

// TestRenderKeepsLongLabelsInsideCards pins the two layout guards a long
// agent label needs: a shrinkable grid track and shrinkable flex items.
// Without them the label column overflowed the summary card and painted
// over the source lines.
func TestRenderKeepsLongLabelsInsideCards(t *testing.T) {
	t.Parallel()
	data, err := Render(Report{
		Commit: "abcdef123456",
		Status: provenance.StatusResult{Head: "abcdef123456", LastAnnotatedCommit: "abcdef123456"},
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion,
			File:    "a.go",
			Blob:    "aaaa1234",
			Commit:  "abcdef123456",
			Lines: []provenance.BlameLine{{
				Number: 1, Content: "one",
				Attribution: model.Attribution{
					Author:   model.AuthorHumanOverride,
					Identity: "john.doe",
					Agent:    "droid",
					Model:    "a-very-long-model-name-that-overflows",
				},
			}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, expected := range []string{
		".source-list { display: grid; grid-template-columns: minmax(0, 1fr);",
		".legend { display: grid; grid-template-columns: minmax(0, 1fr);",
		".legend-row span, .source-head span, .health-row span { min-width: 0;",
		"human-override:john.doe/droid/a-very-long-model-name-that-overflows",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("HTML missing layout guard %q", expected)
		}
	}
	// Writing the fragment during the initial render scrolled the report
	// past its summary, so only an explicit file choice may link.
	if strings.Contains(text, `show(location.hash.slice(1), true)`) {
		t.Error("initial file selection still rewrites the location hash")
	}
	if !strings.Contains(text, `show(location.hash.slice(1), false)`) {
		t.Error("initial file selection does not opt out of hash rewriting")
	}
}

func TestUnknownModelGroupingAndEmptyFile(t *testing.T) {
	t.Parallel()
	report := Report{
		Commit: "abcd1234",
		Status: provenance.StatusResult{Head: "abcd1234"},
		Files: []provenance.BlameResult{
			{
				Version: model.NoteVersion, File: "empty", Blob: "aaaa", Commit: "abcd1234",
			},
			{
				Version: model.NoteVersion, File: "source", Blob: "bbbb", Commit: "abcd1234",
				Lines: []provenance.BlameLine{
					{Number: 1, Attribution: model.Attribution{Author: model.AuthorAI, Agent: "other"}},
					{Number: 2, Attribution: model.Attribution{Author: model.AuthorAI, Agent: "other", Model: "unknown"}},
				},
			},
		},
	}
	view, err := buildView(report)
	if err != nil {
		t.Fatal(err)
	}
	if view.ModelCount != 1 || len(view.Sources) != 1 || view.Sources[0].Label != "ai:other/unknown" {
		t.Fatalf("sources = %+v, model count = %d", view.Sources, view.ModelCount)
	}
	if view.Files[0].AIPercent != 0 {
		t.Fatalf("empty AI percentage = %.1f", view.Files[0].AIPercent)
	}
}

func TestNonAIRemainderCountsAsUntracked(t *testing.T) {
	t.Parallel()
	view, err := buildView(Report{
		Commit: "abcd1234",
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion,
			File:    "source",
			Blob:    "beef1234",
			Commit:  "abcd1234",
			Lines: []provenance.BlameLine{
				{Number: 1, Attribution: model.Attribution{Author: model.AuthorAI, Agent: "droid"}},
				{Number: 2, Attribution: model.Attribution{}},
				{Number: 3, Attribution: model.Attribution{Author: model.AuthorHuman}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.AILines != 1 || view.HumanLines != 1 || view.UntrackedLines != 1 {
		t.Fatalf("totals = AI %d, human %d, untracked %d", view.AILines, view.HumanLines, view.UntrackedLines)
	}
	if view.Files[0].UntrackedLines != 1 || view.Files[0].HumanLines != 1 {
		t.Fatalf("file totals = human %d, untracked %d", view.Files[0].HumanLines, view.Files[0].UntrackedLines)
	}
}

func TestHumanOverrideIsFourthDashboardClass(t *testing.T) {
	t.Parallel()
	report := Report{
		Commit: "abcd1234",
		Status: provenance.StatusResult{Head: "abcd1234"},
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion,
			File:    "source",
			Blob:    "beef1234",
			Commit:  "abcd1234",
			Lines: []provenance.BlameLine{
				{Number: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
				{Number: 2, Attribution: model.Attribution{
					Author: model.AuthorHumanOverride, Agent: "droid", Model: "model",
				}},
				{Number: 3, Attribution: model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "model"}},
				{Number: 4, Attribution: model.Attribution{Author: model.AuthorUntracked}},
			},
		}},
	}
	view, err := buildView(report)
	if err != nil {
		t.Fatal(err)
	}
	if view.HumanOverrideLines != 1 || view.Files[0].HumanOverrideLines != 1 {
		t.Fatalf("human override totals = %d/%d", view.HumanOverrideLines, view.Files[0].HumanOverrideLines)
	}
	found := false
	for _, source := range view.Sources {
		if source.Kind == string(model.AuthorHumanOverride) {
			found = source.Label == "human-override:droid/model" && source.Tone == "tone-human-override"
		}
	}
	if !found {
		t.Fatalf("sources = %+v", view.Sources)
	}
	data, err := Render(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("human override")) ||
		!bytes.Contains(data, []byte("human-override:droid/model")) {
		t.Fatalf("rendered dashboard omits human override")
	}
}

func TestRenderEmptyCommitReport(t *testing.T) {
	t.Parallel()
	report := Report{
		Commit: "abcd1234",
		Status: provenance.StatusResult{
			Head:                "abcd1234",
			LastAnnotatedCommit: "abcd1234",
		},
	}
	data, err := Render(report)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(">0</strong>")) || !bytes.Contains(data, []byte("0 attributed files")) {
		t.Fatalf("empty report missing zero state")
	}
}

func TestAIToneIsStable(t *testing.T) {
	t.Parallel()
	if got := aiTone("ai\x00factory\x00model"); got != "tone-cyan" {
		t.Fatalf("factory tone = %q", got)
	}
	first := aiTone("ai\x00other\x00model")
	second := aiTone("ai\x00other\x00model")
	if first != second || first == "" {
		t.Fatalf("fallback tones = %q, %q", first, second)
	}
}

// TestToneClassesStayInsideThePalette pins every rendered tone to a
// declared token. The scale has no green, orange, or yellow, so a stray
// accent color is a regression, not a nit.
func TestToneClassesStayInsideThePalette(t *testing.T) {
	t.Parallel()
	data, err := Render(Report{
		Commit: "abcd1234",
		Status: provenance.StatusResult{Head: "abcd1234", LastAnnotatedCommit: "abcd1234"},
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion, File: "a.go", Blob: "aaaa1234", Commit: "abcd1234",
			Lines: []provenance.BlameLine{{Number: 1, Content: "a", Attribution: model.Attribution{
				Author: model.AuthorHuman, Identity: "john.doe",
			}}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, class := range append([]string{
		"tone-human", "tone-human-override", "tone-untracked", "tone-ai",
	}, palette...) {
		if !strings.Contains(text, "."+class+" { color: var(--") {
			t.Errorf("tone class %q does not resolve to a palette token", class)
		}
	}
	for _, declared := range []string{
		"#00FFFF", "#6400BE", "#FF0000", "#FF009B", "#1A1A1A", "#000000",
	} {
		if !strings.Contains(text, declared) {
			t.Errorf("palette color %q is missing", declared)
		}
	}
	for _, foreign := range []string{
		"#f97316", "#22c55e", "#facc15", "#60a5fa", "#f59e0b", "#c084fc",
	} {
		if strings.Contains(text, foreign) {
			t.Errorf("color %q is outside the palette", foreign)
		}
	}
	if !strings.Contains(text, `"Cera Pro", Inter, Arial, sans-serif`) {
		t.Error("type stack is missing")
	}
	// The gradient keeps its stop order and even distribution.
	if !strings.Contains(text, "linear-gradient(90deg, #00FFFF 0%, #6400BE 50%, #FF0000 100%)") {
		t.Error("gradient does not match the declared stops")
	}
	// Accessibility guardrails.
	for _, guard := range []string{"min-height: 44px", ":focus-visible", "prefers-reduced-motion"} {
		if !strings.Contains(text, guard) {
			t.Errorf("accessibility guardrail %q is missing", guard)
		}
	}
}

func TestRenderValidation(t *testing.T) {
	t.Parallel()
	valid := Report{
		Commit: "abcd1234",
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion, File: "a", Blob: "beef1234", Commit: "abcd1234",
			Lines: []provenance.BlameLine{{Number: 1, Content: "a", Attribution: model.Attribution{Author: model.AuthorHuman}}},
		}},
	}
	tests := []struct {
		name   string
		change func(*Report)
	}{
		{"invalid commit", func(value *Report) { value.Commit = "bad" }},
		{"too many files", func(value *Report) {
			value.Files = make([]provenance.BlameResult, maxFiles+1)
		}},
		{"mismatched commit", func(value *Report) { value.Files[0].Commit = "deadbeef" }},
		{"invalid version", func(value *Report) { value.Files[0].Version = 9 }},
		{"empty path", func(value *Report) { value.Files[0].File = "" }},
		{"invalid blob", func(value *Report) { value.Files[0].Blob = "bad" }},
		{"wrong line number", func(value *Report) { value.Files[0].Lines[0].Number = 2 }},
		{"invalid attribution", func(value *Report) {
			value.Files[0].Lines[0].Attribution = model.Attribution{Author: model.AuthorAI}
		}},
		{"status mismatch", func(value *Report) { value.Status.Head = "deadbeef" }},
		{"too many lines", func(value *Report) {
			value.Files[0].Lines = make([]provenance.BlameLine, maxLines+1)
			for index := range value.Files[0].Lines {
				value.Files[0].Lines[index] = provenance.BlameLine{
					Number:      index + 1,
					Attribution: model.Attribution{Author: model.AuthorHuman},
				}
			}
		}},
		{"too much source", func(value *Report) {
			value.Files[0].Lines[0].Content = strings.Repeat("x", maxSourceBytes+1)
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value := valid
			value.Files = append([]provenance.BlameResult(nil), valid.Files...)
			value.Files[0].Lines = append([]provenance.BlameLine(nil), valid.Files[0].Lines...)
			test.change(&value)
			if _, err := Render(value); err == nil {
				t.Fatal("Render accepted invalid report")
			}
		})
	}
}

// TestEveryToneClassIsDefined catches a template that references a tone
// class the stylesheet no longer declares, which silently renders the
// inherited color instead of the palette color.
func TestEveryToneClassIsDefined(t *testing.T) {
	t.Parallel()
	renders := map[string][]byte{}
	commit, err := Render(Report{
		Commit: "abcd1234",
		Status: provenance.StatusResult{Head: "abcd1234", LastAnnotatedCommit: "abcd1234"},
		Files: []provenance.BlameResult{{
			Version: model.NoteVersion, File: "a.go", Blob: "aaaa1234", Commit: "abcd1234",
			Lines: []provenance.BlameLine{
				{Number: 1, Content: "a", Attribution: model.Attribution{
					Author: model.AuthorHuman, Identity: "john.doe",
				}},
				{Number: 2, Content: "b", Attribution: model.Attribution{
					Author: model.AuthorAI, Agent: "droid", Model: "model",
				}},
				{Number: 3, Content: "c", Attribution: model.Attribution{Author: model.AuthorUntracked}},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	renders["commit"] = commit
	rangeReport, err := RenderRange(report.Aggregate{
		Version: model.NoteVersion,
		Totals:  report.Totals{Human: 1, AI: 1, HumanOverride: 1, Untracked: 1, Lines: 4},
		Authors: []report.AuthorTotals{{Identity: "john.doe", Totals: report.Totals{Human: 1, Lines: 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	renders["range"] = rangeReport

	used := regexp.MustCompile(`class="[^"]*?(tone-[a-z-]+)`)
	declared := regexp.MustCompile(`\.(tone-[a-z-]+) \{`)
	for name, data := range renders {
		text := string(data)
		defined := map[string]bool{}
		for _, match := range declared.FindAllStringSubmatch(text, -1) {
			defined[match[1]] = true
		}
		for _, match := range used.FindAllStringSubmatch(text, -1) {
			if !defined[match[1]] {
				t.Errorf("%s report uses undeclared tone class %q", name, match[1])
			}
		}
	}
}
