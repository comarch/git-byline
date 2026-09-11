package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/provenance"
)

func TestParseColorMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  colorMode
		valid bool
	}{
		{"auto", colorAuto, true},
		{"always", colorAlways, true},
		{"never", colorNever, true},
		{"", "", false},
		{"yes", "", false},
		{"Always", "", false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			got, err := parseColorMode(test.value)
			if (err == nil) != test.valid {
				t.Fatalf("parseColorMode(%q) error = %v, valid = %t", test.value, err, test.valid)
			}
			if test.valid && got != test.want {
				t.Fatalf("parseColorMode(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// TestUseColor is not parallel because it sets NO_COLOR for the process.
func TestUseColor(t *testing.T) {
	buffer := new(bytes.Buffer)
	if useColor(colorNever, buffer) {
		t.Error("never mode enabled color")
	}
	if !useColor(colorAlways, buffer) {
		t.Error("always mode disabled color")
	}
	if useColor(colorAuto, buffer) {
		t.Error("automatic mode enabled color for a buffer")
	}
	// A regular file is not a character device, so automatic mode must
	// stay plain even though the writer is an *os.File.
	path := filepath.Join(t.TempDir(), "out.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if useColor(colorAuto, file) {
		t.Error("automatic mode enabled color for a regular file")
	}
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}
	if useColor(colorAuto, closed) {
		t.Error("automatic mode enabled color for an unusable file")
	}
	t.Setenv("NO_COLOR", "1")
	if useColor(colorAuto, file) {
		t.Error("NO_COLOR did not disable color")
	}
	if !useColor(colorAlways, file) {
		t.Error("NO_COLOR must not override an explicit choice")
	}
}

func TestUseColorRejectsDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if useColor(colorAuto, os.Stdout) {
		t.Error("dumb terminal enabled color")
	}
}

// TestLabelColorUsesThePalette pins the mapping and its fallback. It is
// not parallel because it sets COLORTERM for the process.
func TestLabelColorUsesThePalette(t *testing.T) {
	tests := []struct {
		author model.Author
		exact  string
		basic  string
	}{
		{model.AuthorHuman, trueHuman, basicHuman},
		{model.AuthorHumanOverride, trueOverride, basicOverride},
		{model.AuthorAI, trueAI, basicAI},
		{model.AuthorUntracked, trueUntracked, basicUntracked},
		{model.Author("other"), trueUntracked, basicUntracked},
	}
	t.Setenv("COLORTERM", "truecolor")
	for _, test := range tests {
		if got := labelColor(test.author); got != test.exact {
			t.Errorf("labelColor(%q) = %q, want %q", test.author, got, test.exact)
		}
	}
	t.Setenv("COLORTERM", "")
	for _, test := range tests {
		if got := labelColor(test.author); got != test.basic {
			t.Errorf("labelColor(%q) without truecolor = %q, want %q", test.author, got, test.basic)
		}
	}
	t.Setenv("COLORTERM", "24bit")
	if !trueColor() {
		t.Error("24bit COLORTERM was not treated as 24-bit color")
	}
	t.Setenv("COLORTERM", "256")
	if trueColor() {
		t.Error("256 COLORTERM was treated as 24-bit color")
	}
}

// TestPaletteColorValues keeps the escape sequences aligned with the
// declared hex values.
func TestPaletteColorValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		sequence string
		rgb      string
	}{
		{"cyan #00FFFF", trueAI, "0;255;255"},
		{"violet 200 #B280DF", trueHuman, "178;128;223"},
		{"magenta #FF009B", trueOverride, "255;0;155"},
		{"grey 500 #A6A6A6", trueUntracked, "166;166;166"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			want := "\x1b[38;2;" + test.rgb + "m"
			if test.sequence != want {
				t.Fatalf("sequence = %q, want %q", test.sequence, want)
			}
		})
	}
}

