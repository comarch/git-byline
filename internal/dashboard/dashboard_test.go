package dashboard

import (
	"bytes"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
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
	if view.ModelCount != 1 || len(view.Sources) != 1 || view.Sources[0].Label != "other/unknown" {
		t.Fatalf("sources = %+v, model count = %d", view.Sources, view.ModelCount)
	}
	if view.Files[0].AIPercent != 0 {
		t.Fatalf("empty AI percentage = %.1f", view.Files[0].AIPercent)
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
	if got := aiTone("ai\x00factory\x00model"); got != "tone-orange" {
		t.Fatalf("factory tone = %q", got)
	}
	first := aiTone("ai\x00other\x00model")
	second := aiTone("ai\x00other\x00model")
	if first != second || first == "" {
		t.Fatalf("fallback tones = %q, %q", first, second)
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
