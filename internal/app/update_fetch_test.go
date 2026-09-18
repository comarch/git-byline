package app

import (
	"bytes"
	"strings"
	"testing"
)

// TestReleaseArchiveNameFor verifies the release archive naming for every
// supported platform.
func TestReleaseArchiveNameFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tag    string
		goos   string
		goarch string
		want   string
	}{
		{tag: "v1.2.0", goos: "darwin", goarch: "amd64", want: "git-byline_1.2.0_macOS_amd64.tar.gz"},
		{tag: "v1.2.0", goos: "darwin", goarch: "arm64", want: "git-byline_1.2.0_macOS_arm64.tar.gz"},
		{tag: "v1.2.0", goos: "linux", goarch: "amd64", want: "git-byline_1.2.0_linux_amd64.tar.gz"},
		{tag: "v1.2.0", goos: "linux", goarch: "arm64", want: "git-byline_1.2.0_linux_arm64.tar.gz"},
		{tag: "v1.2.0", goos: "windows", goarch: "amd64", want: "git-byline_1.2.0_windows_amd64.zip"},
		{tag: "v1.2.0", goos: "windows", goarch: "arm64", want: "git-byline_1.2.0_windows_arm64.zip"},
	}
	for _, c := range cases {
		if got := releaseArchiveNameFor(c.tag, c.goos, c.goarch); got != c.want {
			t.Fatalf("releaseArchiveNameFor(%s, %s, %s) = %q, want %q", c.tag, c.goos, c.goarch, got, c.want)
		}
	}
}

// TestRunFetchUpdateRejectsInvalidTag verifies that a malformed --version
// fails as a usage error before any network access happens.
func TestRunFetchUpdateRejectsInvalidTag(t *testing.T) {
	t.Parallel()
	env := testEnv(&bytes.Buffer{})
	code, err := runFetchUpdate(env, &command{name: "update"}, "/target", "1.2.0", false, false)
	if code != ExitUsage || err == nil {
		t.Fatalf("runFetchUpdate(invalid tag) = %d, %v; want usage error", code, err)
	}
	if !strings.Contains(env.Stderr.(*bytes.Buffer).String(), "unsupported release version: 1.2.0") {
		t.Fatalf("stderr = %q, want unsupported version", env.Stderr.(*bytes.Buffer).String())
	}
}

// TestRunFetchUpdateMissingCurl verifies the failure when curl cannot be
// resolved: an operational error, not a crash.
func TestRunFetchUpdateMissingCurl(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stderr bytes.Buffer
	env := testEnv(&bytes.Buffer{})
	env.Stderr = &stderr
	code, err := runFetchUpdate(env, &command{name: "update"}, "/target", "v9.9.9", false, false)
	if code != ExitFailure || err == nil {
		t.Fatalf("runFetchUpdate(missing curl) = %d, %v; want failure", code, err)
	}
	if !strings.Contains(stderr.String(), "update needs curl") {
		t.Fatalf("stderr = %q, want needs-curl error", stderr.String())
	}
}
