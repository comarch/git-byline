// Package engine performs deterministic line-level attribution transforms.
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"

	"github.com/mrwogu/git-byline/internal/model"
)

const (
	maxLCSCells = 4_000_000
)

// Transition is one observed file snapshot and its attribution.
type Transition struct {
	Content     []byte
	Attribution model.Attribution
}

// Snapshot is content split into lines with one attribution per line.
type Snapshot struct {
	Lines        []string
	Attributions []model.Attribution
}

// SplitLines splits UTF-8 text while retaining line terminators.
func SplitLines(content []byte) ([]string, error) {
	if bytes.IndexByte(content, 0) >= 0 {
		return nil, errors.New("binary content contains NUL")
	}
	if !utf8.Valid(content) {
		return nil, errors.New("content is not valid UTF-8")
	}
	if len(content) == 0 {
		return nil, nil
	}
	lineCount := bytes.Count(content, []byte{'\n'})
	if content[len(content)-1] != '\n' {
		lineCount++
	}
	if lineCount > model.MaxTextLines {
		return nil, fmt.Errorf("content exceeds %d lines", model.MaxTextLines)
	}
	lines := make([]string, 0, lineCount)
	start := 0
	for i, value := range content {
		if value == '\n' {
			lines = append(lines, string(content[start:i+1]))
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, string(content[start:]))
	}
	return lines, nil
}

// NewSnapshot creates attributed content from validated ranges.
func NewSnapshot(content []byte, ranges []model.Range) (Snapshot, error) {
	lines, err := SplitLines(content)
	if err != nil {
		return Snapshot{}, err
	}
	if err := model.ValidateRanges(ranges, len(lines)); err != nil {
		return Snapshot{}, fmt.Errorf("validate initial ranges: %w", err)
	}
	attrs := make([]model.Attribution, len(lines))
	for _, value := range ranges {
		for i := value.Start - 1; i < value.End; i++ {
			attrs[i] = value.Attribution
		}
	}
	return Snapshot{Lines: lines, Attributions: attrs}, nil
}

// UniformRanges covers every line with one attribution.
func UniformRanges(content []byte, attr model.Attribution) ([]model.Range, error) {
	if err := model.ValidateAttribution(attr); err != nil {
		return nil, err
	}
	lines, err := SplitLines(content)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []model.Range{{Start: 1, End: len(lines), Attribution: attr}}, nil
}

// Replay applies observed transitions to an initial snapshot.
func Replay(initial Snapshot, transitions []Transition) (Snapshot, error) {
	current := initial
	if len(current.Lines) != len(current.Attributions) {
		return Snapshot{}, errors.New("initial snapshot line and attribution counts differ")
	}
	for i, transition := range transitions {
		if err := model.ValidateAttribution(transition.Attribution); err != nil {
			return Snapshot{}, fmt.Errorf("transition %d attribution: %w", i, err)
		}
		nextLines, err := SplitLines(transition.Content)
		if err != nil {
			return Snapshot{}, fmt.Errorf("transition %d content: %w", i, err)
		}
		current = projectLines(current, nextLines, transition.Attribution)
	}
	return current, nil
}

// Project maps a replayed snapshot onto target content.
func Project(source Snapshot, target []byte, fallback model.Attribution) (Snapshot, error) {
	if len(source.Lines) != len(source.Attributions) {
		return Snapshot{}, errors.New("source snapshot line and attribution counts differ")
	}
	if err := model.ValidateAttribution(fallback); err != nil {
		return Snapshot{}, fmt.Errorf("fallback attribution: %w", err)
	}
	lines, err := SplitLines(target)
	if err != nil {
		return Snapshot{}, err
	}
	return projectLines(source, lines, fallback), nil
}

