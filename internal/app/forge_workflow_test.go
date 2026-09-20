package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallHooksForgeWorkflow(t *testing.T) {
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
			code, stdout, stderr, err := appRun(root, time.Time{}, nil,
				"install-hooks", "--agent", "none", "--git")
			if code != ExitSuccess || err != nil || stderr != "" ||
				!strings.Contains(stdout, test.wantLine) {
				t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
			}
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(test.wantPath))); statErr != nil {
				t.Fatalf("workflow %s: %v", test.wantPath, statErr)
			}
			code, stdout, stderr, err = appRun(root, time.Time{}, nil,
				"install-hooks", "--agent", "none", "--git")
			if code != ExitSuccess || err != nil || stderr != "" ||
				!strings.Contains(stdout, "workflow already installed at "+test.wantPath) {
				t.Fatalf("reinstall = %d, %q, %q, %v", code, stdout, stderr, err)
			}
		})
	}
}

func TestInstallHooksForgeWorkflowSkips(t *testing.T) {
	t.Parallel()
	t.Run("no remote prints hint", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "none", "--git")
		if code != ExitSuccess || err != nil || stderr != "" ||
			!strings.Contains(stdout, "no GitHub or GitLab remote detected") {
			t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
		}
		if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf(".github created without a forge remote: %v", statErr)
		}
	})
	t.Run("unknown remote prints hint", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		appGit(t, root, "remote", "add", "origin", "https://example.com/owner/repo.git")
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "none", "--git")
		if code != ExitSuccess || err != nil || stderr != "" ||
			!strings.Contains(stdout, "no GitHub or GitLab remote detected") {
			t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
		}
		if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf(".github created for an unsupported forge: %v", statErr)
		}
	})
	t.Run("local notes skips detection", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "none", "--git", "--local-notes")
		if code != ExitSuccess || err != nil || stderr != "" ||
			strings.Contains(stdout, "no GitHub or GitLab remote detected") ||
			strings.Contains(stdout, "created ") {
			t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
		}
		if _, statErr := os.Stat(filepath.Join(root, ".github")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf(".github created with --local-notes: %v", statErr)
		}
	})
	t.Run("agent only skips detection", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "droid")
		if code != ExitSuccess || err != nil ||
			strings.Contains(stdout, "no GitHub or GitLab remote detected") {
			t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
		}
	})
}

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
	root := t.TempDir()
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "none", "--git", "--template")
	if code != ExitSuccess || err != nil ||
		strings.Contains(stdout, "no GitHub or GitLab remote detected") ||
		strings.Contains(stdout, "created ") {
		t.Fatalf("install-hooks = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

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

func TestUninstallForgeWorkflow(t *testing.T) {
	t.Parallel()
	t.Run("removes managed workflow", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
		if code, _, _, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "none", "--git"); err != nil || code != ExitSuccess {
			t.Fatalf("install-hooks = %d, %v", code, err)
		}
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"uninstall", "--agent", "none", "--git")
		if code != ExitSuccess || err != nil || stderr != "" ||
			!strings.Contains(stdout, "updated .github/workflows/git-byline.yml") {
			t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
		}
		if _, statErr := os.Stat(filepath.Join(root, ".github", "workflows", "git-byline.yml")); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("workflow still present: %v", statErr)
		}
	})
	t.Run("keeps modified workflow", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		appGit(t, root, "remote", "add", "origin", "https://github.com/comarch/git-byline.git")
		if code, _, _, err := appRun(root, time.Time{}, nil,
			"install-hooks", "--agent", "none", "--git"); err != nil || code != ExitSuccess {
			t.Fatalf("install-hooks = %d, %v", code, err)
		}
		path := filepath.Join(root, ".github", "workflows", "git-byline.yml")
		if err := os.WriteFile(path, []byte("modified"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"uninstall", "--agent", "none", "--git")
		if code != ExitSuccess || err != nil || stderr != "" ||
			strings.Contains(stdout, "updated .github/workflows/git-byline.yml") {
			t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
		}
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("modified workflow removed: %v", statErr)
		}
	})
	t.Run("blocked path warns", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		if err := os.WriteFile(filepath.Join(root, ".github"), []byte("blocked"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"uninstall", "--agent", "none", "--git")
		if code != ExitSuccess || err != nil ||
			!strings.Contains(stderr, "warning: could not remove the forge workflow") {
			t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
		}
	})
	t.Run("agent only skips workflow removal", func(t *testing.T) {
		t.Parallel()
		root := appRepo(t)
		code, stdout, stderr, err := appRun(root, time.Time{}, nil,
			"uninstall", "--agent", "droid")
		if code != ExitSuccess || err != nil ||
			strings.Contains(stderr, "warning: could not remove the forge workflow") {
			t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
		}
	})
}

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
	if _, _, _, err := appRun(root, time.Time{}, nil,
		"install-hooks", "--agent", "none", "--git", "--template", "--local-notes"); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil,
		"uninstall", "--agent", "none", "--git", "--template")
	if code != ExitSuccess || err != nil ||
		strings.Contains(stdout, ".github/workflows/git-byline.yml") ||
		strings.Contains(stderr, "warning: could not remove the forge workflow") {
		t.Fatalf("uninstall = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}
