// Package runner is the non-Git subprocess boundary for the update
// command: it runs curl for the checksum-verified release fetch, runs the
// staged or freshly installed git-byline binary, and resolves agent
// presence. Like internal/gitcmd, every invocation uses an argument
// slice, never a shell, and is bounded by a timeout.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Subprocess timeouts. The probe follows one redirect and the run
// executes one git-byline command, so both get the same headroom the
// Git boundary uses. Downloads carry a larger payload, so they get
// more. Captured subprocess output is capped so a hostile or broken
// subprocess cannot exhaust memory.
const (
	probeTimeout  = 30 * time.Second
	fetchTimeout  = 120 * time.Second
	runTimeout    = 30 * time.Second
	curlStderrMax = 8 << 10
	outputMax     = 1 << 20
	// waitGrace bounds how long a killed subprocess may keep its output
	// pipes open. CommandContext kills only the direct process, so a
	// descendant holding a pipe could otherwise block Run past the
	// deadline; WaitDelay turns that wait into a bounded one.
	waitGrace = 5 * time.Second
)

// tagPattern accepts exactly the release tags the installers accept.
var tagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// ValidTag reports whether tag is a supported release tag.
func ValidTag(tag string) bool {
	return tagPattern.MatchString(tag)
}

// Runner executes curl for update downloads. The curl executable is
// resolved once, so a missing curl fails the command before any file is
// touched. Timeouts are fields so tests can exercise the deadline paths
// without waiting for the production values.
type Runner struct {
	curlPath     string
	probeTimeout time.Duration
	fetchTimeout time.Duration
}

// New returns a Runner that invokes the curl at curlPath. An empty path
// resolves curl through PATH.
func New(curlPath string) (*Runner, error) {
	if curlPath == "" {
		resolved, err := exec.LookPath("curl")
		if err != nil {
			return nil, fmt.Errorf("update needs curl on PATH: %w", err)
		}
		curlPath = resolved
	}
	return &Runner{curlPath: curlPath, probeTimeout: probeTimeout, fetchTimeout: fetchTimeout}, nil
}

// Fetch downloads url into dest with the installer's HTTPS-only curl
// flags, so the in-binary fetch has the same transport guarantees as
// install.sh. Only https URLs are accepted, so a malformed input can
// never become a curl option. The caller owns dest.
func (r *Runner) Fetch(url, dest string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("fetch %s: only https URLs are allowed", url)
	}
	args := append(curlBase(), "-o", dest, url)
	if err := r.curl(r.fetchTimeout, "fetch "+url, args, nil); err != nil {
		return err
	}
	return nil
}

// LatestTag resolves the newest release tag by following the release
// page redirect, like install.sh, and validating the tag shape so a
// hostile or broken redirect cannot steer later URLs. The probe sends
// HEAD requests, because only the final redirect URL is needed.
func (r *Runner) LatestTag(repository string) (string, error) {
	args := append(curlBase(), "-I", "-o", os.DevNull, "-w", "%{url_effective}", repository+"/releases/latest")
	stdout := &limitWriter{max: outputMax}
	if err := r.curl(r.probeTimeout, "resolve latest release", args, stdout); err != nil {
		return "", err
	}
	effective := strings.TrimSpace(stdout.String())
	if effective == "" {
		return "", errors.New("resolve latest release: no redirect URL")
	}
	tag := effective
	if idx := strings.LastIndex(tag, "/"); idx >= 0 {
		tag = tag[idx+1:]
	}
	if !ValidTag(tag) {
		return "", fmt.Errorf("unexpected latest release tag %q", tag)
	}
	return tag, nil
}

// curlBase returns the HTTPS-only curl prefix shared by every fetch.
// -q must stay first: it ignores curlrc, so a hostile or careless local
// curl configuration cannot weaken the transport flags.
func curlBase() []string {
	return []string{"-q", "-fsSL", "--proto", "=https", "--proto-redir", "=https", "--tlsv1.2"}
}

// curl runs the curl binary once under a bounded context. stderr is
// captured and included in the error, because curl writes its failure
// reason there.
func (r *Runner) curl(timeout time.Duration, operation string, args []string, stdout io.Writer) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.curlPath, args...)
	cmd.WaitDelay = waitGrace
	if stdout == nil {
		cmd.Stdout = nil
	} else {
		cmd.Stdout = stdout
	}
	stderr := &limitWriter{max: curlStderrMax}
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		// Keep the context sentinel reachable so callers can tell a
		// timeout from any other curl failure.
		return fmt.Errorf("%s: timed out: %w", operation, context.DeadlineExceeded)
	}
	if err != nil {
		if reason := strings.TrimSpace(stderr.String()); reason != "" {
			return fmt.Errorf("%s: %w: %s", operation, err, truncate(reason))
		}
		return fmt.Errorf("%s: %w", operation, err)
	}
	return nil
}

// truncate bounds a subprocess stderr excerpt in error messages. The
// capture is already capped by limitWriter; this keeps the formatted
// error itself short.
func truncate(text string) string {
	if len(text) <= curlStderrMax {
		return text
	}
	return text[:curlStderrMax] + "..."
}

// Run executes one git-byline binary with args and returns its stdout
// and stderr. On failure the exit status and stderr also surface in the
// error, so callers can pass subprocess output through and still tell a
// failed hook refresh from a successful one.
func Run(path string, args ...string) (string, string, error) {
	return runBinary(runTimeout, path, args...)
}

// runBinary is Run with an injectable timeout, so tests can cover the
// deadline path without waiting for the production value.
func runBinary(timeout time.Duration, path string, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = waitGrace
	stdout := &limitWriter{max: outputMax}
	stderr := &limitWriter{max: outputMax}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		// Keep the context sentinel reachable so callers can tell a
		// timeout from any other exec failure.
		return "", "", fmt.Errorf("run %s: timed out: %w", path, context.DeadlineExceeded)
	}
	if err != nil {
		if reason := strings.TrimSpace(stderr.String()); reason != "" {
			return "", "", fmt.Errorf("run %s: %w: %s", path, err, truncate(reason))
		}
		return "", "", fmt.Errorf("run %s: %w", path, err)
	}
	return stdout.String(), stderr.String(), nil
}

// limitWriter is an io.Writer that keeps at most max bytes and silently
// discards the rest, so captured subprocess output stays bounded.
type limitWriter struct {
	buf bytes.Buffer
	max int
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); room > 0 {
		if len(p) > room {
			w.buf.Write(p[:room])
			return len(p), nil
		}
		w.buf.Write(p)
	}
	return len(p), nil
}

func (w *limitWriter) String() string {
	return w.buf.String()
}

// Detect reports whether an agent is present, by its command on PATH or
// its configuration directory under the home directory. Detection never
// writes anything, mirroring install.sh.
func Detect(command, configDir string) bool {
	if _, err := exec.LookPath(command); err == nil {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(home, configDir))
	return err == nil && info.IsDir()
}
