package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testFetchLimit int64 = 1 << 20

// writeScript installs a POSIX shell script at path and returns its
// directory for PATH injection.
func writeScript(t *testing.T, path, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell scripts are not executable on Windows")
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(path)
}

// TestNewResolvesExplicitCurl verifies that an explicit curl path is
// used verbatim, without a PATH lookup.
func TestNewResolvesExplicitCurl(t *testing.T) {
	r, err := New("/opt/curl")
	if err != nil {
		t.Fatal(err)
	}
	if r.curlPath != "/opt/curl" {
		t.Fatalf("curl = %q, want /opt/curl", r.curlPath)
	}
}

// TestNewMissingCurl verifies that update fails with a clear error when
// curl is not installed.
func TestNewMissingCurl(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	if _, err := New(""); err == nil || !strings.Contains(err.Error(), "update needs curl") {
		t.Fatalf("New(missing curl) = %v, want needs-curl error", err)
	}
}

// TestFetchWritesArgumentsAndFile verifies the HTTPS-only curl flags and
// the destination handling of one fetch.
func TestFetchWritesArgumentsAndFile(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "args.log")
	fixture := filepath.Join(dir, "payload")
	if err := os.WriteFile(fixture, []byte("release bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, strings.Join([]string{
		`echo "$@" > "$FAKE_LOG"`,
		`for arg in "$@"; do url="$arg"; done`,
		`cat "$FAKE_DIR/${url##*/}"`,
	}, "\n"))
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_DIR", dir)
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "downloaded")
	if err := r.Fetch("https://example.com/payload", dest, testFetchLimit); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "release bytes\n" {
		t.Fatalf("downloaded = %q, want release bytes", got)
	}
	args, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "-q -fsSL --proto =https --proto-redir =https --tlsv1.2 https://example.com/payload"
	if string(args) != want+"\n" {
		t.Fatalf("curl args = %q, want %q", args, want)
	}
}

func TestFetchRejectsOversizedDownload(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `printf 'oversized'`)
	r, err := New(bin)
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "downloaded")
	err = r.Fetch("https://example.com/payload", dest, 4)
	if err == nil || !strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("Fetch(oversized) = %v, want size-limit error", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("downloaded file remains after failure: %v", err)
	}
}

// TestFetchRejectsInvalidLimit verifies that a broken caller cannot start
// an effectively unbounded download.
func TestFetchRejectsInvalidLimit(t *testing.T) {
	r, err := New("/opt/curl")
	if err != nil {
		t.Fatal(err)
	}
	err = r.Fetch("https://example.com/payload", filepath.Join(t.TempDir(), "downloaded"), -1)
	if err == nil || !strings.Contains(err.Error(), "invalid size limit") {
		t.Fatalf("Fetch(negative limit) = %v, want invalid-limit error", err)
	}
}

// TestFetchRefusesExistingDestination verifies that a failed download setup
// cannot replace a caller-owned file.
func TestFetchRefusesExistingDestination(t *testing.T) {
	r, err := New("/opt/curl")
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "downloaded")
	if err := os.WriteFile(dest, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = r.Fetch("https://example.com/payload", dest, testFetchLimit)
	if err == nil || !strings.Contains(err.Error(), "create destination") {
		t.Fatalf("Fetch(existing destination) = %v, want create error", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "keep" {
		t.Fatalf("destination = %q, want original content", data)
	}
}

// TestFetchRejectsNonHTTPS verifies that a URL outside https cannot
// reach curl, so it can never parse as an option.
func TestFetchRejectsNonHTTPS(t *testing.T) {
	r, err := New("/opt/curl")
	if err != nil {
		t.Fatal(err)
	}
	for _, url := range []string{"http://example.com/a", "file:///etc/passwd", "-fsSL"} {
		if err := r.Fetch(url, t.TempDir()+"/dest", testFetchLimit); err == nil ||
			!strings.Contains(err.Error(), "only https URLs are allowed") {
			t.Fatalf("Fetch(%q) = %v, want https-only error", url, err)
		}
	}
}

// TestRunTruncatesCapture verifies that subprocess output beyond the
// capture cap is discarded instead of growing memory.
func TestRunTruncatesCapture(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "noisy")
	writeScript(t, bin, `yes hello | head -c 2097153`)
	out, _, err := Run(bin)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != outputMax {
		t.Fatalf("captured %d bytes, want %d", len(out), outputMax)
	}
}

// TestFetchReportsCurlFailure verifies that a curl exit code and its
// stderr land in the error.
func TestFetchReportsCurlFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `echo "curl: (56) connection reset" >&2; exit 56`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	err = r.Fetch("https://example.com/archive.tar.gz", filepath.Join(dir, "dest"), testFetchLimit)
	if err == nil ||
		!strings.Contains(err.Error(), "fetch https://example.com/archive.tar.gz") ||
		!strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Fetch(curl failure) = %v, want operation and stderr", err)
	}
}

