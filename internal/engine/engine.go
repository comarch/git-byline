// Package engine performs deterministic line-level attribution transforms.
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/model"
)

const (
	maxLCSCells = 4_000_000
)

// Transition is one observed file snapshot and its attribution.
type Transition struct {
	Content     []byte
	Attribution model.Attribution
}

// TransitionStats reports line changes caused by one transition.
type TransitionStats struct {
	Attribution model.Attribution
	Added       int
	Deleted     int
	Overridden  []model.Attribution
}

// Snapshot is content split into lines with one attribution per line.
type Snapshot struct {
	Lines        []string
	Attributions []model.Attribution
}

// MatcherBudget bounds aggregate dynamic-programming work for one operation.
type MatcherBudget struct {
	remaining int64
}

// NewMatcherBudget creates a bounded line-matcher budget.
func NewMatcherBudget(cells int64) *MatcherBudget {
	if cells < 0 {
		cells = 0
	}
	return &MatcherBudget{remaining: cells}
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
	current, _, err := ReplayWithStats(initial, transitions)
	return current, err
}

// ReplayWithStats applies transitions and reports their line changes.
func ReplayWithStats(initial Snapshot, transitions []Transition) (Snapshot, []TransitionStats, error) {
	current := initial
	if len(current.Lines) != len(current.Attributions) {
		return Snapshot{}, nil, errors.New("initial snapshot line and attribution counts differ")
	}
	latestAI, hasLatestAI := latestAIAttribution(current.Attributions)
	stats := make([]TransitionStats, 0, len(transitions))
	for i, transition := range transitions {
		if err := model.ValidateAttribution(transition.Attribution); err != nil {
			return Snapshot{}, nil, fmt.Errorf("transition %d attribution: %w", i, err)
		}
		nextLines, err := SplitLines(transition.Content)
		if err != nil {
			return Snapshot{}, nil, fmt.Errorf("transition %d content: %w", i, err)
		}
		pairs := equalPairs(current.Lines, nextLines)
		transitionStats := TransitionStats{
			Attribution: transition.Attribution,
			Added:       len(nextLines) - len(pairs),
			Deleted:     len(current.Lines) - len(pairs),
		}
		for _, gap := range unmatchedGaps(len(current.Lines), len(nextLines), pairs) {
			for oldIndex := gap.oldStart; oldIndex < gap.oldEnd; oldIndex++ {
				previous := current.Attributions[oldIndex]
				if previous.Author != model.AuthorAI || previous.Session == "" {
					continue
				}
				if transition.Attribution.Author == model.AuthorAI &&
					previous.Agent == transition.Attribution.Agent &&
					previous.Session == transition.Attribution.Session {
					continue
				}
				transitionStats.Overridden = append(transitionStats.Overridden, previous)
			}
		}
		stats = append(stats, transitionStats)
		var override model.Attribution
		if transition.Attribution.Author == model.AuthorHuman && hasLatestAI {
			override = latestAI
		}
		current = projectLinesWithOverrideAndPairs(current, nextLines, transition.Attribution, override, pairs)
		if transition.Attribution.Author == model.AuthorAI {
			latestAI = transition.Attribution
			hasLatestAI = true
		}
	}
	return current, stats, nil
}

func latestAIAttribution(values []model.Attribution) (model.Attribution, bool) {
	var latest model.Attribution
	found := false
	for _, value := range values {
		if value.Author == model.AuthorAI {
			latest = value
			found = true
		}
	}
	return latest, found
}

// Project maps a replayed snapshot onto target content.
func Project(source Snapshot, target []byte, fallback model.Attribution) (Snapshot, error) {
	return projectWithBudget(source, target, fallback, nil)
}

// ProjectWithBudget maps one source onto target with a shared matcher budget.
func ProjectWithBudget(source Snapshot, target []byte, fallback model.Attribution, budget *MatcherBudget) (Snapshot, error) {
	return projectWithBudget(source, target, fallback, budget)
}

func projectWithBudget(source Snapshot, target []byte, fallback model.Attribution, budget *MatcherBudget) (Snapshot, error) {
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
	pairs, err := equalPairsBudget(source.Lines, lines, budget)
	if err != nil {
		return Snapshot{}, err
	}
	return projectLinesWithOverrideAndPairs(source, lines, fallback, model.Attribution{}, pairs), nil
}

// ProjectLayered maps ordered source snapshots onto target content.
// A later source replaces attribution for lines it matches.
func ProjectLayered(sources []Snapshot, target []byte, fallback model.Attribution) (Snapshot, error) {
	return projectLayeredWithBudget(sources, target, fallback, nil)
}

// ProjectLayeredWithBudget maps sources onto target with a shared matcher budget.
func ProjectLayeredWithBudget(
	sources []Snapshot,
	target []byte,
	fallback model.Attribution,
	budget *MatcherBudget,
) (Snapshot, error) {
	return projectLayeredWithBudget(sources, target, fallback, budget)
}

