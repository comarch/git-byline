//go:build !windows

package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/version"
)

// fakeRelease serves one release through a fake curl: the latest-tag
// probe prints a redirect, and downloads copy files from a fixture
// directory keyed by the URL's last segment.
type fakeRelease struct {
	dir     string
	latest  string
	tagName string
}

// newFakeRelease builds a release directory for the given tag, with the
// checksums and the archive for the current POSIX platform, plus a fake
// curl on PATH.
func newFakeRelease(t *testing.T, latest, binaryScript string) *fakeRelease {
	t.Helper()
	dir := t.TempDir()
	rel := &fakeRelease{dir: dir, latest: latest}
	rel.buildArchive(t, binaryScript)
	bin := filepath.Join(dir, "curl")
	body := strings.Join([]string{
		`for arg in "$@"; do`,
		`  [ "$arg" = "%{url_effective}" ] && { echo "$FAKE_REDIRECT"; exit 0; }`,
		`done`,
		`prev=""`,
		`for arg in "$@"; do`,
		`  [ "$prev" = "-o" ] && dest="$arg"`,
		`  prev="$arg"`,
		`done`,
		`for arg in "$@"; do url="$arg"; done`,
		`cp "$FAKE_DIR/${url##*/}" "$dest"`,
	}, "\n")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("FAKE_RELEASE_VERSION", latest)
	t.Setenv("FAKE_REDIRECT", "https://github.com/comarch/git-byline/releases/tag/"+rel.latest)
	return rel
}

// buildArchive assembles the tar.gz release for the current platform and
// its checksums.txt, and records the archive name.
func (r *fakeRelease) buildArchive(t *testing.T, binaryScript string) {
	t.Helper()
	name := releaseArchiveName(r.latest)
	r.tagName = name
	archivePath := filepath.Join(r.dir, "fixture.tar.gz")
	buildTarGZ(t, archivePath, releaseTarEntries(binaryScript))
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(archivePath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksums(t, r.dir, name, data)
}

// runFakeUpdate executes runFetchUpdate with a stand-in target and
// returns the target path for assertions.
func (r *fakeRelease) runFakeUpdate(t *testing.T, tag string, dryRun, noHooks bool) (int, *bytes.Buffer, *bytes.Buffer, error, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "git-byline")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := r.runFakeUpdateTarget(t, target, tag, dryRun, noHooks)
	return code, stdout, stderr, err, target
}

// runFakeUpdateTarget executes runFetchUpdate against an explicit target.
func (r *fakeRelease) runFakeUpdateTarget(t *testing.T, target, tag string, dryRun, noHooks bool) (int, *bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	code, err := runFetchUpdate(env, &command{name: "update"}, target, tag, dryRun, noHooks)
	return code, &stdout, &stderr, err
}

// goodBinary is a stand-in release binary that reports the fake release
// version from the environment, satisfying the staged version sanity check.
const goodBinary = `#!/bin/sh
echo "git-byline $FAKE_RELEASE_VERSION"
`

// isolateHookEnv points HOME and XDG_CONFIG_HOME at fresh directories,
// so hook refresh tests read no real user configuration, and returns the
// XDG base for template setup.
func isolateHookEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return xdg
}