// TestLatestTagParsesRedirect verifies the release-page redirect probe
// and the resulting tag validation.
func TestLatestTagParsesRedirect(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `echo "https://github.com/comarch/git-byline/releases/tag/v9.9.9"`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	tag, err := r.LatestTag("https://github.com/comarch/git-byline")
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v9.9.9" {
		t.Fatalf("LatestTag = %q, want v9.9.9", tag)
	}
}

// TestLatestTagRejectsUnexpectedRedirect verifies that a redirect
// outside the release tag shape fails instead of steering later URLs.
func TestLatestTagRejectsUnexpectedRedirect(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `echo "https://github.com/comarch/git-byline/releases/latest"`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.LatestTag("https://github.com/comarch/git-byline")
	if err == nil || !strings.Contains(err.Error(), `unexpected latest release tag "latest"`) {
		t.Fatalf("LatestTag(no redirect) = %v, want unexpected tag", err)
	}
}

// TestRunCapturesStdoutAndFailure verifies self-execution of the staged
// or installed binary: stdout on success, exit status and stderr in the
// error on failure.
func TestRunCapturesStdoutAndFailure(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	writeScript(t, good, `echo "git-byline v9.9.9"`)
	out, _, err := Run(good, "version")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "git-byline v9.9.9" {
		t.Fatalf("Run stdout = %q, want git-byline v9.9.9", out)
	}
	bad := filepath.Join(dir, "bad")
	writeScript(t, bad, `echo "boom" >&2; exit 3`)
	_, _, err = Run(bad, "annotate")
	if err == nil ||
		!strings.Contains(err.Error(), "exit status 3") ||
		!strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run(failure) = %v, want status and stderr", err)
	}
}

// TestRunMissingPath verifies the error for a binary that cannot run.
func TestRunMissingPath(t *testing.T) {
	_, _, err := Run(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("Run(missing path) = nil, want error")
	}
	if !strings.Contains(err.Error(), "run ") {
		t.Fatalf("Run error = %v, want run prefix", err)
	}
}

// TestDetectByCommandConfigDirAndNone verifies the installer's agent
// detection rule: command on PATH, or configuration directory under
// home. HOME and USERPROFILE are both set because os.UserHomeDir reads
// USERPROFILE on Windows.
func TestDetectByCommandConfigDirAndNone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if !Detect("git", ".definitely-not-here") {
		t.Fatal("Detect(existing command) = false, want true")
	}
	if err := os.MkdirAll(filepath.Join(home, ".gemini"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !Detect("no-such-command-anywhere", ".gemini") {
		t.Fatal("Detect(config dir) = false, want true")
	}
	if Detect("no-such-command-anywhere", ".nothing-here") {
		t.Fatal("Detect(nothing) = true, want false")
	}
}

// TestFetchTimesOut verifies that a download exceeding its deadline
// fails with the context timeout sentinel, without waiting for the
// production timeout.
func TestFetchTimesOut(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `sleep 5`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runner{curlPath: bin, probeTimeout: 50 * time.Millisecond, fetchTimeout: 50 * time.Millisecond}
	start := time.Now()
	err := r.Fetch("https://example.com/archive.tar.gz", filepath.Join(dir, "dest"), testFetchLimit)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Fetch(deadline) = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > waitGrace+2*time.Second {
		t.Fatalf("Fetch returned after %v, want it bounded by the deadline plus waitGrace", elapsed)
	}
}

// TestLatestTagTimesOut verifies the probe deadline the same way.
func TestLatestTagTimesOut(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `sleep 5`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r := &Runner{curlPath: bin, probeTimeout: 50 * time.Millisecond, fetchTimeout: 50 * time.Millisecond}
	start := time.Now()
	_, err := r.LatestTag("https://github.com/comarch/git-byline")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LatestTag(deadline) = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > waitGrace+2*time.Second {
		t.Fatalf("LatestTag returned after %v, want it bounded by the deadline plus waitGrace", elapsed)
	}
}

// TestRunTimesOut verifies the self-exec deadline through the injectable
// timeout seam.
func TestRunTimesOut(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "slow")
	writeScript(t, bin, `sleep 5`)
	start := time.Now()
	_, _, err := runBinary(50*time.Millisecond, bin, "version")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runBinary(deadline) = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > waitGrace+2*time.Second {
		t.Fatalf("runBinary returned after %v, want it bounded by the deadline plus waitGrace", elapsed)
	}
}

