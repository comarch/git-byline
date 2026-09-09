package main

import (
	"os"
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

func TestScanSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, content, pattern string
	}{
		{"aws access key", strings.Join([]string{"AK", "IA", strings.Repeat("A", 16)}, ""), "aws-access-key"},
		{"github token", "gh" + "p_" + strings.Repeat("a", 36), "github-token"},
		{"github fine token", "github" + "_pat_" + strings.Repeat("a", 24), "github-fine-token"},
		{"gitlab token", "gl" + "pat-" + strings.Repeat("a", 24), "gitlab-token"},
		{"slack token", "xo" + "xb-" + strings.Repeat("a", 24), "slack-token"},
		{"private key block", "-----BEGIN RSA " + "PRIVATE KEY-----", "private-key"},
		{"credential assignment", "pass" + `word = "correct-horse"`, "credential-assignment"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "input.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}
			found, err := scanFile("input.txt", path)
			if err != nil {
				t.Fatalf("scanFile: %v", err)
			}
			for _, finding := range found {
				if finding.Kind == kindSecret && finding.Pattern == tt.pattern {
					return
				}
			}
			t.Errorf("scanFile() = %v, want secret pattern %s", found, tt.pattern)
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
		{".factory", false},
		{".factory/droids", false},
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

func TestScanSkipFileSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if !scanSkipFile("link", info) {
		t.Fatal("scanSkipFile accepted a symlink")
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

func TestCheckScansRedactsSensitiveFindings(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"secret":       "gh" + "p_" + strings.Repeat("a", 36),
		"private path": "/" + "Users/alice/project",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "sensitive.txt"), []byte(content+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			err := checkScans(root)
			if err == nil {
				t.Fatal("checkScans accepted sensitive content")
			}
			if strings.Contains(err.Error(), content) || !strings.Contains(err.Error(), "[redacted]") {
				t.Fatalf("checkScans did not redact its finding: %v", err)
			}
		})
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