// fakeCurlProbe installs a fake curl that answers every probe with the
// given redirect URL and serves nothing else.
func fakeCurlProbe(t *testing.T, redirect string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \""+redirect+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// loggingBinary reports the fake release version from the environment and
// additionally records install-hooks invocations, so end-to-end tests
// observe the hook refresh.
const loggingBinary = `#!/bin/sh
if [ "$1" = "install-hooks" ]; then
  echo "$@" >> "$FAKE_TARGET_LOG"
fi
echo "git-byline $FAKE_RELEASE_VERSION"
`

// TestRunFetchUpdateHookRefreshModes covers the end-to-end hook refresh
// as a table: after a real swap the new binary receives the project
// install-hooks pass, and a dry run neither swaps nor refreshes hooks.
func TestRunFetchUpdateHookRefreshModes(t *testing.T) {
	cases := []struct {
		name     string
		dryRun   bool
		wantLog  bool
		wantOut  string
		wantBody string
	}{
		{
			name:     "swap refreshes hooks through the new binary",
			wantLog:  true,
			wantOut:  "Updated git-byline to v9.9.9 at ",
			wantBody: loggingBinary,
		},
		{
			name:     "dry run verifies without swapping or refreshing",
			dryRun:   true,
			wantOut:  "Dry run: verified",
			wantBody: "old binary",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateHookEnv(t)
			root := appRepo(t)
			log := filepath.Join(t.TempDir(), "target.log")
			t.Setenv("FAKE_TARGET_LOG", log)
			newFakeRelease(t, "v9.9.9", loggingBinary)
			target := filepath.Join(t.TempDir(), "git-byline")
			if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			env := testEnv(&stdout)
			env.Stderr = &stderr
			env.Dir = root
			code, err := runFetchUpdate(env, &command{name: "update"}, target, "latest", c.dryRun, false)
			if code != ExitSuccess || err != nil {
				t.Fatalf("runFetchUpdate = %d, %v; stderr %q", code, err, stderr.String())
			}
			if !strings.Contains(stdout.String(), c.wantOut) {
				t.Fatalf("stdout = %q, want %q", stdout.String(), c.wantOut)
			}
			assertTarget(t, target, c.wantBody)
			calls := readTargetLog(t, log)
			if c.wantLog {
				if len(calls) == 0 || calls[0] != "install-hooks --agent none --git --project" {
					t.Fatalf("calls = %v, want the project refresh first", calls)
				}
				return
			}
			if len(calls) != 0 {
				t.Fatalf("calls = %v, want no hook refresh on dry run", calls)
			}
		})
	}
}

// TestRunFetchUpdateDownloadsAndSwaps covers the full download path: tag
// probe, checksums fetch, archive fetch, checksum verify, staged version
// check, and the swap.
func TestRunFetchUpdateDownloadsAndSwaps(t *testing.T) {
	rel := newFakeRelease(t, "v9.9.9", goodBinary)
	code, out, stderr, err, target := rel.runFakeUpdate(t, "latest", false, true)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runFetchUpdate = %d, %v; stderr %q", code, err, stderr.String())
	}
	assertTarget(t, target, goodBinary)
	want := fmt.Sprintf("Updated git-byline to %s at %s (was %s)\nRun 'git-byline version' to confirm the new version.\n",
		rel.latest, target, version.Version)
	if out.String() != want {
		t.Fatalf("stdout = %q, want %q", out.String(), want)
	}
}

// TestRunFetchUpdatePinnedTag skips the probe when --version names the
// release directly.
func TestRunFetchUpdatePinnedTag(t *testing.T) {
	rel := newFakeRelease(t, "v9.9.9", goodBinary)
	code, out, stderr, err, _ := rel.runFakeUpdate(t, "v9.9.9", false, true)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runFetchUpdate(pinned) = %d, %v; stderr %q", code, err, stderr.String())
	}
	if !strings.Contains(out.String(), "Updated git-byline to v9.9.9") {
		t.Fatalf("stdout = %q, want updated message", out.String())
	}
}

// TestRunFetchUpdatePinnedCurrentNeedsNoCurl verifies that pinning the
// already-running release reports the no-op without resolving curl.
func TestRunFetchUpdatePinnedCurrentNeedsNoCurl(t *testing.T) {
	previous := version.Version
	version.Version = "v9.9.9"
	defer func() { version.Version = previous }()
	t.Setenv("PATH", t.TempDir())
	target := filepath.Join(t.TempDir(), "git-byline")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	code, err := runFetchUpdate(env, &command{name: "update"}, target, "v9.9.9", false, true)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runFetchUpdate(pinned current) = %d, %v; stderr %q", code, err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "git-byline v9.9.9 is already up to date at ") {
		t.Fatalf("stdout = %q, want up-to-date message", stdout.String())
	}
	assertTarget(t, target, "old binary")
}