// TestRunTimesOutWithPipeHoldingDescendant verifies that a descendant
// keeping the output pipe open cannot block the caller past the
// deadline: WaitDelay bounds the wait and the timeout sentinel stays
// reachable.
func TestRunTimesOutWithPipeHoldingDescendant(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "slow")
	writeScript(t, bin, "sleep 5 &\nsleep 5")
	start := time.Now()
	_, _, err := runBinary(50*time.Millisecond, bin, "version")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runBinary(deadline, pipe holder) = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > waitGrace+2*time.Second {
		t.Fatalf("runBinary returned after %v, want it bounded by the deadline plus waitGrace", elapsed)
	}
}

// TestLatestTagEmptyRedirect verifies the failure when the probe answers
// without a redirect URL.
func TestLatestTagEmptyRedirect(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `echo ""`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.LatestTag("https://github.com/comarch/git-byline")
	if err == nil || !strings.Contains(err.Error(), "resolve latest release: no redirect URL") {
		t.Fatalf("LatestTag(empty redirect) = %v, want no-redirect error", err)
	}
}

// TestFetchReportsCurlFailureWithoutStderr verifies the error shape when
// curl fails with no stderr at all.
func TestFetchReportsCurlFailureWithoutStderr(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	writeScript(t, bin, `exit 56`)
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	r, err := New("")
	if err != nil {
		t.Fatal(err)
	}
	err = r.Fetch("https://example.com/archive.tar.gz", filepath.Join(dir, "dest"), testFetchLimit)
	if err == nil ||
		!strings.Contains(err.Error(), "fetch https://example.com/archive.tar.gz: curl: exit status 56") {
		t.Fatalf("Fetch(silent failure) = %v, want operation and status", err)
	}
}

// TestRunTruncatesStderrExcerpt verifies that a large subprocess stderr is
// trimmed in the formatted error.
func TestRunTruncatesStderrExcerpt(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "noisy-failure")
	writeScript(t, bin, `yes boom | head -c 20000 >&2; exit 3`)
	_, _, err := Run(bin, "version")
	if err == nil {
		t.Fatal("Run(noisy failure) = nil, want error")
	}
	if !strings.HasSuffix(err.Error(), "...") {
		t.Fatalf("error tail = %q, want the truncation marker", err.Error()[len(err.Error())-20:])
	}
	if len(err.Error()) > outputMax {
		t.Fatalf("error length = %d, want a bounded excerpt", len(err.Error()))
	}
}

// TestLimitWriterPartialAndDiscard verifies the capture cap at the Write
// level: one overlong write is trimmed, and later writes are discarded.
func TestLimitWriterPartialAndDiscard(t *testing.T) {
	w := &limitWriter{max: 10}
	if n, err := w.Write([]byte("hello world more")); err != nil || n != 16 {
		t.Fatalf("Write(overlong) = %d, %v; want all bytes consumed", n, err)
	}
	if w.String() != "hello worl" {
		t.Fatalf("captured = %q, want the first 10 bytes", w.String())
	}
	if n, err := w.Write([]byte("x")); err != nil || n != 1 {
		t.Fatalf("Write(after cap) = %d, %v", n, err)
	}
	if w.String() != "hello worl" {
		t.Fatalf("captured changed after cap: %q", w.String())
	}
}

// TestDetectWithoutHome verifies the fallback when no home directory can
// be resolved at all.
func TestDetectWithoutHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if Detect("no-such-command-anywhere", ".nothing-here") {
		t.Fatal("Detect(no home) = true, want false")
	}
}

// TestValidTag verifies the release tag shape.
func TestValidTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"v1.2.0", true},
		{"v10.20.30", true},
		{"latest", false},
		{"1.2.0", false},
		{"v1.2", false},
		{"v1.2.3-rc1", false},
		{"", false},
	}
	for _, c := range cases {
		if got := ValidTag(c.tag); got != c.want {
			t.Fatalf("ValidTag(%q) = %v, want %v", c.tag, got, c.want)
		}
	}
}
