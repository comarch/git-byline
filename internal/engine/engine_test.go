package engine

import (
	"bytes"
	"math/rand"
	"reflect"
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

func TestReplayWithStatsReportsTransitionLineChanges(t *testing.T) {
	t.Parallel()
	ai := model.Attribution{Author: model.AuthorAI, Agent: "droid"}
	_, stats, err := ReplayWithStats(Snapshot{}, []Transition{
		{Content: []byte("agent line\nkept\n"), Attribution: ai},
		{Content: []byte("human line\nkept\n"), Attribution: model.Attribution{Author: model.AuthorHuman}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Added != 2 || stats[0].Deleted != 0 ||
		stats[1].Added != 1 || stats[1].Deleted != 1 {
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