// TestRunFetchUpdateVersionSelection is the table-driven version of the
// release-selection paths: the latest pointer equal to or older than the
// running release, and a pinned release below the running one.
func TestRunFetchUpdateVersionSelection(t *testing.T) {
	cases := []struct {
		name     string
		running  string
		latest   string
		runTag   string
		wantOut  string
		wantBody string
	}{
		{
			name:     "latest equals the running release",
			running:  "v9.9.9",
			latest:   "v9.9.9",
			runTag:   "latest",
			wantOut:  "git-byline v9.9.9 is already up to date at ",
			wantBody: "old binary",
		},
		{
			name:     "latest is older than the running release",
			running:  "v9.9.9",
			latest:   "v8.8.8",
			runTag:   "latest",
			wantOut:  "git-byline v9.9.9 is newer than the latest release v8.8.8; not updating automatically",
			wantBody: "old binary",
		},
		{
			name:     "pinned release below the running one is deliberate",
			running:  "v9.9.9",
			latest:   "v8.8.8",
			runTag:   "v8.8.8",
			wantOut:  "Updated git-byline to v8.8.8 at ",
			wantBody: goodBinary,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			previous := version.Version
			version.Version = c.running
			defer func() { version.Version = previous }()
			rel := newFakeRelease(t, c.latest, goodBinary)
			code, out, stderr, err, target := rel.runFakeUpdate(t, c.runTag, false, true)
			if code != ExitSuccess || err != nil {
				t.Fatalf("runFetchUpdate = %d, %v; stderr %q", code, err, stderr.String())
			}
			if !strings.Contains(out.String(), c.wantOut) {
				t.Fatalf("stdout = %q, want %q", out.String(), c.wantOut)
			}
			assertTarget(t, target, c.wantBody)
		})
	}
}

// TestRunFetchUpdateDryRun verifies that a dry run verifies the download
// but leaves the target unchanged.
func TestRunFetchUpdateDryRun(t *testing.T) {
	rel := newFakeRelease(t, "v9.9.9", goodBinary)
	code, out, stderr, err, target := rel.runFakeUpdate(t, "latest", true, true)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runFetchUpdate(dry) = %d, %v; stderr %q", code, err, stderr.String())
	}
	if !strings.Contains(out.String(), "Dry run: verified") {
		t.Fatalf("stdout = %q, want dry-run message", out.String())
	}
	assertTarget(t, target, "old binary")
}

// TestRunFetchUpdateChecksumMismatch verifies that a tampered download
// fails and the target stays unchanged.
func TestRunFetchUpdateChecksumMismatch(t *testing.T) {
	rel := newFakeRelease(t, "v9.9.9", goodBinary)
	archiveBytes, err := os.ReadFile(filepath.Join(rel.dir, rel.tagName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rel.dir, rel.tagName), append(archiveBytes, []byte("tampered")...), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, stderr, err, target := rel.runFakeUpdate(t, "latest", false, true)
	if code != ExitFailure || err == nil {
		t.Fatalf("runFetchUpdate(tampered) = %d, %v", code, err)
	}
	if !strings.Contains(stderr.String(), "checksum mismatch") {
		t.Fatalf("stderr = %q, want checksum mismatch", stderr.String())
	}
	assertTarget(t, target, "old binary")
}

// TestRunFetchUpdateWrongStagedVersion verifies that a checksummed
// archive whose binary reports a different release fails before the
// swap.
func TestRunFetchUpdateWrongStagedVersion(t *testing.T) {
	rel := newFakeRelease(t, "v9.9.9", "#!/bin/sh\necho \"git-byline v0.0.1\"\n")
	code, _, stderr, err, target := rel.runFakeUpdate(t, "latest", false, true)
	if code != ExitFailure || err == nil {
		t.Fatalf("runFetchUpdate(wrong version) = %d, %v", code, err)
	}
	if !strings.Contains(stderr.String(), "staged binary reports git-byline v0.0.1, want git-byline v9.9.9") {
		t.Fatalf("stderr = %q, want staged version mismatch", stderr.String())
	}
	assertTarget(t, target, "old binary")
}

// writeFakeTarget installs a stand-in git-byline binary that logs its
// arguments and exits 0, so hook refresh invocations are observable.
func writeFakeTarget(t *testing.T, log string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "git-byline")
	body := strings.Join([]string{
		`echo "$@" >> "$FAKE_TARGET_LOG"`,
		`echo refreshed`,
	}, "\n")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TARGET_LOG", log)
	return target
}

