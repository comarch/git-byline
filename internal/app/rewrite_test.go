package app

import (
	"strings"
	"testing"
	"time"
)

var timeZero time.Time

func TestRewriteCommandUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing mode", args: []string{"rewrite", "--hook-input", "stdin"}},
		{name: "bad mode", args: []string{"rewrite", "--mode", "other", "--hook-input", "stdin"}},
		{name: "bad input", args: []string{"rewrite", "--mode", "ref-txn", "--hook-input", "file"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			code, _, stderr, err := appRun(t.TempDir(), timeZero, nil, test.args...)
			if code != ExitUsage || err == nil || !strings.Contains(stderr, "git-byline rewrite") {
				t.Fatalf("Run(%v) = %d, %q, %v", test.args, code, stderr, err)
			}
		})
	}
}

func TestRewriteReferenceTransactionNestedReturnsImmediately(t *testing.T) {
	t.Setenv("GIT_BYLINE_NESTED", "1")
	code, stdout, stderr, err := appRun(t.TempDir(), timeZero, strings.NewReader("bad\n"),
		"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed")
	if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
		t.Fatalf("nested rewrite = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestRewriteCommandHookModes(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "content\n")
	appCommit(t, root, "base")
	head := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	tests := [][]string{
		{"rewrite", "--mode", "post-rewrite", "--hook-input", "stdin", "rebase"},
		{"rewrite", "--mode", "post-checkout", "--hook-input", "stdin", head, head, "0"},
		{"rewrite", "--mode", "post-merge", "--hook-input", "stdin", "0"},
		{"rewrite", "--mode", "ref-txn", "--hook-input", "stdin", "committed"},
	}
	for _, args := range tests {
		args := args
		t.Run(args[2], func(t *testing.T) {
			input := strings.NewReader("")
			if args[1] == "--mode" && args[2] == "ref-txn" {
				input = strings.NewReader(
					strings.Repeat("0", 40) + " " +
						strings.Repeat("1", 40) + " refs/tags/example\n",
				)
			}
			code, stdout, stderr, err := appRun(root, timeZero, input, args...)
			if code != ExitSuccess || err != nil || stdout != "" || stderr != "" {
				t.Fatalf("Run(%v) = %d, %q, %q, %v", args, code, stdout, stderr, err)
			}
		})
	}
}
