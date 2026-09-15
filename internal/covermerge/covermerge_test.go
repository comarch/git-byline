package covermerge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProfileFile writes a raw profile for test setup.
func writeProfileFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// readProfileFile reads a profile back for assertions.
func readProfileFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestMergeFilesMergesCountsAndOrdersDeterministically(t *testing.T) {
	dir := t.TempDir()
	unit := writeProfileFile(t, dir, "unit.out",
		"mode: count\n"+
			"github.com/comarch/git-byline/internal/store/store.go:10.2,11.10 2 0\n"+
			"github.com/comarch/git-byline/cmd/git-byline/main.go:13.2,16.20 4 0\n")
	child := writeProfileFile(t, dir, "child.out",
		"mode: count\n"+
			"github.com/comarch/git-byline/cmd/git-byline/main.go:13.2,16.20 4 3\n"+
			"github.com/comarch/git-byline/internal/app/app.go:5.2,6.8 1 1\n")
	out := filepath.Join(dir, "merged.out")

	covered, count, err := MergeFiles([]string{unit, child}, out)
	if err != nil {
		t.Fatalf("MergeFiles() = %v, want nil", err)
	}
	if count != 3 {
		t.Fatalf("block count = %d, want 3", count)
	}
	if want := 500.0 / 7.0; covered != want {
		t.Fatalf("covered = %v, want %v", covered, want)
	}
	want := "mode: count\n" +
		"github.com/comarch/git-byline/cmd/git-byline/main.go:13.2,16.20 4 3\n" +
		"github.com/comarch/git-byline/internal/app/app.go:5.2,6.8 1 1\n" +
		"github.com/comarch/git-byline/internal/store/store.go:10.2,11.10 2 0\n"
	if got := readProfileFile(t, out); got != want {
		t.Fatalf("merged profile =\n%q\nwant\n%q", got, want)
	}
}

func TestMergeFilesSumCountsSameBlockAcrossProfiles(t *testing.T) {
	dir := t.TempDir()
	a := writeProfileFile(t, dir, "a.out",
		"mode: count\nmain.go:1.2,2.10 1 2\n")
	b := writeProfileFile(t, dir, "b.out",
		"mode: count\nmain.go:1.2,2.10 1 3\n")
	out := filepath.Join(dir, "merged.out")

	if _, _, err := MergeFiles([]string{a, b}, out); err != nil {
		t.Fatalf("MergeFiles() = %v, want nil", err)
	}
	if got := readProfileFile(t, out); !strings.Contains(got, "main.go:1.2,2.10 1 5\n") {
		t.Fatalf("merged counts not summed: %q", got)
	}
}

// TestMergeFilesAcceptsZeroStatementBlocks pins that synthetic
// zero-statement blocks (Go emits them for empty regions) merge like
// any other block instead of failing validation.
func TestMergeFilesAcceptsZeroStatementBlocks(t *testing.T) {
	dir := t.TempDir()
	a := writeProfileFile(t, dir, "a.out",
		"mode: count\nrewrite.go:214.11,214.11 0 1\n")
	b := writeProfileFile(t, dir, "b.out",
		"mode: count\nrewrite.go:214.11,214.11 0 1\n")
	out := filepath.Join(dir, "merged.out")

	if _, _, err := MergeFiles([]string{a, b}, out); err != nil {
		t.Fatalf("MergeFiles() = %v, want nil", err)
	}
	if got := readProfileFile(t, out); !strings.Contains(got, "rewrite.go:214.11,214.11 0 2\n") {
		t.Fatalf("zero-statement block not merged: %q", got)
	}
}

func TestMergeFilesRejectsInconsistentInput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "merged.out")
	cases := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "statement count mismatch",
			a:    "mode: count\nmain.go:1.2,2.10 1 1\n",
			b:    "mode: count\nmain.go:1.2,2.10 2 1\n",
		},
		{
			name: "mode mismatch",
			a:    "mode: count\nmain.go:1.2,2.10 1 1\n",
			b:    "mode: set\nmain.go:1.2,2.10 1 1\n",
		},
		{
			name: "missing mode header",
			a:    "mode: count\nmain.go:1.2,2.10 1 1\n",
			b:    "main.go:1.2,2.10 1 1\n",
		},
		{
			name: "empty profile",
			a:    "mode: count\nmain.go:1.2,2.10 1 1\n",
			b:    "",
		},
		{
			name: "malformed block",
			a:    "mode: count\nmain.go:1.2,2.10 1 1\n",
			b:    "mode: count\nnot-a-block\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := writeProfileFile(t, t.TempDir(), "a.out", tc.a)
			b := writeProfileFile(t, t.TempDir(), "b.out", tc.b)
			if _, _, err := MergeFiles([]string{a, b}, out); err == nil {
				t.Fatalf("MergeFiles() = nil error, want failure")
			}
		})
	}
}

