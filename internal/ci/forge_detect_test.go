package ci

import (
	"embed"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRemoteHost checks host extraction across Git remote URL forms.
func TestRemoteHost(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		remote string
		host   string
		found  bool
	}{
		{"https github", "https://github.com/comarch/git-byline.git", "github.com", true},
		{"https gitlab", "https://gitlab.com/group/project.git", "gitlab.com", true},
		{"scp github", "git@github.com:comarch/git-byline.git", "github.com", true},
		{"scp gitlab", "git@gitlab.com:group/project.git", "gitlab.com", true},
		{"ssh github", "ssh://git@github.com/comarch/git-byline.git", "github.com", true},
		{"ssh gitlab port", "ssh://git@gitlab.com:2222/group/project.git", "gitlab.com", true},
		{"ssh host port", "ssh://github.com:22/comarch/git-byline.git", "github.com", true},
		{"https token user", "https://oauth2:token@github.com/comarch/git-byline.git", "github.com", true},
		{"https at user", "https://user@github.com/comarch/git-byline.git", "github.com", true},
		{"mixed case", "HTTPS://GitHub.COM/O/R", "github.com", true},
		{"enterprise host", "https://github.example.com/o/r.git", "github.example.com", true},
		{"file github", "file://github.com/share/repo.git", "", false},
		{"http github", "http://github.com/owner/repo.git", "", false},
		{"git scheme", "git://github.com/owner/repo.git", "", false},
		{"file url", "file:///srv/git/repo.git", "", false},
		{"local path", "/srv/git/repo.git", "", false},
		{"empty", "", "", false},
		{"separator only", "://", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			host, found := remoteHost(test.remote)
			if host != test.host || found != test.found {
				t.Fatalf("remoteHost(%q) = %q, %t, want %q, %t",
					test.remote, host, found, test.host, test.found)
			}
		})
	}
}

// TestDetectProviderRemotes maps origin remotes to the expected provider.
func TestDetectProviderRemotes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		remote   string
		provider Provider
		found    bool
	}{
		{"github https", "https://github.com/comarch/git-byline.git", ProviderGitHub, true},
		{"gitlab ssh", "ssh://git@gitlab.com/group/project.git", ProviderGitLab, true},
		{"unknown host", "https://example.com/owner/repo.git", "", false},
		{"file remote", "file://github.com/share/repo.git", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			runGit(t, root, "init", "-b", "main")
			runGit(t, root, "remote", "add", "origin", test.remote)
			provider, found := DetectProvider(root)
			if provider != test.provider || found != test.found {
				t.Fatalf("DetectProvider() = %q, %t, want %q, %t",
					provider, found, test.provider, test.found)
			}
		})
	}
}

// TestDetectProviderNoRemote covers a repository without an origin remote.
func TestDetectProviderNoRemote(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	if provider, found := DetectProvider(root); provider != "" || found {
		t.Fatalf("DetectProvider() = %q, %t, want none", provider, found)
	}
}

// TestDetectProviderOutsideRepository covers a directory outside any
// repository.
func TestDetectProviderOutsideRepository(t *testing.T) {
	t.Parallel()
	if provider, found := DetectProvider(t.TempDir()); provider != "" || found {
		t.Fatalf("DetectProvider() = %q, %t, want none", provider, found)
	}
}

// TestDetectProviderUnreadableConfig covers a local config Git cannot read.
func TestDetectProviderUnreadableConfig(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unreadable config needs a non-root POSIX user")
	}
	root := t.TempDir()
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "remote", "add", "origin",
		"https://github.com/comarch/git-byline.git")
	config := filepath.Join(root, ".git", "config")
	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(config, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(config, info.Mode().Perm()) })
	if provider, found := DetectProvider(root); provider != "" || found {
		t.Fatalf("DetectProvider() = %q, %t, want the unreadable config to report none",
			provider, found)
	}
}

// forgeFixture installs a template workflow and returns the removed-path
// expectation helpers use.
func forgeFixture(t *testing.T, root string, provider Provider) {
	t.Helper()
	if _, err := Install(root, provider); err != nil {
		t.Fatal(err)
	}
}

// TestUninstallRemovesTemplateWorkflows covers removal of both installed
// templates.
func TestUninstallRemovesTemplateWorkflows(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	forgeFixture(t, root, ProviderGitLab)
	removed, err := Uninstall(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".github/workflows/git-byline.yml", ".gitlab/ci/git-byline.yml"}
	if len(removed) != 2 || removed[0] != want[0] || removed[1] != want[1] {
		t.Fatalf("Uninstall() = %v, want %v", removed, want)
	}
	for _, relative := range want {
		if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("%s stat error = %v, want removed", relative, statErr)
		}
	}
}

// TestUninstallKeepsModifiedWorkflow covers a workflow that no longer
// matches the template.
func TestUninstallKeepsModifiedWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
	if err := os.WriteFile(path, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Fatalf("Uninstall() = %v, want none", removed)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("modified workflow removed: %v", statErr)
	}
}

// TestUninstallKeepsLongerWorkflow covers a workflow with content appended
// to the template, which no longer matches it.
func TestUninstallKeepsLongerWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if err != nil || len(removed) != 0 {
		t.Fatalf("Uninstall() = %v, %v, want the longer workflow kept", removed, err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("longer workflow removed: %v", statErr)
	}
}

