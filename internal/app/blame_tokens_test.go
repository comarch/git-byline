package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
)

func TestWriteBlameTextTokens(t *testing.T) {
	t.Parallel()
	lines := []provenance.BlameLine{
		{Number: 1, Content: "one", Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: "droid", Model: "m", Session: "s1",
		}},
		{Number: 2, Content: "two", Attribution: model.Attribution{
			Author: model.AuthorHuman, Identity: "john.doe",
		}},
		{Number: 3, Content: "three", Attribution: model.Attribution{Author: model.AuthorUntracked}},
	}
	sessions := map[string]model.NoteSession{
		model.NoteSessionKey("droid", "s1"): {
			Agent:      "droid",
			TokensIn:   120,
			TokensOut:  34,
			CacheRead:  8,
			CacheWrite: 2,
		},
	}
	var out bytes.Buffer
	writeBlameText(&out, lines, sessions, false, true)
	rendered := out.String()
	if !strings.Contains(rendered, "ai:droid/m [120i/34o/8cr/2cw]") {
		t.Fatalf("tokens suffix missing: %q", rendered)
	}
	if strings.Count(rendered, "[") != 1 {
		t.Fatalf("suffix leaked beyond the attributed line: %q", rendered)
	}

	var off bytes.Buffer
	writeBlameText(&off, lines, sessions, false, false)
	if strings.Contains(off.String(), "[120i") {
		t.Fatalf("suffix without --tokens: %q", off.String())
	}

	var noUsage bytes.Buffer
	writeBlameText(&noUsage, lines, nil, false, true)
	if strings.Contains(noUsage.String(), "[") {
		t.Fatalf("suffix without sessions: %q", noUsage.String())
	}
}

func TestTokenSuffixCacheOnlyWhenNonzero(t *testing.T) {
	t.Parallel()
	suffix := tokenSuffix(
		map[string]model.NoteSession{
			model.NoteSessionKey("droid", "s1"): {Agent: "droid", TokensIn: 5, TokensOut: 6},
		},
		model.Attribution{Author: model.AuthorAI, Agent: "droid", Session: "s1"},
	)
	if suffix != " [5i/6o]" {
		t.Fatalf("tokenSuffix = %q, want [5i/6o]", suffix)
	}

	empty := model.Attribution{Author: model.AuthorHuman, Identity: "john.doe"}
	if got := tokenSuffix(map[string]model.NoteSession{}, empty); got != "" {
		t.Fatalf("tokenSuffix(human) = %q, want empty", got)
	}
}