func projectLines(source Snapshot, target []string, fallback model.Attribution) Snapshot {
	attrs := make([]model.Attribution, len(target))
	for i := range attrs {
		attrs[i] = fallback
	}
	for _, pair := range equalPairs(source.Lines, target) {
		attrs[pair.new] = source.Attributions[pair.old]
	}
	return Snapshot{Lines: target, Attributions: attrs}
}

// Ranges merges adjacent lines carrying identical attribution.
func (snapshot Snapshot) Ranges() ([]model.Range, error) {
	if len(snapshot.Lines) != len(snapshot.Attributions) {
		return nil, errors.New("snapshot line and attribution counts differ")
	}
	if len(snapshot.Lines) == 0 {
		return nil, nil
	}
	ranges := make([]model.Range, 0, len(snapshot.Lines))
	start := 0
	for i := 1; i <= len(snapshot.Attributions); i++ {
		if i < len(snapshot.Attributions) && snapshot.Attributions[i] == snapshot.Attributions[start] {
			continue
		}
		ranges = append(ranges, model.Range{
			Start:       start + 1,
			End:         i,
			Attribution: snapshot.Attributions[start],
		})
		start = i
	}
	if err := model.ValidateRanges(ranges, len(snapshot.Lines)); err != nil {
		return nil, err
	}
	return ranges, nil
}

type linePair struct {
	old int
	new int
}

func equalPairs(oldLines, newLines []string) []linePair {
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) && oldLines[prefix] == newLines[prefix] {
		prefix++
	}
	oldEnd := len(oldLines)
	newEnd := len(newLines)
	for oldEnd > prefix && newEnd > prefix && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	pairs := make([]linePair, 0, prefix+len(oldLines)-oldEnd)
	for i := 0; i < prefix; i++ {
		pairs = append(pairs, linePair{old: i, new: i})
	}
	middleOld := oldLines[prefix:oldEnd]
	middleNew := newLines[prefix:newEnd]
	var middle []linePair
	if len(middleOld) > 0 && len(middleNew) > 0 {
		if len(middleOld) <= maxLCSCells/len(middleNew) {
			middle = lcsPairs(middleOld, middleNew)
		} else {
			middle = greedyPairs(middleOld, middleNew)
		}
	}
	for _, value := range middle {
		pairs = append(pairs, linePair{old: value.old + prefix, new: value.new + prefix})
	}
	suffix := len(oldLines) - oldEnd
	for i := 0; i < suffix; i++ {
		pairs = append(pairs, linePair{old: oldEnd + i, new: newEnd + i})
	}
	return pairs
}

func lcsPairs(oldLines, newLines []string) []linePair {
	width := len(newLines) + 1
	cells := make([]uint32, (len(oldLines)+1)*width)
	at := func(i, j int) int { return i*width + j }
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				cells[at(i, j)] = cells[at(i+1, j+1)] + 1
			} else if cells[at(i+1, j)] >= cells[at(i, j+1)] {
				cells[at(i, j)] = cells[at(i+1, j)]
			} else {
				cells[at(i, j)] = cells[at(i, j+1)]
			}
		}
	}
	var pairs []linePair
	for i, j := 0, 0; i < len(oldLines) && j < len(newLines); {
		switch {
		case oldLines[i] == newLines[j]:
			pairs = append(pairs, linePair{old: i, new: j})
			i++
			j++
		case cells[at(i+1, j)] >= cells[at(i, j+1)]:
			i++
		default:
			j++
		}
	}
	return pairs
}

func greedyPairs(oldLines, newLines []string) []linePair {
	positions := make(map[string][]int, len(oldLines))
	for i, line := range oldLines {
		positions[line] = append(positions[line], i)
	}
	var pairs []linePair
	nextOld := 0
	for newIndex, line := range newLines {
		indexes := positions[line]
		at := sort.SearchInts(indexes, nextOld)
		if at == len(indexes) {
			continue
		}
		oldIndex := indexes[at]
		pairs = append(pairs, linePair{old: oldIndex, new: newIndex})
		nextOld = oldIndex + 1
	}
	return pairs
}