func projectLayeredWithBudget(
	sources []Snapshot,
	target []byte,
	fallback model.Attribution,
	budget *MatcherBudget,
) (Snapshot, error) {
	if err := model.ValidateAttribution(fallback); err != nil {
		return Snapshot{}, fmt.Errorf("fallback attribution: %w", err)
	}
	lines, err := SplitLines(target)
	if err != nil {
		return Snapshot{}, err
	}
	attrs := make([]model.Attribution, len(lines))
	for index := range attrs {
		attrs[index] = fallback
	}
	for index, source := range sources {
		if len(source.Lines) != len(source.Attributions) {
			return Snapshot{}, fmt.Errorf(
				"source snapshot %d line and attribution counts differ",
				index,
			)
		}
		for line, attribution := range source.Attributions {
			if err := model.ValidateAttribution(attribution); err != nil {
				return Snapshot{}, fmt.Errorf("source snapshot %d line %d: %w", index, line+1, err)
			}
		}
		pairs, err := equalPairsBudget(source.Lines, lines, budget)
		if err != nil {
			return Snapshot{}, fmt.Errorf("source snapshot %d: %w", index, err)
		}
		for _, pair := range pairs {
			attrs[pair.new] = source.Attributions[pair.old]
		}
	}
	return Snapshot{Lines: lines, Attributions: attrs}, nil
}

func projectLinesWithOverrideAndPairs(
	source Snapshot,
	target []string,
	fallback model.Attribution,
	override model.Attribution,
	pairs []linePair,
) Snapshot {
	attrs := make([]model.Attribution, len(target))
	for i := range attrs {
		attrs[i] = fallback
	}
	for _, pair := range pairs {
		attrs[pair.new] = source.Attributions[pair.old]
	}
	if override.Author == model.AuthorAI {
		for _, gap := range unmatchedGaps(len(source.Lines), len(target), pairs) {
			hasOverride := false
			for oldIndex := gap.oldStart; oldIndex < gap.oldEnd; oldIndex++ {
				if source.Attributions[oldIndex] == override {
					hasOverride = true
					break
				}
			}
			if !hasOverride {
				continue
			}
			for newIndex := gap.newStart; newIndex < gap.newEnd; newIndex++ {
				attrs[newIndex] = model.Attribution{
					Author:  model.AuthorHumanOverride,
					Agent:   override.Agent,
					Model:   override.Model,
					Session: override.Session,
					TS:      override.TS,
				}
			}
		}
	}
	return Snapshot{Lines: target, Attributions: attrs}
}

type lineGap struct {
	oldStart int
	oldEnd   int
	newStart int
	newEnd   int
}

func unmatchedGaps(oldCount, newCount int, pairs []linePair) []lineGap {
	gaps := make([]lineGap, 0, len(pairs)+1)
	oldStart, newStart := 0, 0
	for _, pair := range pairs {
		if oldStart < pair.old || newStart < pair.new {
			gaps = append(gaps, lineGap{
				oldStart: oldStart,
				oldEnd:   pair.old,
				newStart: newStart,
				newEnd:   pair.new,
			})
		}
		oldStart = pair.old + 1
		newStart = pair.new + 1
	}
	if oldStart < oldCount || newStart < newCount {
		gaps = append(gaps, lineGap{
			oldStart: oldStart,
			oldEnd:   oldCount,
			newStart: newStart,
			newEnd:   newCount,
		})
	}
	return gaps
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
	pairs, _ := equalPairsBudget(oldLines, newLines, nil)
	return pairs
}

// ErrMatcherBudget reports exhausted aggregate matcher work.
var ErrMatcherBudget = errors.New("line matcher budget exceeded")

func equalPairsBudget(oldLines, newLines []string, budget *MatcherBudget) ([]linePair, error) {
	pairs, err := exactPairsBudget(oldLines, newLines, budget)
	if err != nil {
		return nil, err
	}
	if len(pairs) < 2 {
		return pairs, nil
	}
	result := make([]linePair, 0, len(pairs))
	previous := pairs[0]
	result = append(result, previous)
	for _, anchor := range pairs[1:] {
		oldGap := oldLines[previous.old+1 : anchor.old]
		newGap := newLines[previous.new+1 : anchor.new]
		whitespace, err := whitespacePairsBudget(
			oldGap,
			newGap,
			previous.old+1,
			previous.new+1,
			budget,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, whitespace...)
		result = append(result, anchor)
		previous = anchor
	}
	return result, nil
}

func exactPairsBudget(oldLines, newLines []string, budget *MatcherBudget) ([]linePair, error) {
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
			if budget != nil && !budget.reserve(len(middleOld), len(middleNew)) {
				return nil, ErrMatcherBudget
			}
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
	return pairs, nil
}

func whitespacePairsBudget(
	oldLines,
	newLines []string,
	oldOffset,
	newOffset int,
	budget *MatcherBudget,
) ([]linePair, error) {
	if len(oldLines) == 0 || len(newLines) == 0 {
		return nil, nil
	}
	oldKeys := make([]string, len(oldLines))
	newKeys := make([]string, len(newLines))
	for i, line := range oldLines {
		oldKeys[i] = strings.TrimSpace(line)
	}
	for i, line := range newLines {
		newKeys[i] = strings.TrimSpace(line)
	}
	var pairs []linePair
	if len(oldLines) <= maxLCSCells/len(newLines) {
		if budget != nil && !budget.reserve(len(oldLines), len(newLines)) {
			return nil, ErrMatcherBudget
		}
		pairs = lcsPairs(oldKeys, newKeys)
	} else {
		pairs = greedyPairs(oldKeys, newKeys)
	}
	for i := range pairs {
		pairs[i].old += oldOffset
		pairs[i].new += newOffset
	}
	return pairs, nil
}

func (budget *MatcherBudget) reserve(oldCount, newCount int) bool {
	if budget == nil {
		return true
	}
	cells := int64(oldCount) * int64(newCount)
	if cells < 0 || cells > budget.remaining {
		return false
	}
	budget.remaining -= cells
	return true
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
