package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScanFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		fixture  string // path under testdata/scans
		wantKind findingKind
		wantPat  string
	}{
		{"aws access key", "secret/aws-key.txt", kindSecret, "aws-access-key"},
		{"github token", "secret/github.txt", kindSecret, "github-token"},
		{"private key block", "secret/private-key.pem", kindSecret, "private-key"},
		{"credential assignment", "secret/password.txt", kindSecret, "credential-assignment"},
		{"work marker", "placeholder/todo.txt", kindPlaceholder, "todo"},
		{"insert marker", "placeholder/insert.txt", kindPlaceholder, "insert-marker"},
		{"fill-in marker", "placeholder/your-key.txt", kindPlaceholder, "your-placeholder"},
		{"users path", "private/users-path.txt", kindPrivatePath, "users-dir"},
		{"em dash", "dash/em-dash.txt", kindForbiddenDash, "em-dash"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join("testdata", "scans", tt.fixture)
			found, err := scanFile(tt.fixture, path)
			if err != nil {
				t.Fatalf("scanFile: %v", err)
			}
			for _, f := range found {
				if f.Kind == tt.wantKind && f.Pattern == tt.wantPat {
					return
				}
			}
			t.Errorf("scanFile(%s) = %v, want a %s finding with pattern %s",
				tt.fixture, found, tt.wantKind, tt.wantPat)
		})
	}
}

func TestScanFileClean(t *testing.T) {
	t.Parallel()
	path := filepath.Join("testdata", "scans", "clean", "ok.txt")
	found, err := scanFile("clean/ok.txt", path)
	if err != nil {
		t.Fatalf("scanFile: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("scanFile(clean fixture) = %v, want no findings", found)
	}
}

func TestScanFileMissing(t *testing.T) {
	t.Parallel()
	if _, err := scanFile("nope.txt", filepath.Join("testdata", "scans", "nope.txt")); err == nil {
		t.Error("scanFile on a missing file = nil error, want error")
	}
}

func TestScanSkipDir(t *testing.T) {
	t.Parallel()
	tests := []struct {
		rel  string
		want bool
	}{
		{".", false},
		{".git", true},
		{".git/hooks", true},
		{".worktrees", true},
		{".worktrees/feature", true},
		{"dist", true},
		{"node_modules", true},
		{".github", false},
		{".github/workflows", false},
		{".promptscript", false},
		{".promptscript/sub", false},
		{".vscode", true},
		{".code-review-graph", true},
		{"tools/validate/testdata", true},
		{"tools/validate/testdata/scans/secret", true},
		{"tools/validate", false},
		{"internal", false},
		{"cmd/git-byline", false},
	}
	for _, tt := range tests {
		if got := scanSkipDir(tt.rel); got != tt.want {
			t.Errorf("scanSkipDir(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}

// TestScanRepoFindsPlantedFindings walks the fixture tree directly (so the
// skip rules do not apply) and verifies the planted findings are really
// detectable.
func TestScanRepoFindsPlantedFindings(t *testing.T) {
	t.Parallel()
	findings, err := scanRepo(filepath.Join("testdata", "scans"))
	if err != nil {
		t.Fatalf("scanRepo: %v", err)
	}
	if len(findings) == 0 {
		t.Fatal("scanRepo(testdata/scans) found nothing, want the planted findings")
	}
}

// TestCheckScansOnRepo is the integration guard: the real repository must
// pass its own scanners.
func TestCheckScansOnRepo(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if err := checkScans(root); err != nil {
		t.Errorf("checkScans(repo) = %v, want nil", err)
	}
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in    string
		want  string
		limit int
	}{
		{"short", "short", 10},
		{"exactly-10", "exactly-10", 10},
		{"this line is far too long", "this line ...", 10},
		{"zażółć gęślą jaźń", "zażółć ...", 7},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.limit); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.limit, got, tt.want)
		}
	}
}

func TestIsBinary(t *testing.T) {
	t.Parallel()
	if isBinary([]byte("plain text\n")) {
		t.Error("isBinary(plain text) = true, want false")
	}
	if !isBinary([]byte("text\x00with NUL\n")) {
		t.Error("isBinary(text with NUL) = false, want true")
	}
	if isBinary([]byte(strings.Repeat("a", 16384) + "\x00")) {
		t.Error("isBinary(NUL beyond 8 KiB) = true, want false")
	}
}
