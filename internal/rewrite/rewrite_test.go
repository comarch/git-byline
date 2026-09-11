package rewrite

import (
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/model"
)

const (
	oldCommit = "1111111111111111111111111111111111111111"
	newCommit = "2222222222222222222222222222222222222222"
)

func TestParsePostRewriteAndRemap(t *testing.T) {
	t.Parallel()
	mapping, err := ParsePostRewrite(strings.NewReader(
		oldCommit + " " + newCommit + "\n" +
			oldCommit + " " + oldCommit + "\n",
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := mapping.Remap(oldCommit); !ok || got != oldCommit {
		t.Fatalf("Remap() = %q, %t", got, ok)
	}
	if got := mapping.Targets(oldCommit); len(got) != 2 || got[0] != newCommit {
		t.Fatalf("Targets() = %v", got)
	}
	if got := mapping.Sources(newCommit); len(got) != 1 || got[0] != oldCommit {
		t.Fatalf("Sources() = %v", got)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = oldCommit
	state.Pending.BaseCommit = oldCommit
	state = mapping.RemapState(state)
	if state.LastAnnotatedCommit != oldCommit || state.Pending.BaseCommit != oldCommit {
		t.Fatalf("RemapState() = %+v", state)
	}
}

func TestParsePostRewriteRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	tests := []string{
		"short\n",
		oldCommit + "\n",
		oldCommit + " not-an-object\n",
		"-bad " + newCommit + "\n",
	}
	for _, input := range tests {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePostRewrite(strings.NewReader(input)); err == nil {
				t.Fatal("ParsePostRewrite accepted malformed input")
			}
		})
	}
}

func TestParseReferenceTransaction(t *testing.T) {
	t.Parallel()
	input := strings.NewReader(
		strings.Repeat("0", 40) + " " + newCommit + " HEAD\n" +
			oldCommit + " " + newCommit + " refs/heads/main\n" +
			oldCommit + " " + newCommit + " refs/stash\n",
	)
	updates, err := ParseReferenceTransaction(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 3 || updates[1].Ref != "refs/heads/main" {
		t.Fatalf("updates = %+v", updates)
	}
}

func TestParseReferenceTransactionKeepsUninterestingRefsForFiltering(t *testing.T) {
	t.Parallel()
	updates, err := ParseReferenceTransaction(strings.NewReader(
		oldCommit + " " + newCommit + " refs/tags/v1\n",
	))
	if err != nil || len(updates) != 1 || updates[0].Ref != "refs/tags/v1" {
		t.Fatalf("uninteresting updates = %+v, %v", updates, err)
	}
	for _, input := range []string{
		oldCommit + " " + newCommit + " refs/heads/\n",
		oldCommit + " " + newCommit + " refs/heads/a b\n",
		"bad " + newCommit + " HEAD\n",
	} {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseReferenceTransaction(strings.NewReader(input)); err == nil {
				t.Fatal("ParseReferenceTransaction accepted malformed input")
			}
		})
	}
}

func TestReadLinesBounds(t *testing.T) {
	t.Parallel()
	if _, err := ParsePostRewrite(strings.NewReader(strings.Repeat("x", maxInputBytes+1))); err == nil {
		t.Fatal("ParsePostRewrite accepted oversized input")
	}
	var lines strings.Builder
	for i := 0; i <= maxInputLines; i++ {
		lines.WriteString(oldCommit + " " + newCommit + "\n")
	}
	if _, err := ParsePostRewrite(strings.NewReader(lines.String())); err == nil {
		t.Fatal("ParsePostRewrite accepted too many lines")
	}
}

func TestProjectLayeredWrapper(t *testing.T) {
	t.Parallel()
	source := engine.Snapshot{
		Lines:        []string{"one\n"},
		Attributions: []model.Attribution{{Author: model.AuthorHuman}},
	}
	projected, err := ProjectLayered(
		[]engine.Snapshot{source},
		[]byte("one\n"),
		model.Attribution{Author: model.AuthorUntracked},
	)
	if err != nil || len(projected.Attributions) != 1 ||
		projected.Attributions[0].Author != model.AuthorHuman {
		t.Fatalf("ProjectLayered() = %+v, %v", projected, err)
	}
}