// readTargetLog returns the recorded invocations of the stand-in binary.
func readTargetLog(t *testing.T, log string) []string {
	t.Helper()
	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var calls []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			calls = append(calls, line)
		}
	}
	return calls
}

// TestRefreshHooksProjectAndTemplate verifies the Git-side refresh: the
// project hooks run inside a worktree, and the template refresh runs only
// when the managed template is already the configured one.
func TestRefreshHooksProjectAndTemplate(t *testing.T) {
	xdg := isolateHookEnv(t)
	root := appRepo(t)
	log := filepath.Join(t.TempDir(), "target.log")
	target := writeFakeTarget(t, log)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	env.Dir = root

	// Without a template configured, only the project hook refresh runs.
	refreshHooks(env, target, func(string, string) bool { return false })
	calls := readTargetLog(t, log)
	want := []string{"install-hooks --agent none --git --project"}
	if len(calls) != 1 || calls[0] != want[0] {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if !strings.Contains(stdout.String(), "refreshed") {
		t.Fatalf("stdout = %q, want pass-through hook output", stdout.String())
	}

	// With the managed template configured, the template refresh joins in.
	templateDir := filepath.Join(xdg, "git-byline", "templates")
	if err := gitcmd.SetGlobalConfig("init.templateDir", templateDir); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	_ = os.Remove(log)
	refreshHooks(env, target, func(string, string) bool { return false })
	calls = readTargetLog(t, log)
	want = []string{
		"install-hooks --agent none --git --project",
		"install-hooks --agent none --git --template",
	}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v", calls, want)
	}

	// A foreign template value is left alone.
	if err := gitcmd.SetGlobalConfig("init.templateDir", filepath.Join(t.TempDir(), "foreign-templates")); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(log)
	refreshHooks(env, target, func(string, string) bool { return false })
	calls = readTargetLog(t, log)
	if len(calls) != 1 || calls[0] != want[0] {
		t.Fatalf("calls = %v, want only the project refresh", calls)
	}
}

