package engine

import (
	"bytes"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/mrwogu/git-byline/internal/model"
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
