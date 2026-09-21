package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// forgeInstall runs one install-hooks pass and fails unless it succeeds
// without warnings.
func forgeInstall(t *testing.T, root string, args ...string) string {
	t.Helper()
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		append([]string{"install-hooks"}, args...)...)
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("install-hooks %v = %d, %q, %q, %v", args, code, stdout, stderr, err)
	}
	return stdout
}

// forgeUninstall runs one uninstall pass and fails unless it exits
// successfully.
func forgeUninstall(t *testing.T, root string, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		append([]string{"uninstall"}, args...)...)
	if code != ExitSuccess || err != nil {
		t.Fatalf("uninstall %v = %d, %q, %q, %v", args, code, stdout, stderr, err)
	}
	return stdout, stderr
}

// TestInstallHooksProvisionsForgeWorkflow covers workflow creation for
// public GitHub and GitLab origins, including the idempotent rerun.
func TestInstallHooksProvisionsForgeWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		remote   string
		wantLine string
		wantPath string
	}{
		{
			"github",
			"https://github.com/comarch/git-byline.git",
			"created .github/workflows/git-byline.yml",
			".github/workflows/git-byline.yml",
		},
		{
			"gitlab",
			"ssh://git@gitlab.com/group/project.git",
			"created .gitlab/ci/git-byline.yml",
			".gitlab/ci/git-byline.yml",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := appRepo(t)
			appGit(t, root, "remote", "add", "origin", test.remote)
			stdout := forgeInstall(t, root, "--agent", "none", "--git")
			if !strings.Contains(stdout, test.wantLine) {
				t.Fatalf("install status = %q, want %q", stdout, test.wantLine)
			}
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(test.wantPath))); statErr != nil {
				t.Fatalf("workflow %s: %v", test.wantPath, statErr)
			}
			stdout = forgeInstall(t, root, "--agent", "none", "--git")
			if !strings.Contains(stdout, "workflow already installed at "+test.wantPath) {
				t.Fatalf("reinstall status = %q", stdout)
			}
		})
	}
}

// TestInstallHooksForgeWorkflowHintNoRemote covers the hint for a
// repository without a remote.
func TestInstallHooksForgeWorkflowHintNoRemote(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	stdout := forgeInstall(t, root, "--agent", "none", "--git")
	if !strings.Contains(stdout, "no GitHub or GitLab remote detected") {
		t.Fatalf("install status = %q, want the remote hint", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".github created without a forge remote: %v", statErr)
	}
}

// TestInstallHooksForgeWorkflowHintUnsupportedRemote covers the hint for
// a remote outside the supported forges.
func TestInstallHooksForgeWorkflowHintUnsupportedRemote(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://example.com/owner/repo.git")
	stdout := forgeInstall(t, root, "--agent", "none", "--git")
	if !strings.Contains(stdout, "no GitHub or GitLab remote detected") {
		t.Fatalf("install status = %q, want the remote hint", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".github created for an unsupported forge: %v", statErr)
	}
}

// TestInstallHooksLocalNotesSkipsForgeWorkflow covers the sharing opt-out.
func TestInstallHooksLocalNotesSkipsForgeWorkflow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
	stdout := forgeInstall(t, root, "--agent", "none", "--git", "--local-notes")
	if strings.Contains(stdout, "no GitHub or GitLab remote detected") ||
		strings.Contains(stdout, "created ") {
		t.Fatalf("install status = %q, want no workflow output", stdout)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".github created with --local-notes: %v", statErr)
	}
}

// TestInstallHooksTemplateSkipsForgeWorkflow covers template scope, which
// has no repository remote.
func TestInstallHooksTemplateSkipsForgeWorkflow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stdout := forgeInstall(t, t.TempDir(),
		"--agent", "none", "--git", "--template")
	if strings.Contains(stdout, "no GitHub or GitLab remote detected") ||
		strings.Contains(stdout, "created ") {
		t.Fatalf("install status = %q, want no workflow output", stdout)
	}
}