// TestUninstallMissingWorkflows covers a root without workflow files.
func TestUninstallMissingWorkflows(t *testing.T) {
	t.Parallel()
	removed, err := Uninstall(t.TempDir())
	if err != nil || len(removed) != 0 {
		t.Fatalf("Uninstall() = %v, %v, want none", removed, err)
	}
}

// TestUninstallOpenRootFails covers an uninstall root that is missing.
func TestUninstallOpenRootFails(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing-root")
	if _, err := Uninstall(missing); err == nil ||
		!strings.Contains(err.Error(), "open CI uninstall root") {
		t.Fatalf("Uninstall(missing root) = %v, want open CI uninstall root error", err)
	}
}

// TestUninstallWorkflowParentNotDirectory covers a blocked parent path on
// every platform.
func TestUninstallWorkflowParentNotDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".github"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "is not a directory") {
		t.Fatalf("Uninstall() = %v, %v, want the blocked parent to fail", removed, err)
	}
}

// TestUninstallRefusesSymlinkedParent covers a symlinked path component.
func TestUninstallRefusesSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	forgeFixture(t, outside, ProviderGitHub)
	if err := os.Symlink(filepath.Join(outside, ".github"), filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "refusing symlinked CI workflow path") {
		t.Fatalf("Uninstall() = %v, %v, want the symlink refusal", removed, err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, ".github", "workflows", "git-byline.yml")); statErr != nil {
		t.Fatalf("workflow removed through the symlink: %v", statErr)
	}
}

// TestUninstallRefusesSymlinkedWorkflow covers a symlinked workflow file.
func TestUninstallRefusesSymlinkedWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	forgeFixture(t, outside, ProviderGitHub)
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(outside, ".github", "workflows", "git-byline.yml"),
		filepath.Join(root, ".github", "workflows", "git-byline.yml"),
	); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "refusing symlinked CI workflow path") {
		t.Fatalf("Uninstall() = %v, %v, want the symlink refusal", removed, err)
	}
	if _, statErr := os.Stat(filepath.Join(outside, ".github", "workflows", "git-byline.yml")); statErr != nil {
		t.Fatalf("workflow removed through the symlink: %v", statErr)
	}
}

// TestUninstallWorkflowNotRegularFile covers a directory at the workflow
// path.
func TestUninstallWorkflowNotRegularFile(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows", "git-byline.yml"), 0o755); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("Uninstall() = %v, %v, want the not-regular refusal", removed, err)
	}
}

// TestUninstallInspectFailsOnUnreadableParent covers a path Git cannot
// inspect through a modeless directory.
func TestUninstallInspectFailsOnUnreadableParent(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unreadable directories need a non-root POSIX user")
	}
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	workflows := filepath.Join(root, ".github", "workflows")
	info, err := os.Stat(workflows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workflows, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workflows, info.Mode().Perm()) })
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "inspect CI workflow path") {
		t.Fatalf("Uninstall() = %v, %v, want the inspect failure", removed, err)
	}
}

// TestUninstallRemoveFailsOnUnwritableDirectory covers a removal the file
// system rejects.
func TestUninstallRemoveFailsOnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unwritable directories need a non-root POSIX user")
	}
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	workflows := filepath.Join(root, ".github", "workflows")
	info, err := os.Stat(workflows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(workflows, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(workflows, info.Mode().Perm()) })
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "remove CI workflow") {
		t.Fatalf("Uninstall() = %v, %v, want the remove failure", removed, err)
	}
}

// TestUninstallPreservesRemovalsOnLaterFailure covers a run where one
// workflow is removed before a later provider fails.
func TestUninstallPreservesRemovalsOnLaterFailure(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	if err := os.WriteFile(filepath.Join(root, ".gitlab"), []byte("blocked"), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := Uninstall(root)
	if len(removed) != 1 || removed[0] != ".github/workflows/git-byline.yml" {
		t.Fatalf("Uninstall() removals = %v, want the github workflow", removed)
	}
	if err == nil {
		t.Fatal("Uninstall() error = nil, want the blocked gitlab path to fail")
	}
	if _, statErr := os.Stat(filepath.Join(root, ".github", "workflows", "git-byline.yml")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("github workflow still present: %v", statErr)
	}
}

// TestUninstallTemplateReadFails covers the template read failure branch.
func TestUninstallTemplateReadFails(t *testing.T) {
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	original := workflowTemplates
	workflowTemplates = embed.FS{}
	t.Cleanup(func() { workflowTemplates = original })
	_, err := Uninstall(root)
	assertErrorContains(t, err, "read embedded CI template")
}

// TestUninstallReadFailsOnUnreadableWorkflow covers a workflow the process
// cannot read.
func TestUninstallReadFailsOnUnreadableWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unreadable workflows need a non-root POSIX user")
	}
	root := t.TempDir()
	forgeFixture(t, root, ProviderGitHub)
	path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, info.Mode().Perm()) })
	removed, err := Uninstall(root)
	if len(removed) != 0 || err == nil ||
		!strings.Contains(err.Error(), "read CI workflow") {
		t.Fatalf("Uninstall() = %v, %v, want the read failure", removed, err)
	}
}