func TestMergeFilesRejectsMissingInputs(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := MergeFiles(nil, filepath.Join(dir, "merged.out")); err == nil {
		t.Fatal("MergeFiles(nil) = nil error, want failure")
	}
	missing := filepath.Join(t.TempDir(), "missing.out")
	if _, _, err := MergeFiles([]string{missing}, filepath.Join(dir, "merged.out")); err == nil {
		t.Fatal("MergeFiles(missing input) = nil error, want failure")
	}
	unwritable := filepath.Join(dir, "missing-dir", "merged.out")
	profile := writeProfileFile(t, dir, "unit.out", "mode: count\nmain.go:1.2,2.10 1 1\n")
	if _, _, err := MergeFiles([]string{profile}, unwritable); err == nil {
		t.Fatal("MergeFiles(unwritable output) = nil error, want failure")
	}
}

func TestScanProfileSkipsBlankLines(t *testing.T) {
	content := "mode: count\n\nmain.go:1.2,2.10 1 1\n\n"
	var mode string
	index := make(map[string]*block)
	var order []string
	if err := scanProfile(strings.NewReader(content), "unit.out", &mode, index, &order); err != nil {
		t.Fatalf("scanProfile() = %v, want nil", err)
	}
	if len(order) != 1 {
		t.Fatalf("order = %v, want one block", order)
	}
}

func TestScanProfileReportsReadError(t *testing.T) {
	var mode string
	index := make(map[string]*block)
	var order []string
	if err := scanProfile(errReader{err: errors.New("disk gone")}, "unit.out", &mode, index, &order); err == nil {
		t.Fatal("scanProfile() = nil error, want read failure")
	}
}

// errReader always fails, simulating profile reads hitting I/O errors.
type errReader struct {
	err error
}

func (r errReader) Read([]byte) (int, error) {
	return 0, r.err
}

func TestWriteProfileReportsWriteError(t *testing.T) {
	if err := writeProfile(t.TempDir(), "count", nil); err == nil {
		t.Fatal("writeProfile() to directory = nil error, want write failure")
	}
}

func TestParseBlock(t *testing.T) {
	cases := []struct {
		name string
		line string
		want block
	}{
		{
			name: "valid",
			line: "main.go:13.2,15.16 3 2",
			want: block{file: "main.go", span: "13.2,15.16", stmts: 3, counts: 2},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseBlock(tc.line)
			if err != nil {
				t.Fatalf("parseBlock(%q) = %v", tc.line, err)
			}
			if got != tc.want {
				t.Fatalf("parseBlock(%q) = %+v, want %+v", tc.line, got, tc.want)
			}
		})
	}
	invalid := []struct {
		name string
		line string
	}{
		{name: "missing file separator", line: "main.go 13.2,15.16 3 2"},
		{name: "too few fields", line: "main.go:13.2,15.16 3"},
		{name: "too many fields", line: "main.go:13.2,15.16 3 2 1"},
		{name: "statement count not numeric", line: "main.go:13.2,15.16 three 2"},
		{name: "execution count not numeric", line: "main.go:13.2,15.16 3 two"},
		{name: "negative statements", line: "main.go:13.2,15.16 -1 2"},
		{name: "negative count", line: "main.go:13.2,15.16 3 -1"},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseBlock(tc.line); err == nil {
				t.Fatalf("parseBlock(%q) = nil error, want failure", tc.line)
			}
		})
	}
}

func TestRenderProfileOrdersSameFileBySpan(t *testing.T) {
	blocks := []block{
		{file: "main.go", span: "9.2,10.4", stmts: 1, counts: 1},
		{file: "main.go", span: "1.2,2.4", stmts: 1, counts: 0},
	}
	got := string(renderProfile("count", blocks))
	want := "mode: count\nmain.go:1.2,2.4 1 0\nmain.go:9.2,10.4 1 1\n"
	if got != want {
		t.Fatalf("renderProfile() =\n%q\nwant\n%q", got, want)
	}
}

func TestPercent(t *testing.T) {
	cases := []struct {
		name   string
		blocks []block
		want   float64
	}{
		{name: "nil", blocks: nil, want: 0},
		{
			name:   "all covered",
			blocks: []block{{span: "a", stmts: 2, counts: 1}, {span: "b", stmts: 1, counts: 5}},
			want:   100,
		},
		{
			name:   "partial",
			blocks: []block{{span: "a", stmts: 3, counts: 0}, {span: "b", stmts: 1, counts: 2}},
			want:   25,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := percent(tc.blocks); got != tc.want {
				t.Fatalf("percent() = %v, want %v", got, tc.want)
			}
		})
	}
}