func TestWriteBlameTextAlignsAndColors(t *testing.T) {
	t.Parallel()
	lines := []provenance.BlameLine{
		{Number: 1, Content: "one", Attribution: model.Attribution{
			Author: model.AuthorHuman, Identity: "john.doe",
		}},
		{Number: 2, Content: "two", Attribution: model.Attribution{
			Author:   model.AuthorHumanOverride,
			Identity: "john.doe",
			Agent:    "droid",
			Model:    "a-long-model-name",
		}},
		{Number: 3, Content: "three", Attribution: model.Attribution{Author: model.AuthorUntracked}},
	}
	var plain bytes.Buffer
	writeBlameText(&plain, lines, false)
	rows := strings.Split(strings.TrimRight(plain.String(), "\n"), "\n")
	if len(rows) != 3 {
		t.Fatalf("rows = %d", len(rows))
	}
	separator := strings.Index(rows[0], "|")
	if separator < 0 {
		t.Fatalf("row %q has no separator", rows[0])
	}
	for _, row := range rows {
		if got := strings.Index(row, "|"); got != separator {
			t.Fatalf("row %q separator at %d, want %d", row, got, separator)
		}
	}
	if !strings.HasPrefix(rows[1], "human-override:john.doe/droid/a-long-model-name ") {
		t.Fatalf("widest label was padded: %q", rows[1])
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("plain output contains ANSI sequences")
	}

	var colored bytes.Buffer
	writeBlameText(&colored, lines, true)
	for _, expected := range []string{
		labelColor(model.AuthorHuman),
		labelColor(model.AuthorHumanOverride),
		labelColor(model.AuthorUntracked),
		ansiDim,
		ansiReset,
	} {
		if !strings.Contains(colored.String(), expected) {
			t.Fatalf("colored output missing %q", expected)
		}
	}
}

func TestWriteBlameTextHandlesNoLines(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	writeBlameText(&out, nil, true)
	if out.Len() != 0 {
		t.Fatalf("empty blame wrote %q", out.String())
	}
}

func TestParseBlameFlags(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  []string
		json  bool
		mode  colorMode
		rest  []string
		valid bool
	}{
		{name: "file only", args: []string{"a.go"}, mode: colorAuto, rest: []string{"a.go"}, valid: true},
		{name: "json", args: []string{"--json", "a.go"}, json: true, mode: colorAuto, rest: []string{"a.go"}, valid: true},
		{name: "color never", args: []string{"--color=never", "a.go"}, mode: colorNever, rest: []string{"a.go"}, valid: true},
		{name: "color after file", args: []string{"a.go", "--color=always"}, mode: colorAlways, rest: []string{"a.go"}, valid: true},
		{name: "duplicate json", args: []string{"--json", "--json"}},
		{name: "duplicate color", args: []string{"--color=auto", "--color=never"}},
		{name: "bad color", args: []string{"--color=maybe"}},
		{name: "unknown flag", args: []string{"--nope"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			jsonOutput, mode, rest, err := parseBlameFlags(test.args)
			if (err == nil) != test.valid {
				t.Fatalf("parseBlameFlags(%v) error = %v, valid = %t", test.args, err, test.valid)
			}
			if !test.valid {
				return
			}
			if jsonOutput != test.json || mode != test.mode || strings.Join(rest, ",") != strings.Join(test.rest, ",") {
				t.Fatalf("parseBlameFlags(%v) = %t, %q, %v", test.args, jsonOutput, mode, rest)
			}
		})
	}
	if _, _, _, err := parseBlameFlags([]string{"--help"}); err == nil {
		t.Fatal("parseBlameFlags(--help) returned no error")
	}
}

func TestBlameCommandShowsIdentityAndRespectsColorFlag(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.go", "one\ntwo\n")
	appCommit(t, root, "two lines")
	head := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(head, "file.go")
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.go": {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{
						Author: model.AuthorHuman, Identity: "john.doe",
					}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author:   model.AuthorHumanOverride,
						Identity: "john.doe",
						Agent:    "droid",
						Model:    "model-a",
					}},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		t.Fatal(err)
	}

	code, stdout, stderr, err := appRun(root, zeroTime(), nil, "blame", "file.go")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	for _, want := range []string{
		"human:john.doe",
		"human-override:john.doe/droid/model-a",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("blame output = %q, want %q", stdout, want)
		}
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Error("blame wrote ANSI sequences to a buffer")
	}

	code, colored, stderr, err := appRun(root, zeroTime(), nil, "blame", "--color=always", "file.go")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame --color=always = %d, %q, %q, %v", code, colored, stderr, err)
	}
	if !strings.Contains(colored, labelColor(model.AuthorHuman)) ||
		!strings.Contains(colored, labelColor(model.AuthorHumanOverride)) {
		t.Errorf("colored blame output = %q", colored)
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil, "blame", "--color=maybe", "file.go")
	if code != ExitUsage || err == nil || !strings.Contains(stderr, "--color must be") {
		t.Fatalf("blame --color=maybe = %d, %q, %q, %v", code, stdout, stderr, err)
	}

	code, stdout, stderr, err = appRun(root, zeroTime(), nil, "blame", "--json", "file.go")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("blame --json = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	if !strings.Contains(stdout, `"identity":"john.doe"`) {
		t.Errorf("blame JSON = %q", stdout)
	}
}