// TestRefreshHooksUserAgents verifies the user-level agent refresh and
// its installer-parity output.
func TestRefreshHooksUserAgents(t *testing.T) {
	isolateHookEnv(t)
	root := t.TempDir()
	log := filepath.Join(t.TempDir(), "target.log")
	target := writeFakeTarget(t, log)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	env.Dir = root
	refreshHooks(env, target, func(command, _ string) bool {
		return command == "claude" || command == "droid"
	})
	calls := readTargetLog(t, log)
	want := []string{"install-hooks --agent droid --user", "install-hooks --agent claude --user"}
	if len(calls) != 2 || calls[0] != want[0] || calls[1] != want[1] {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
	if !strings.Contains(stdout.String(), "Installed the droid hook for every repository.") ||
		!strings.Contains(stdout.String(), "Installed the claude hook for every repository.") {
		t.Fatalf("stdout = %q, want installed lines", stdout.String())
	}
}

// TestRefreshHooksUserAgentFailure verifies the warning when a user-level
// refresh fails.
func TestRefreshHooksUserAgentFailure(t *testing.T) {
	isolateHookEnv(t)
	bad := filepath.Join(t.TempDir(), "git-byline")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	refreshHooks(env, bad, func(string, string) bool { return true })
	if !strings.Contains(stderr.String(), "Could not install the droid hook. Run: git-byline install-hooks --agent droid --user") {
		t.Fatalf("stderr = %q, want could-not-install warning", stderr.String())
	}
	if strings.Contains(stdout.String(), "Installed the droid hook") {
		t.Fatalf("stdout = %q, want no success line", stdout.String())
	}
}

// TestRefreshHooksManualAgents verifies the installer-parity copy
// commands for agents git-byline cannot configure itself.
func TestRefreshHooksManualAgents(t *testing.T) {
	isolateHookEnv(t)
	root := t.TempDir()
	log := filepath.Join(t.TempDir(), "target.log")
	target := writeFakeTarget(t, log)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	env.Dir = root
	refreshHooks(env, target, func(command, _ string) bool { return command == "gemini" })
	out := stdout.String()
	if !strings.Contains(out, "Detected agents that need one hook file per project:") {
		t.Fatalf("stdout = %q, want detected header", out)
	}
	if !strings.Contains(out, "gemini         curl -fsSL --proto =https --tlsv1.2 -o .gemini/settings.json --create-dirs \\") {
		t.Fatalf("stdout = %q, want gemini curl command", out)
	}
	if !strings.Contains(out, "https://github.com/comarch/git-byline/raw/main/marketplace/harness/gemini/settings.json") {
		t.Fatalf("stdout = %q, want gemini harness URL", out)
	}
	if !strings.Contains(out, "Merge the block for gemini instead of replacing the file.") {
		t.Fatalf("stdout = %q, want merge hint", out)
	}
	if len(readTargetLog(t, log)) != 0 {
		t.Fatalf("calls = %v, want none for manual agents", readTargetLog(t, log))
	}
}

// TestRefreshHooksRefreshStepFailure verifies that a failing Git-side
// refresh warns instead of failing the update.
func TestRefreshHooksRefreshStepFailure(t *testing.T) {
	isolateHookEnv(t)
	bad := filepath.Join(t.TempDir(), "git-byline")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\necho boom >&2; exit 4\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := appRepo(t)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	env.Dir = root
	refreshHooks(env, bad, func(string, string) bool { return false })
	if !strings.Contains(stderr.String(), "git-byline install-hooks --agent none --git --project failed:") {
		t.Fatalf("stderr = %q, want refresh failure warning", stderr.String())
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q, want subprocess stderr", stderr.String())
	}
}

// TestRunOfflineUpdateRefreshesHooks verifies that the offline
// --archive --checksums path refreshes managed hooks after the swap,
// unless --no-hooks asks otherwise.
func TestRunOfflineUpdateRefreshesHooks(t *testing.T) {
	isolateHookEnv(t)
	root := appRepo(t)
	log := filepath.Join(t.TempDir(), "target.log")
	t.Setenv("FAKE_TARGET_LOG", log)
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "release.tar.gz")
	buildTarGZ(t, archivePath, releaseTarEntries(loggingBinary))
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	checksumsPath := writeChecksums(t, dir, filepath.Base(archivePath), archiveData)
	target := writeFakeTarget(t, log)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	env.Dir = root
	code, err := runOfflineUpdate(env, &command{name: "update"}, target, archivePath, checksumsPath, false, false)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runOfflineUpdate(hooks) = %d, %v; stderr %q", code, err, stderr.String())
	}
	calls := readTargetLog(t, log)
	if len(calls) == 0 || calls[0] != "install-hooks --agent none --git --project" {
		t.Fatalf("calls = %v, want the project refresh first", calls)
	}
	_ = os.Remove(log)
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	code, err = runOfflineUpdate(env, &command{name: "update"}, target, archivePath, checksumsPath, false, true)
	if code != ExitSuccess || err != nil {
		t.Fatalf("runOfflineUpdate(no hooks) = %d, %v", code, err)
	}
	if calls := readTargetLog(t, log); len(calls) != 0 {
		t.Fatalf("calls = %v, want no refresh with --no-hooks", calls)
	}
}

