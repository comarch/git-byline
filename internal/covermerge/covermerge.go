// Package covermerge merges Go coverage profiles.
//
// It combines a `go test -coverprofile` profile with profiles produced
// by `go tool covdata textfmt` from binaries built with `go build
// -cover`, summing execution counts per coverage block. The merged
// profile is what coverage gates measure, so statements covered only
// through spawned binaries (package main) still count.
package covermerge

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// block is one coverage block keyed by file and source span.
type block struct {
	file   string
	span   string
	stmts  int
	counts int64
}

// MergeFiles merges the input profiles into out and returns the covered
// percentage of statements plus the merged block count. All inputs must
// share one coverage mode and identical block shapes.
func MergeFiles(inputs []string, out string) (float64, int, error) {
	if len(inputs) == 0 {
		return 0, 0, errors.New("no input profiles")
	}
	blocks, mode, err := readProfiles(inputs)
	if err != nil {
		return 0, 0, err
	}
	if err := writeProfile(out, mode, blocks); err != nil {
		return 0, 0, err
	}
	return percent(blocks), len(blocks), nil
}

// readProfiles parses every input profile and sums counts per block.
func readProfiles(inputs []string) ([]block, string, error) {
	var mode string
	index := make(map[string]*block)
	var order []string
	for _, path := range inputs {
		if err := readOne(path, &mode, index, &order); err != nil {
			return nil, "", err
		}
	}
	blocks := make([]block, 0, len(order))
	for _, key := range order {
		blocks = append(blocks, *index[key])
	}
	return blocks, mode, nil
}

// readOne folds one profile into index. Keys of new blocks are appended
// to order in first-seen sequence; mode tracks the header seen so far.
func readOne(path string, mode *string, index map[string]*block, order *[]string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open profile: %w", err)
	}
	defer f.Close()
	return scanProfile(f, path, mode, index, order)
}

// scanProfile parses profile content from r and folds it into index.
// Split from readOne so read errors are testable without file fixtures.
func scanProfile(r io.Reader, path string, mode *string, index map[string]*block, order *[]string) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			if err := checkMode(line, *mode, path); err != nil {
				return err
			}
			*mode = strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
			first = false
			continue
		}
		if line == "" {
			continue
		}
		b, err := parseBlock(line)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		key := b.file + ":" + b.span
		if existing, ok := index[key]; ok {
			if existing.stmts != b.stmts {
				return fmt.Errorf("%s: block %s has %d statements, earlier profile had %d",
					path, key, b.stmts, existing.stmts)
			}
			existing.counts += b.counts
			continue
		}
		index[key] = &b
		*order = append(*order, key)
	}
	if first {
		return fmt.Errorf("%s: empty profile", path)
	}
	return scanner.Err()
}

// checkMode validates the mode header against profiles seen so far.
func checkMode(line, mode, path string) error {
	if !strings.HasPrefix(line, "mode:") {
		return fmt.Errorf("%s: missing mode header", path)
	}
	next := strings.TrimSpace(strings.TrimPrefix(line, "mode:"))
	if mode != "" && next != mode {
		return fmt.Errorf("%s: coverage mode %q differs from %q", path, next, mode)
	}
	return nil
}

// parseBlock parses one "file:span statements count" profile line.
func parseBlock(line string) (block, error) {
	sep := strings.IndexByte(line, ':')
	if sep < 0 {
		return block{}, fmt.Errorf("malformed block %q", line)
	}
	fields := strings.Fields(line[sep+1:])
	if len(fields) != 3 {
		return block{}, fmt.Errorf("malformed block %q", line)
	}
	stmts, err := strconv.Atoi(fields[1])
	if err != nil {
		return block{}, fmt.Errorf("statement count in %q: %w", line, err)
	}
	count, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return block{}, fmt.Errorf("execution count in %q: %w", line, err)
	}
	if stmts < 0 || count < 0 {
		return block{}, fmt.Errorf("invalid counts in %q", line)
	}
	return block{file: line[:sep], span: fields[0], stmts: stmts, counts: count}, nil
}

// writeProfile writes the rendered profile to out with one file write,
// so disk failures surface through a single testable error path.
func writeProfile(out, mode string, blocks []block) error {
	if err := os.WriteFile(out, renderProfile(mode, blocks), 0o600); err != nil {
		return fmt.Errorf("write output profile: %w", err)
	}
	return nil
}

// renderProfile returns the merged profile in canonical `go tool
// cover` text format, sorted by file then span for deterministic
// output. Rendering goes to memory: bytes.Buffer writes never fail.
func renderProfile(mode string, blocks []block) []byte {
	sorted := make([]block, len(blocks))
	copy(sorted, blocks)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].file != sorted[j].file {
			return sorted[i].file < sorted[j].file
		}
		return sorted[i].span < sorted[j].span
	})
	var buf bytes.Buffer
	buf.WriteString("mode: " + mode + "\n")
	for _, b := range sorted {
		buf.WriteString(fmt.Sprintf("%s:%s %d %d\n", b.file, b.span, b.stmts, b.counts))
	}
	return buf.Bytes()
}

// percent returns covered statements over total statements.
func percent(blocks []block) float64 {
	var total, covered int
	for _, b := range blocks {
		total += b.stmts
		if b.counts > 0 {
			covered += b.stmts
		}
	}
	if total == 0 {
		return 0
	}
	return float64(covered) / float64(total) * 100
}
