package engine

import (
	"bytes"
	"errors"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

func TestSplitLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    []string
		valid   bool
	}{
		{"empty", "", nil, true},
		{"one without terminator", "a", []string{"a"}, true},
		{"one with terminator", "a\n", []string{"a\n"}, true},
		{"multiple", "a\r\nb\nc", []string{"a\r\n", "b\n", "c"}, true},
		{"terminator only", "\n", []string{"\n"}, true},
		{"nul", "a\x00b", nil, false},
		{"invalid utf8", string([]byte{0xff}), nil, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := SplitLines([]byte(test.content))
			if (err == nil) != test.valid {
				t.Fatalf("SplitLines() error = %v, valid = %t", err, test.valid)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("SplitLines() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestSplitLinesRejectsExcessiveLineCount(t *testing.T) {
	t.Parallel()
	content := bytes.Repeat([]byte{'\n'}, model.MaxTextLines+1)
	if _, err := SplitLines(content); err == nil {
		t.Fatal("SplitLines accepted excessive line count")
	}
}

func TestReplayAndProject(t *testing.T) {
	t.Parallel()
	untracked := model.Attribution{Author: model.AuthorUntracked}
	human := model.Attribution{Author: model.AuthorHuman}
	ai := model.Attribution{Author: model.AuthorAI, Agent: "droid", Model: "test"}
	ranges, err := UniformRanges([]byte("old\nsame\n"), untracked)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := NewSnapshot([]byte("old\nsame\n"), ranges)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := Replay(initial, []Transition{
		{Content: []byte("old\nsame\nhuman\n"), Attribution: human},
		{Content: []byte("ai\nsame\nhuman\n"), Attribution: ai},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := replayed.Attributions; !reflect.DeepEqual(got, []model.Attribution{ai, untracked, human}) {
		t.Fatalf("attributions = %+v", got)
	}
	projected, err := Project(replayed, []byte("ai\nsame\nhuman\nfinal\n"), human)
	if err != nil {
		t.Fatal(err)
	}
	gotRanges, err := projected.Ranges()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateRanges(gotRanges, 4); err != nil {
		t.Fatal(err)
	}
	if projected.Attributions[3] != human {
		t.Fatalf("new final line = %+v, want human", projected.Attributions[3])
	}
	budgeted, err := ProjectWithBudget(
		replayed,
		[]byte("ai\nsame\nhuman\nfinal\n"),
		human,
		NewMatcherBudget(100),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(budgeted.Attributions, projected.Attributions) {
		t.Fatalf("ProjectWithBudget() attributions = %+v, want %+v",
			budgeted.Attributions, projected.Attributions)
	}
}

func TestProjectLayeredLaterSourceWins(t *testing.T) {
	t.Parallel()
	first := Snapshot{
		Lines: []string{"one\n", "same\n"},
		Attributions: []model.Attribution{
			{Author: model.AuthorAI, Agent: "first", Model: "model"},
			{Author: model.AuthorHuman},
		},
	}
	second := Snapshot{
		Lines: []string{"same\n", "two\n"},
		Attributions: []model.Attribution{
			{Author: model.AuthorAI, Agent: "second", Model: "model"},
			{Author: model.AuthorAI, Agent: "second", Model: "model"},
		},
	}
	got, err := ProjectLayered(
		[]Snapshot{first, second},
		[]byte("one\nsame\ntwo\nnew\n"),
		model.Attribution{Author: model.AuthorUntracked},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Attribution{
		{Author: model.AuthorAI, Agent: "first", Model: "model"},
		{Author: model.AuthorAI, Agent: "second", Model: "model"},
		{Author: model.AuthorAI, Agent: "second", Model: "model"},
		{Author: model.AuthorUntracked},
	}
	if !reflect.DeepEqual(got.Attributions, want) {
		t.Fatalf("ProjectLayered() attributions = %+v, want %+v", got.Attributions, want)
	}
	ranges, err := got.Ranges()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateRanges(ranges, len(got.Lines)); err != nil {
		t.Fatal(err)
	}
}

func TestProjectLayeredRejectsInvalidSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		sources  []Snapshot
		target   []byte
		fallback model.Attribution
	}{
		{
			name:     "mismatched source",
			sources:  []Snapshot{{Lines: []string{"one\n"}}},
			target:   []byte("one\n"),
			fallback: model.Attribution{Author: model.AuthorHuman},
		},
		{
			name: "invalid source attribution",
			sources: []Snapshot{{
				Lines:        []string{"one\n"},
				Attributions: []model.Attribution{{Author: "bad"}},
			}},
			target:   []byte("one\n"),
			fallback: model.Attribution{Author: model.AuthorHuman},
		},
		{
			name:     "invalid fallback",
			target:   []byte("one\n"),
			fallback: model.Attribution{Author: "bad"},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ProjectLayered(test.sources, test.target, test.fallback); err == nil {
				t.Fatal("ProjectLayered accepted invalid input")
			}
		})
	}
}

func TestReplayMarksHumanReplacementAsOverride(t *testing.T) {
	t.Parallel()
	ai := model.Attribution{
		Author:  model.AuthorAI,
		Agent:   "droid",
		Model:   "model",
		Session: "session-1",
		TS:      "2026-01-02T03:04:05Z",
	}
	replayed, err := Replay(Snapshot{}, []Transition{
		{Content: []byte("agent line\nkept\n"), Attribution: ai},
		{Content: []byte("human line\nkept\n"), Attribution: model.Attribution{Author: model.AuthorHuman}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Attribution{
		{
			Author:  model.AuthorHumanOverride,
			Agent:   ai.Agent,
			Model:   ai.Model,
			Session: ai.Session,
			TS:      ai.TS,
		},
		ai,
	}
	if !reflect.DeepEqual(replayed.Attributions, want) {
		t.Fatalf("Replay() attributions = %+v, want %+v", replayed.Attributions, want)
	}
	ranges, err := replayed.Ranges()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateRanges(ranges, len(replayed.Lines)); err != nil {
		t.Fatal(err)
	}
}

func TestReplayUsesInitialAIAttributionForHumanOverride(t *testing.T) {
	t.Parallel()
	ai := model.Attribution{
		Author:  model.AuthorAI,
		Agent:   "droid",
		Model:   "model",
		Session: "session-1",
		TS:      "2026-01-02T03:04:05Z",
	}
	tests := []struct {
		name          string
		initialAuthor model.Attribution
		wantAuthor    model.Author
	}{
		{
			name:          "parent AI line",
			initialAuthor: ai,
			wantAuthor:    model.AuthorHumanOverride,
		},
		{
			name:          "parent untracked line",
			initialAuthor: model.Attribution{Author: model.AuthorUntracked},
			wantAuthor:    model.AuthorHuman,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			initial := Snapshot{
				Lines:        []string{"parent\n"},
				Attributions: []model.Attribution{test.initialAuthor},
			}
			replayed, err := Replay(initial, []Transition{{
				Content:     []byte("human\n"),
				Attribution: model.Attribution{Author: model.AuthorHuman},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if len(replayed.Attributions) != 1 || replayed.Attributions[0].Author != test.wantAuthor {
				t.Fatalf("Replay() attributions = %+v, want %s", replayed.Attributions, test.wantAuthor)
			}
			if test.wantAuthor == model.AuthorHumanOverride &&
				(replayed.Attributions[0].Agent != ai.Agent ||
					replayed.Attributions[0].Model != ai.Model ||
					replayed.Attributions[0].Session != ai.Session ||
					replayed.Attributions[0].TS != ai.TS) {
				t.Fatalf("Replay() override metadata = %+v, want %+v", replayed.Attributions[0], ai)
			}
		})
	}
}

func TestReplayWithStatsReportsTransitionLineChanges(t *testing.T) {
	t.Parallel()
	ai := model.Attribution{Author: model.AuthorAI, Agent: "droid", Session: "session-1"}
	_, stats, err := ReplayWithStats(Snapshot{}, []Transition{
		{Content: []byte("agent line\nkept\n"), Attribution: ai},
		{Content: []byte("human line\nkept\n"), Attribution: model.Attribution{Author: model.AuthorHuman}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Added != 2 || stats[0].Deleted != 0 ||
		stats[1].Added != 1 || stats[1].Deleted != 1 ||
		len(stats[1].Overridden) != 1 {
		t.Fatalf("ReplayWithStats() = %+v", stats)
	}
}

func TestReplayDoesNotGuessHumanLinesOutsideAITransitionGap(t *testing.T) {
	t.Parallel()
	replayed, err := Replay(Snapshot{}, []Transition{
		{
			Content: []byte("agent line\n"),
			Attribution: model.Attribution{
				Author: model.AuthorAI, Agent: "droid",
			},
		},
		{
			Content:     []byte("human line\n"),
			Attribution: model.Attribution{Author: model.AuthorHuman},
		},
		{
			Content:     []byte("human line\nnew line\n"),
			Attribution: model.Attribution{Author: model.AuthorHuman},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Attributions[0].Author != model.AuthorHumanOverride ||
		replayed.Attributions[1].Author != model.AuthorHuman {
		t.Fatalf("Replay() guessed attribution = %+v", replayed.Attributions)
	}
}

func TestReplayErrors(t *testing.T) {
	t.Parallel()
	if _, err := Replay(Snapshot{Lines: []string{"a"}}, nil); err == nil {
		t.Fatal("Replay accepted mismatched initial snapshot")
	}
	if _, err := Replay(Snapshot{}, []Transition{{Content: []byte("a"), Attribution: model.Attribution{Author: "bad"}}}); err == nil {
		t.Fatal("Replay accepted invalid attribution")
	}
	if _, err := Project(Snapshot{Lines: []string{"a"}}, nil, model.Attribution{Author: model.AuthorHuman}); err == nil {
		t.Fatal("Project accepted mismatched source snapshot")
	}
	if _, err := UniformRanges([]byte("a"), model.Attribution{Author: "bad"}); err == nil {
		t.Fatal("UniformRanges accepted invalid attribution")
	}
	if _, err := (Snapshot{Lines: []string{"a"}}).Ranges(); err == nil {
		t.Fatal("Ranges accepted mismatched snapshot")
	}
}

func TestDuplicateLinesAreDeterministic(t *testing.T) {
	t.Parallel()
	old := []string{"x\n", "same\n", "same\n", "y\n"}
	newLines := []string{"same\n", "x\n", "same\n", "y\n"}
	first := equalPairs(old, newLines)
	for i := 0; i < 20; i++ {
		if got := equalPairs(old, newLines); !reflect.DeepEqual(got, first) {
			t.Fatalf("equalPairs run %d = %+v, want %+v", i, got, first)
		}
	}
}

func TestWhitespaceMatchingStaysBetweenExactAnchors(t *testing.T) {
	t.Parallel()
	oldLines := []string{
		"package example\n",
		"func value() {\n",
		"\tfirst := 1\n",
		"\tsecond := 2\n",
		"}\n",
	}
	newLines := []string{
		"package example\n",
		"func value() {\n",
		"  first := 1\n",
		"  second := 2\n",
		"}\n",
	}
	got := equalPairs(oldLines, newLines)
	want := []linePair{
		{old: 0, new: 0},
		{old: 1, new: 1},
		{old: 2, new: 2},
		{old: 3, new: 3},
		{old: 4, new: 4},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("equalPairs() = %+v, want %+v", got, want)
	}
}

func TestWhitespaceMatchingDoesNotGuessReflow(t *testing.T) {
	t.Parallel()
	oldLines := []string{
		"const value = {\n",
		"  first: 1,\n",
		"  second: 2,\n",
		"};\n",
	}
	newLines := []string{
		"const value = {\n",
		"  first: 1, second: 2,\n",
		"};\n",
	}
	got := equalPairs(oldLines, newLines)
	want := []linePair{{old: 0, new: 0}, {old: 3, new: 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("equalPairs() = %+v, want %+v", got, want)
	}
}

func TestLayeredMatchingGoldenContract(t *testing.T) {
	t.Parallel()
	source := Snapshot{
		Lines: []string{
			"start\n",
			"\tfirst\n",
			"\tsecond\n",
			"end\n",
		},
		Attributions: []model.Attribution{
			{Author: model.AuthorHuman},
			{Author: model.AuthorAI, Agent: "droid", Model: "model"},
			{Author: model.AuthorAI, Agent: "droid", Model: "model"},
			{Author: model.AuthorHuman},
		},
	}
	target := []byte("start\n  first\n  second\nnew\nend\n")
	projected, err := Project(source, target, model.Attribution{Author: model.AuthorHuman})
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Attribution{
		{Author: model.AuthorHuman},
		{Author: model.AuthorAI, Agent: "droid", Model: "model"},
		{Author: model.AuthorAI, Agent: "droid", Model: "model"},
		{Author: model.AuthorHuman},
		{Author: model.AuthorHuman},
	}
	if !reflect.DeepEqual(projected.Attributions, want) {
		t.Fatalf("Project() attributions = %+v, want %+v", projected.Attributions, want)
	}
	ranges, err := projected.Ranges()
	if err != nil {
		t.Fatal(err)
	}
	if err := model.ValidateRanges(ranges, len(projected.Lines)); err != nil {
		t.Fatal(err)
	}
}

func TestGreedyFallback(t *testing.T) {
	t.Parallel()
	direct := greedyPairs(
		[]string{"one", "same", "same", "three"},
		[]string{"same", "one", "same", "three"},
	)
	if len(direct) != 3 {
		t.Fatalf("greedyPairs() returned %d pairs, want 3", len(direct))
	}
	oldLines := make([]string, 2_001)
	newLines := make([]string, 2_001)
	for i := range oldLines {
		oldLines[i] = string(rune('a' + i%20))
		newLines[i] = oldLines[i]
	}
	newLines[1_000] = "changed"
	pairs := equalPairs(oldLines, newLines)
	if len(pairs) != 2_000 {
		t.Fatalf("equalPairs() returned %d pairs, want 2000", len(pairs))
	}
}

func TestRandomReplayInvariants(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewSource(42))
	human := model.Attribution{Author: model.AuthorHuman}
	ai := model.Attribution{Author: model.AuthorAI, Agent: "test"}
	for iteration := 0; iteration < 10_000; iteration++ {
		var transitions []Transition
		lineCount := random.Intn(12)
		for step := 0; step < 5; step++ {
			var value strings.Builder
			for line := 0; line < lineCount; line++ {
				value.WriteByte(byte('a' + random.Intn(5)))
				value.WriteByte('\n')
			}
			attr := human
			if step%2 == 1 {
				attr = ai
			}
			transitions = append(transitions, Transition{Content: []byte(value.String()), Attribution: attr})
			lineCount += random.Intn(3) - 1
			if lineCount < 0 {
				lineCount = 0
			}
		}
		snapshot, err := Replay(Snapshot{}, transitions)
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		ranges, err := snapshot.Ranges()
		if err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		if err := model.ValidateRanges(ranges, len(snapshot.Lines)); err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
	}
}

func TestProjectLayeredHonorsAggregateMatcherBudget(t *testing.T) {
	t.Parallel()
	if budget := NewMatcherBudget(-1); budget.remaining != 0 {
		t.Fatalf("negative matcher budget = %d, want zero", budget.remaining)
	}
	const lineCount = 1_500
	var old strings.Builder
	var target strings.Builder
	for i := 0; i < lineCount; i++ {
		old.WriteString("source-")
		old.WriteString(strconv.Itoa(i))
		old.WriteByte('\n')
		target.WriteString("target-")
		target.WriteString(strconv.Itoa(i))
		target.WriteByte('\n')
	}
	content := []byte(old.String())
	lines, err := SplitLines(content)
	if err != nil {
		t.Fatal(err)
	}
	source := Snapshot{
		Lines:        lines,
		Attributions: make([]model.Attribution, len(lines)),
	}
	for i := range source.Attributions {
		source.Attributions[i] = model.Attribution{Author: model.AuthorUntracked}
	}
	_, err = ProjectLayeredWithBudget(
		[]Snapshot{source, source},
		[]byte(target.String()),
		model.Attribution{Author: model.AuthorUntracked},
		NewMatcherBudget(maxLCSCells),
	)
	if !errors.Is(err, ErrMatcherBudget) {
		t.Fatalf("budget error = %v, want %v", err, ErrMatcherBudget)
	}
}

func FuzzSplitLines(f *testing.F) {
	for _, value := range [][]byte{nil, []byte("a\n"), []byte("a\r\nb"), {0}, {0xff}} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value []byte) {
		lines, err := SplitLines(value)
		if err != nil {
			return
		}
		if strings.Join(lines, "") != string(value) {
			t.Fatalf("joined lines differ from input")
		}
	})
}

func FuzzLayeredMatcher(f *testing.F) {
	for _, seed := range [][2]string{
		{"a\n\tb\nc\n", "a\n  b\nc\n"},
		{"start\none\ntwo\nend\n", "start\n  one\n  two\nend\n"},
		{"one\ntwo\n", "one\ntwo\nthree\n"},
	} {
		f.Add(seed[0], seed[1])
	}
	f.Fuzz(func(t *testing.T, oldText, newText string) {
		if len(oldText) > 64<<10 || len(newText) > 64<<10 {
			return
		}
		oldLines, err := SplitLines([]byte(oldText))
		if err != nil {
			return
		}
		newLines, err := SplitLines([]byte(newText))
		if err != nil {
			return
		}
		first := equalPairs(oldLines, newLines)
		for i := 0; i < 3; i++ {
			if got := equalPairs(oldLines, newLines); !reflect.DeepEqual(got, first) {
				t.Fatalf("equalPairs run %d = %+v, want %+v", i, got, first)
			}
		}
		source := Snapshot{
			Lines:        oldLines,
			Attributions: make([]model.Attribution, len(oldLines)),
		}
		for i := range source.Attributions {
			source.Attributions[i] = model.Attribution{Author: model.AuthorUntracked}
		}
		projected, err := Project(source, []byte(newText), model.Attribution{Author: model.AuthorHuman})
		if err != nil {
			t.Fatal(err)
		}
		if len(projected.Lines) != len(projected.Attributions) {
			t.Fatalf("line coverage length = %d/%d", len(projected.Lines), len(projected.Attributions))
		}
		ranges, err := projected.Ranges()
		if err != nil {
			t.Fatal(err)
		}
		if err := model.ValidateRanges(ranges, len(projected.Lines)); err != nil {
			t.Fatalf("ValidateRanges() = %v", err)
		}
	})
}

// TestNewSnapshotRejectsInvalidContentAndRanges covers the NewSnapshot
// guards: invalid UTF-8 content and ranges that do not cover the content.
func TestNewSnapshotRejectsInvalidContentAndRanges(t *testing.T) {
	t.Parallel()
	human := model.Attribution{Author: model.AuthorHuman}
	if _, err := NewSnapshot([]byte{0xff, 'a'}, nil); err == nil {
		t.Fatal("NewSnapshot accepted invalid UTF-8 content")
	}
	shortRanges := []model.Range{{Start: 1, End: 1, Attribution: human}}
	if _, err := NewSnapshot([]byte("a\nb\n"), shortRanges); err == nil {
		t.Fatal("NewSnapshot accepted ranges that do not cover the content")
	}
}

// TestUniformRangesRejectsInvalidContentAndEmptyContent covers the
// UniformRanges guards: invalid UTF-8 content and empty content.
func TestUniformRangesRejectsInvalidContentAndEmptyContent(t *testing.T) {
	t.Parallel()
	human := model.Attribution{Author: model.AuthorHuman}
	if _, err := UniformRanges([]byte{0xff}, human); err == nil {
		t.Fatal("UniformRanges accepted invalid UTF-8 content")
	}
	ranges, err := UniformRanges(nil, human)
	if err != nil || ranges != nil {
		t.Fatalf("UniformRanges(nil) = %v, %v; want nil, nil", ranges, err)
	}
}

// TestReplayWithStatsTransitionContentError pins the transition content
// guard: invalid UTF-8 content fails the whole replay.
func TestReplayWithStatsTransitionContentError(t *testing.T) {
	t.Parallel()
	human := model.Attribution{Author: model.AuthorHuman}
	_, _, err := ReplayWithStats(Snapshot{}, []Transition{{Content: []byte{0xff}, Attribution: human}})
	if err == nil || !strings.Contains(err.Error(), "transition 0 content") {
		t.Fatalf("ReplayWithStats() error = %v, want transition content failure", err)
	}
}

// TestReplayWithStatsOverrideSelection covers the override selection
// branches: non-AI lines and same-agent same-session AI lines are not
// overrides, while other-session AI lines are.
func TestReplayWithStatsOverrideSelection(t *testing.T) {
	t.Parallel()
	ai := model.Attribution{Author: model.AuthorAI, Agent: "droid", Session: "session-1"}
	other := model.Attribution{Author: model.AuthorAI, Agent: "claude", Session: "session-2"}
	initial, err := NewSnapshot([]byte("human\nsame\nother\n"), []model.Range{
		{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 2, End: 2, Attribution: ai},
		{Start: 3, End: 3, Attribution: other},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, stats, err := ReplayWithStats(initial, []Transition{
		{Content: []byte("replacement\n"), Attribution: ai},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || len(stats[0].Overridden) != 1 {
		t.Fatalf("ReplayWithStats() = %+v", stats)
	}
	if got := stats[0].Overridden[0]; got.Agent != "claude" || got.Session != "session-2" {
		t.Fatalf("overridden attribution = %+v, want the other-session AI line", got)
	}
}

// TestProjectGuards covers the ProjectWithBudget guards: invalid fallback
// attribution, invalid target content, and an exhausted matcher budget.
func TestProjectGuards(t *testing.T) {
	t.Parallel()
	human := model.Attribution{Author: model.AuthorHuman}
	source := Snapshot{Lines: []string{"a"}, Attributions: []model.Attribution{human}}
	if _, err := ProjectWithBudget(source, []byte{0xff}, human, nil); err == nil {
		t.Fatal("Project accepted invalid UTF-8 target content")
	}
	if _, err := ProjectWithBudget(source, []byte("a\n"), model.Attribution{Author: model.AuthorAI}, nil); err == nil {
		t.Fatal("Project accepted an invalid fallback attribution")
	}
	_, err := ProjectWithBudget(source, []byte("b\n"), human, NewMatcherBudget(0))
	if !errors.Is(err, ErrMatcherBudget) {
		t.Fatalf("Project budget error = %v, want %v", err, ErrMatcherBudget)
	}
	if _, err := ProjectLayered(nil, []byte{0xff}, human); err == nil {
		t.Fatal("ProjectLayered accepted invalid UTF-8 target content")
	}
}

// TestRangesRejectsInvalidAttribution pins the Ranges guard: a snapshot
// carrying an invalid attribution must fail range derivation rather
// than emit invalid ranges.
func TestRangesRejectsInvalidAttribution(t *testing.T) {
	t.Parallel()
	invalid := Snapshot{
		Lines:        []string{"a"},
		Attributions: []model.Attribution{{Author: model.AuthorAI}},
	}
	if _, err := invalid.Ranges(); err == nil {
		t.Fatal("Ranges accepted an invalid attribution")
	}
}

// TestMatcherGuards covers the matcher helpers directly: greedy fallback
// beyond the LCS cell ceiling, whitespace-gap budget exhaustion, and the
// nil-budget reserve fast path.
func TestMatcherGuards(t *testing.T) {
	t.Parallel()
	var hugeOld, hugeNew []string
	for i := 0; i < 4001; i++ {
		hugeOld = append(hugeOld, " old"+strconv.Itoa(i))
	}
	for i := 0; i < 1000; i++ {
		hugeNew = append(hugeNew, " new"+strconv.Itoa(i))
	}
	pairs, err := exactPairsBudget(hugeOld, hugeNew, nil)
	if err != nil {
		t.Fatalf("exactPairsBudget(greedy fallback) = %v", err)
	}
	if len(pairs) != 0 {
		t.Fatalf("exactPairsBudget(greedy fallback) = %d pairs, want none", len(pairs))
	}
	if _, err := whitespacePairsBudget(hugeOld, hugeNew, 0, 0, nil); err != nil {
		t.Fatalf("whitespacePairsBudget(greedy fallback) = %v", err)
	}
	if _, err := whitespacePairsBudget(hugeOld[0:2], hugeNew[0:2], 0, 0, NewMatcherBudget(0)); !errors.Is(err, ErrMatcherBudget) {
		t.Fatalf("whitespace budget error = %v, want %v", err, ErrMatcherBudget)
	}
	old := []string{"a", " p", " q", " r", "b"}
	next := []string{"a", " P", " Q", " R", "b"}
	if _, err := equalPairsBudget(old, next, NewMatcherBudget(10)); !errors.Is(err, ErrMatcherBudget) {
		t.Fatalf("anchor gap budget error = %v, want %v", err, ErrMatcherBudget)
	}
	var nilBudget *MatcherBudget
	if !nilBudget.reserve(2, 3) {
		t.Fatal("nil budget reserve = false, want true")
	}
}