// TestInstallHooksAgentOnlySkipsForgeWorkflow covers the path without Git
// hooks.
func TestInstallHooksAgentOnlySkipsForgeWorkflow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	stdout := forgeInstall(t, root, "--agent", "droid")
	if strings.Contains(stdout, "no GitHub or GitLab remote detected") {
		t.Fatalf("install status = %q, want no workflow output", stdout)
	}
}

// TestInstallHooksForgeWorkflowFailureWarns covers the blocked workflow
// path on every platform.
func TestInstallHooksForgeWorkflowFailureWarns(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
	if err := os.WriteFile(filepath.Join(root, ".github"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "none", "--git")
	if code != ExitSuccess || err != nil ||
		!strings.Contains(stderr, "warning: could not install the forge workflow") {
		t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

// TestUninstallRemovesForgeWorkflow covers removal of the provisioned
// workflow.
func TestUninstallRemovesForgeWorkflow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
	forgeInstall(t, root, "--agent", "none", "--git")
	stdout, stderr := forgeUninstall(t, root, "--agent", "none", "--git")
	if !strings.Contains(stdout, "updated .github/workflows/git-byline.yml") {
		t.Fatalf("uninstall status = %q, stderr = %q", stdout, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github", "workflows", "git-byline.yml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workflow still present: %v", statErr)
	}
}

// TestUninstallKeepsModifiedForgeWorkflow covers a customized workflow,
// which stays in place.
func TestUninstallKeepsModifiedForgeWorkflow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
	forgeInstall(t, root, "--agent", "none", "--git")
	path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
	if err := os.WriteFile(path, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := forgeUninstall(t, root, "--agent", "none", "--git")
	if strings.Contains(stdout, "updated .github/workflows/git-byline.yml") {
		t.Fatalf("uninstall status = %q, stderr = %q, want the modified file kept", stdout, stderr)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("modified workflow removed: %v", statErr)
	}
}

// TestUninstallForgeWorkflowPartialRemovalWarns covers a run where one
// workflow is removed before a later provider fails.
func TestUninstallForgeWorkflowPartialRemovalWarns(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
	forgeInstall(t, root, "--agent", "none", "--git")
	if err := os.WriteFile(filepath.Join(root, ".gitlab"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := forgeUninstall(t, root, "--agent", "none", "--git")
	if !strings.Contains(stdout, "updated .github/workflows/git-byline.yml") {
		t.Fatalf("uninstall status = %q, want the completed removal", stdout)
	}
	if !strings.Contains(stderr, "warning: could not remove the forge workflow") {
		t.Fatalf("uninstall stderr = %q, want the later failure warning", stderr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github", "workflows", "git-byline.yml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("workflow still present: %v", statErr)
	}
}

// TestUninstallAgentOnlySkipsForgeWorkflow covers the path without Git
// hooks.
func TestUninstallAgentOnlySkipsForgeWorkflow(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	stdout, stderr := forgeUninstall(t, root, "--agent", "droid")
	if strings.Contains(stdout, "updated .github/workflows/git-byline.yml") ||
		strings.Contains(stderr, "warning: could not remove the forge workflow") {
		t.Fatalf("uninstall = %q, %q, want no workflow output", stdout, stderr)
	}
}

// TestUninstallTemplateSkipsForgeWorkflow covers template scope, which
// never manages the repository workflow.
func TestUninstallTemplateSkipsForgeWorkflow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	forgeInstall(t, root, "--agent", "none", "--git", "--template", "--local-notes")
	stdout, stderr := forgeUninstall(t, root, "--agent", "none", "--git", "--template")
	if strings.Contains(stdout, ".github/workflows/git-byline.yml") ||
		strings.Contains(stderr, "warning: could not remove the forge workflow") {
		t.Fatalf("uninstall = %q, %q, want no workflow output", stdout, stderr)
	}
}
