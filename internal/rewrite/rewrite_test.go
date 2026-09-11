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

func TestHookParsersRequireFullNonZeroObjectIDs(t *testing.T) {
	t.Parallel()
	zero := strings.Repeat("0", 40)
	if _, err := ParsePostRewrite(strings.NewReader(zero + " " + newCommit + "\n")); err == nil {
		t.Fatal("ParsePostRewrite accepted an all-zero old ID")
	}
	if mapping, err := ParsePostRewrite(strings.NewReader(oldCommit + " " + zero + "\n")); err != nil {
		t.Fatal(err)
	} else if len(mapping.Pairs) != 1 || mapping.Pairs[0].New != zero {
		t.Fatalf("drop mapping = %+v", mapping)
	}
	short := oldCommit[:39] + " " + newCommit + "\n"
	if _, err := ParsePostRewrite(strings.NewReader(short)); err == nil {
		t.Fatal("ParsePostRewrite accepted a short ID")
	}
	sha256Old := strings.Repeat("a", 64)
	sha256New := strings.Repeat("b", 64)
	if mapping, err := ParsePostRewriteForLength(
		strings.NewReader(sha256Old+" "+sha256New+"\n"),
		64,
	); err != nil || len(mapping.Pairs) != 1 {
		t.Fatalf("SHA-256 mapping = %+v, %v", mapping, err)
	}
}

func TestNewMappingForLengthValidatesSHA256IDs(t *testing.T) {
	t.Parallel()
	oldID := strings.Repeat("a", 64)
	newID := strings.Repeat("b", 64)
	mapping, err := NewMappingForLength([]Pair{{Old: oldID, New: newID}}, 64)
	if err != nil || len(mapping.Pairs) != 1 {
		t.Fatalf("NewMappingForLength() = %+v, %v", mapping, err)
	}
	if _, err := NewMappingForLength([]Pair{{Old: oldID, New: newID}}, 40); err == nil {
		t.Fatal("NewMappingForLength accepted SHA-256 IDs for SHA-1")
	}
}

func TestMappingDeduplicatesPairsAndDoesNotStoreDropState(t *testing.T) {
	t.Parallel()
	mapping, err := NewMapping([]Pair{
		{Old: oldCommit, New: newCommit},
		{Old: oldCommit, New: newCommit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping.Pairs) != 1 {
		t.Fatalf("deduplicated pairs = %+v", mapping.Pairs)
	}
	state := model.NewState()
	state.LastAnnotatedCommit = oldCommit
	state.Pending.BaseCommit = oldCommit
	drop, err := NewMapping([]Pair{{Old: oldCommit, New: strings.Repeat("0", 40)}})
	if err != nil {
		t.Fatal(err)
	}
	state = drop.RemapState(state)
	if state.LastAnnotatedCommit == strings.Repeat("0", 40) ||
		state.Pending.BaseCommit == strings.Repeat("0", 40) {
		t.Fatalf("drop stored in state: %+v", state)
	}
}

func TestReferenceFilterAndStashInput(t *testing.T) {
	t.Parallel()
	relevant, err := HasRelevantReference(strings.NewReader(
		oldCommit + " " + newCommit + " refs/tags/v1\n",
	))
	if err != nil || relevant {
		t.Fatalf("tag filter = %t, %v", relevant, err)
	}
	relevant, err = HasRelevantReference(strings.NewReader(
		oldCommit + " " + newCommit + " refs/heads/main\n",
	))
	if err != nil || !relevant {
		t.Fatalf("branch filter = %t, %v", relevant, err)
	}
	stash, err := ParseStashApply(
		strings.NewReader(newCommit+" 1\n"),
		40,
	)
	if err != nil || stash.Commit != newCommit || !stash.Keep {
		t.Fatalf("stash input = %+v, %v", stash, err)
	}
	if _, err := ParseStashApply(strings.NewReader(newCommit+" 2\n"), 40); err == nil {
		t.Fatal("ParseStashApply accepted an invalid keep flag")
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
