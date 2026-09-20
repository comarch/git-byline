package ci

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

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

func TestDetectProvider(t *testing.T) {
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
		{"file remote", "file:///srv/git/repo.git", "", false},
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
	t.Run("no remote", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		runGit(t, root, "init", "-b", "main")
		if provider, found := DetectProvider(root); provider != "" || found {
			t.Fatalf("DetectProvider() = %q, %t, want none", provider, found)
		}
	})
	t.Run("outside repository", func(t *testing.T) {
		t.Parallel()
		if provider, found := DetectProvider(t.TempDir()); provider != "" || found {
			t.Fatalf("DetectProvider() = %q, %t, want none", provider, found)
		}
	})
	t.Run("unreadable config", func(t *testing.T) {
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
	})
}

func TestUninstallForgeWorkflows(t *testing.T) {
	t.Parallel()
	t.Run("removes template files", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for _, provider := range []Provider{ProviderGitHub, ProviderGitLab} {
			if _, err := Install(root, provider); err != nil {
				t.Fatal(err)
			}
		}
		removed, err := Uninstall(root)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{".github/workflows/git-byline.yml", ".gitlab/ci/git-byline.yml"}
		if len(removed) != 2 || removed[0] != want[0] || removed[1] != want[1] {
			t.Fatalf("Uninstall() = %v, want %v", removed, want)
		}
		for _, relative := range want {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("%s stat error = %v, want removed", relative, err)
			}
		}
	})
	t.Run("keeps modified file", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if _, err := Install(root, ProviderGitHub); err != nil {
			t.Fatal(err)
		}
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
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("modified workflow removed: %v", err)
		}
	})
	t.Run("missing files", func(t *testing.T) {
		t.Parallel()
		removed, err := Uninstall(t.TempDir())
		if err != nil || len(removed) != 0 {
			t.Fatalf("Uninstall() = %v, %v, want none", removed, err)
		}
	})
	t.Run("workflow path blocked", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".github"), []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Uninstall(root); err == nil {
			t.Fatal("Uninstall() error = nil, want the blocked path to fail")
		}
	})
	t.Run("unwritable directory", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("unwritable directories need a non-root POSIX user")
		}
		root := t.TempDir()
		if _, err := Install(root, ProviderGitHub); err != nil {
			t.Fatal(err)
		}
		workflows := filepath.Join(root, ".github", "workflows")
		info, err := os.Stat(workflows)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(workflows, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(workflows, info.Mode().Perm()) })
		if _, err := Uninstall(root); err == nil {
			t.Fatal("Uninstall() error = nil, want the remove failure to surface")
		}
	})
}