// TestRefreshHooksTemplateConfigError verifies that a Git template
// configuration read failure warns and continues the refresh instead of
// silently skipping it.
func TestRefreshHooksTemplateConfigError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "relative-path")
	log := filepath.Join(t.TempDir(), "target.log")
	target := writeFakeTarget(t, log)
	var stdout, stderr bytes.Buffer
	env := testEnv(&stdout)
	env.Stderr = &stderr
	refreshHooks(env, target, func(command, _ string) bool { return command == "claude" })
	if !strings.Contains(stderr.String(), "git-byline could not read the Git template configuration:") {
		t.Fatalf("stderr = %q, want template configuration warning", stderr.String())
	}
	if !strings.Contains(stderr.String(), "XDG_CONFIG_HOME is not an absolute path") {
		t.Fatalf("stderr = %q, want the underlying error", stderr.String())
	}
	if !strings.Contains(stdout.String(), "Installed the claude hook for every repository.") {
		t.Fatalf("stdout = %q, want the refresh to continue", stdout.String())
	}
}

// TestVersionCheckRunningNewer verifies the report when the running
// release is newer than the latest-release pointer.
func TestVersionCheckRunningNewer(t *testing.T) {
	fakeCurlProbe(t, "https://github.com/comarch/git-byline/releases/tag/v8.8.8")
	previous := version.Version
	version.Version = "v9.9.9"
	defer func() { version.Version = previous }()
	code, out, stderr, err := appRun(t.TempDir(), zeroTime(), nil, "version", "--check")
	if code != ExitSuccess || err != nil {
		t.Fatalf("version --check = %d, %v; stderr %q", code, err, stderr)
	}
	if !strings.Contains(out, "git-byline v9.9.9 is newer than the latest release v8.8.8.") {
		t.Fatalf("stdout = %q, want newer-than-latest message", out)
	}
}

// TestVersionCheckReportsNewRelease verifies the version --check hint
// through a fake curl probe.
func TestVersionCheckReportsNewRelease(t *testing.T) {
	fakeCurlProbe(t, "https://github.com/comarch/git-byline/releases/tag/v9.9.9")
	code, out, stderr, err := appRun(t.TempDir(), zeroTime(), nil, "version", "--check")
	if code != ExitSuccess || err != nil {
		t.Fatalf("version --check = %d, %v; stderr %q", code, err, stderr)
	}
	if !strings.Contains(out, "git-byline dev\n") {
		t.Fatalf("stdout = %q, want version line", out)
	}
	if !strings.Contains(out, "New release v9.9.9 is available. Run 'git-byline update' to install it.") {
		t.Fatalf("stdout = %q, want new release hint", out)
	}
}

// TestVersionCheckUpToDate verifies the up-to-date message when the
// running release matches the latest one.
func TestVersionCheckUpToDate(t *testing.T) {
	fakeCurlProbe(t, "https://github.com/comarch/git-byline/releases/tag/v9.9.9")
	previous := version.Version
	version.Version = "v9.9.9"
	defer func() { version.Version = previous }()
	code, out, stderr, err := appRun(t.TempDir(), zeroTime(), nil, "version", "--check")
	if code != ExitSuccess || err != nil {
		t.Fatalf("version --check = %d, %v; stderr %q", code, err, stderr)
	}
	if !strings.Contains(out, "git-byline v9.9.9 is up to date with the latest release.") {
		t.Fatalf("stdout = %q, want up-to-date message", out)
	}
}

// TestVersionCheckProbeFailure verifies that an unreachable release page
// fails the check as an operational error.
func TestVersionCheckProbeFailure(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "curl")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'curl: (7) connect failed' >&2\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))
	code, _, stderr, err := appRun(t.TempDir(), zeroTime(), nil, "version", "--check")
	if code != ExitFailure || err == nil {
		t.Fatalf("version --check(probe failure) = %d, %v", code, err)
	}
	if !strings.Contains(stderr, "connect failed") {
		t.Fatalf("stderr = %q, want curl failure", stderr)
	}
}
