// scan.go implements the repository scanners: leaked credentials,
// unfinished-work markers, private paths, and forbidden characters.
//
// The scanners are pure functions over file contents, so tests pin their
// behavior with fixtures. The rule patterns below are split into two
// string halves so the scanner source does not contain the pattern text
// verbatim; otherwise the repository scan would flag its own rules.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// findingKind categorizes a scanner hit.
type findingKind string

const (
	kindSecret        findingKind = "secret"
	kindPlaceholder   findingKind = "placeholder"
	kindPrivatePath   findingKind = "private-path"
	kindForbiddenDash findingKind = "forbidden-dash"
)

// finding is one scanner hit.
type finding struct {
	Kind    findingKind
	Pattern string
	File    string
	Line    int
	Text    string
}

// scanRule pairs a rule name with its regular expression.
type scanRule struct {
	name    string
	pattern *regexp.Regexp
}

// ruleSet groups rules under one finding kind.
type ruleSet struct {
	kind  findingKind
	rules []scanRule
}

// scanRuleSets lists all scanner rules in evaluation order.
var scanRuleSets = []ruleSet{
	{kind: kindSecret, rules: []scanRule{
		{"aws-access-key", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
		{"github-token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36}\b`)},
		{"github-fine-token", regexp.MustCompile(`\bgithub` + `_pat_[A-Za-z0-9_]{20,}\b`)},
		{"gitlab-token", regexp.MustCompile(`\bgl` + `pat-[A-Za-z0-9_-]{20,}\b`)},
		{"slack-token", regexp.MustCompile(`\bxo` + `x[baprs]-[A-Za-z0-9-]{10,}\b`)},
		{"private-key", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
		{"credential-assignment", regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key)\b\s*[:=]\s*["'][^"']{8,}["']`)},
	}},
	{kind: kindPlaceholder, rules: []scanRule{
		{"todo", regexp.MustCompile(`\bTO` + `DO\b`)},
		{"fixme", regexp.MustCompile(`\bFIX` + `ME\b`)},
		{"xxx", regexp.MustCompile(`\bXX` + `X\b`)},
		{"tbd", regexp.MustCompile(`\bT` + `BD\b`)},
		{"changeme", regexp.MustCompile(`\bCHANGE` + `ME\b`)},
		{"lorem-ipsum", regexp.MustCompile(`(?i)lorem` + ` ipsum`)},
		{"insert-marker", regexp.MustCompile(`\[INS` + `ERT\b`)},
		{"your-placeholder", regexp.MustCompile(`(?i)(<your` + `-[a-z-]+>|YOUR_` + `[A-Z_]+_HERE)`)},
	}},
	{kind: kindPrivatePath, rules: []scanRule{
		{"users-dir", regexp.MustCompile(`/Use` + `rs/`)},
		{"home-dir", regexp.MustCompile(`/ho` + `me/`)},
		{"windows-users", regexp.MustCompile(`C:\\Use` + `rs\\`)},
		{"mac-tmp", regexp.MustCompile(`/var/fold` + `ers/`)},
	}},
	{kind: kindForbiddenDash, rules: []scanRule{
		{"em-dash", regexp.MustCompile("\u2014")},
		{"en-dash", regexp.MustCompile("\u2013")},
	}},
}

// maxScanBytes is the size above which files are skipped: large files are
// almost always generated data, not the source the scan is meant for.
const maxScanBytes = 2 << 20 // 2 MiB

// testDataDir is the scanner fixture directory, excluded from the
// repository scan because its files carry the flagged patterns on
// purpose.
const testDataDir = "tools/validate/testdata"

// scanRepo walks the repository rooted at root and scans every text
// file. It stays out of VCS directories, local caches, build output, and
// the scanner fixtures, which carry the flagged patterns on purpose.
func scanRepo(root string) ([]finding, error) {
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			if scanSkipDir(rel) {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		if scanSkipFile(rel, info) {
			return nil
		}
		found, err := scanFile(rel, path)
		if err != nil {
			return err
		}
		findings = append(findings, found...)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	return findings, nil
}

// scanSkipDir reports whether the repository scan stays out of the
// directory at rel, the path relative to the repository root. Hidden
// directories at the repository root are skipped except generated agent
// configuration and PromptScript source; local tool caches stay out of the
// scan.
func scanSkipDir(rel string) bool {
	rel = path.Clean(strings.ReplaceAll(filepath.ToSlash(rel), `\`, "/"))
	if rel == "." {
		return false
	}
	switch rel {
	case ".git", ".worktrees", "dist", "node_modules":
		return true
	}
	if rel == testDataDir || strings.HasPrefix(rel, testDataDir+"/") {
		return true // scanner fixtures: they carry the flagged patterns
	}
	rootDir, _, _ := strings.Cut(rel, "/")
	if strings.HasPrefix(rootDir, ".") && !slices.Contains(agentConfigDirs, rootDir) {
		return true
	}
	return false
}

var agentConfigDirs = []string{
	".claude",
	".claude-plugin",
	".codex",
	".cursor",
	".factory",
	".factory-plugin",
	".gemini",
	".github",
	".gitlab",
	".grok",
	".promptscript",
	".windsurf",
}

// scanSkipFile reports whether the file at rel is not scanned.
func scanSkipFile(rel string, info fs.FileInfo) bool {
	rel = path.Clean(strings.ReplaceAll(filepath.ToSlash(rel), `\`, "/"))
	// A .git regular file is a linked-worktree pointer: VCS metadata,
	// never project source.
	if rel == ".git" {
		return true
	}
	if !info.Mode().IsRegular() {
		return true
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".test", ".out", ".exe", ".bin", ".png", ".jpg", ".jpeg", ".gif",
		".ico", ".pdf", ".zip", ".gz", ".tar", ".dylib", ".a":
		return true
	}
	return info.Size() > maxScanBytes
}

// scanFile scans one file and reports findings with paths relative to
// the repository root. Binary files are skipped.
func scanFile(rel, path string) ([]finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	if isBinary(data) {
		return nil, nil
	}
	var found []finding
	for i, line := range strings.Split(string(data), "\n") {
		for _, set := range scanRuleSets {
			for _, rule := range set.rules {
				if rule.pattern.MatchString(line) {
					found = append(found, finding{
						Kind:    set.kind,
						Pattern: rule.name,
						File:    rel,
						Line:    i + 1,
						Text:    truncate(strings.TrimSuffix(line, "\r"), 120),
					})
				}
			}
		}
	}
	return found, nil
}

// truncate shortens text to at most limit characters for display. It
// counts runes, not bytes, so a cut never splits a multi-byte character.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}

// isBinary reports whether data looks like binary rather than text: it
// contains a NUL byte in the first 8 KiB.
func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 8192 {
		limit = 8192
	}
	return bytes.IndexByte(data[:limit], 0) >= 0
}

// checkScans runs all repository scanners and fails on the first hit.
func checkScans(root string) error {
	ciTemplates := filepath.Join(root, "internal", "ci", "templates")
	if info, err := os.Stat(ciTemplates); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("CI template path is not a directory")
		}
		if err := checkCITemplates(root); err != nil {
			return fmt.Errorf("CI template contract: %w", err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect CI template directory: %w", err)
	}
	findings, err := scanRepo(root)
	if err != nil {
		return fmt.Errorf("scan: %w", err)
	}
	if len(findings) == 0 {
		return nil
	}
	var b strings.Builder
	for _, f := range findings {
		text := f.Text
		if f.Kind == kindSecret || f.Kind == kindPrivatePath {
			text = "[redacted]"
		}
		fmt.Fprintf(&b, "  %s %s:%d [%s] %s\n", f.Kind, f.File, f.Line, f.Pattern, text)
	}
	return fmt.Errorf("%d finding(s) in repository scan:\n%s", len(findings), b.String())
}
