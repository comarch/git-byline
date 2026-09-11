// Package rewrite parses Git history-rewrite hook input and preserves ordering.
package rewrite

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/model"
)

const (
	maxInputBytes = 1 << 20
	maxInputLines = 10_000
)

// Pair maps one old commit to one rewritten commit.
type Pair struct {
	Old string
	New string
}

// Mapping stores an ordered old-to-new rewrite map.
type Mapping struct {
	Pairs  []Pair
	OldNew map[string][]string
	NewOld map[string][]string
}

// NewMapping validates pairs and indexes both directions.
func NewMapping(pairs []Pair) (Mapping, error) {
	mapping := Mapping{
		Pairs:  append([]Pair(nil), pairs...),
		OldNew: make(map[string][]string),
		NewOld: make(map[string][]string),
	}
	for index, pair := range pairs {
		if err := validateObjectID(pair.Old, "old commit", true); err != nil {
			return Mapping{}, fmt.Errorf("pair %d: %w", index, err)
		}
		if err := validateObjectID(pair.New, "new commit", true); err != nil {
			return Mapping{}, fmt.Errorf("pair %d: %w", index, err)
		}
		mapping.OldNew[pair.Old] = append(mapping.OldNew[pair.Old], pair.New)
		mapping.NewOld[pair.New] = append(mapping.NewOld[pair.New], pair.Old)
	}
	return mapping, nil
}

// Remap returns the last rewritten target for old.
func (mapping Mapping) Remap(old string) (string, bool) {
	values := mapping.OldNew[old]
	if len(values) == 0 {
		return "", false
	}
	return values[len(values)-1], true
}

// Sources returns old commits for new in the hook's input order.
func (mapping Mapping) Sources(new string) []string {
	return append([]string(nil), mapping.NewOld[new]...)
}

// Targets returns all rewritten commits for old in the hook's input order.
func (mapping Mapping) Targets(old string) []string {
	return append([]string(nil), mapping.OldNew[old]...)
}

// RemapState moves the durable annotation boundary through a rewrite map.
func (mapping Mapping) RemapState(state model.State) model.State {
	if target, ok := mapping.Remap(state.LastAnnotatedCommit); ok {
		state.LastAnnotatedCommit = target
	}
	if target, ok := mapping.Remap(state.Pending.BaseCommit); ok {
		state.Pending.BaseCommit = target
	}
	return state
}

// ParsePostRewrite parses Git post-rewrite lines containing old and new IDs.
func ParsePostRewrite(input io.Reader) (Mapping, error) {
	lines, err := readLines(input)
	if err != nil {
		return Mapping{}, err
	}
	pairs := make([]Pair, 0, len(lines))
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return Mapping{}, fmt.Errorf("post-rewrite line %d must contain old and new IDs", index+1)
		}
		pairs = append(pairs, Pair{Old: fields[0], New: fields[1]})
	}
	return NewMapping(pairs)
}

// RefUpdate is one line from a reference-transaction hook.
type RefUpdate struct {
	Old string
	New string
	Ref string
}

// ParseReferenceTransaction parses old, new, and ref fields.
func ParseReferenceTransaction(input io.Reader) ([]RefUpdate, error) {
	lines, err := readLines(input)
	if err != nil {
		return nil, err
	}
	updates := make([]RefUpdate, 0, len(lines))
	for index, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("reference-transaction line %d must contain old, new, and ref", index+1)
		}
		if err := validateObjectID(fields[0], "old object", true); err != nil {
			return nil, fmt.Errorf("reference-transaction line %d: %w", index+1, err)
		}
		if err := validateObjectID(fields[1], "new object", true); err != nil {
			return nil, fmt.Errorf("reference-transaction line %d: %w", index+1, err)
		}
		if err := validateHookRef(fields[2]); err != nil {
			return nil, fmt.Errorf("reference-transaction line %d: %w", index+1, err)
		}
		updates = append(updates, RefUpdate{Old: fields[0], New: fields[1], Ref: fields[2]})
	}
	return updates, nil
}

func readLines(input io.Reader) ([]string, error) {
	if input == nil {
		return nil, errors.New("rewrite hook input is nil")
	}
	reader := bufio.NewReader(io.LimitReader(input, maxInputBytes+1))
	var lines []string
	var total int
	for {
		line, err := reader.ReadString('\n')
		total += len(line)
		if total > maxInputBytes {
			return nil, fmt.Errorf("rewrite hook input exceeds %d bytes", maxInputBytes)
		}
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			line = strings.TrimSuffix(line, "\r")
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
				if len(lines) > maxInputLines {
					return nil, fmt.Errorf("rewrite hook input exceeds %d lines", maxInputLines)
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return lines, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read rewrite hook input: %w", err)
		}
	}
}

// ProjectLayered maps ordered sources onto target content.
func ProjectLayered(sources []engine.Snapshot, target []byte, fallback model.Attribution) (engine.Snapshot, error) {
	return engine.ProjectLayered(sources, target, fallback)
}

func validateObjectID(value, name string, allowZero bool) error {
	if allowZero && isZeroObjectID(value) {
		return nil
	}
	if !model.ValidObjectID(value) {
		return fmt.Errorf("%s is not a valid object ID", name)
	}
	return nil
}

func isZeroObjectID(value string) bool {
	if len(value) < 4 {
		return false
	}
	for _, char := range value {
		if char != '0' {
			return false
		}
	}
	return true
}

func validateHookRef(ref string) error {
	if ref == "" {
		return errors.New("reference is empty")
	}
	for _, char := range ref {
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' || char == 0 {
			return errors.New("reference contains whitespace or a control character")
		}
	}
	if strings.Contains(ref, "..") || strings.Contains(ref, "//") ||
		strings.Contains(ref, "@{") || strings.HasPrefix(ref, "/") ||
		strings.HasSuffix(ref, "/") {
		return errors.New("reference is not a valid Git ref")
	}
	for _, component := range strings.Split(ref, "/") {
		if component == "" || component == "." || component == ".." {
			return errors.New("reference is not a valid Git ref")
		}
	}
	return nil
}
