package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const (
	coverageFakeGitForeign             = "foreign"
	coverageFakeGitReadErrorAfterUnset = "read-error-after-unset"
)

const coverageFakeGitScript = `#!/bin/sh
case "$*" in
  *"--get-all $BYLINE_FAKE_GIT_KEY"*)
    case "$BYLINE_FAKE_GIT_MODE" in
      read-error|read-error-with-unset-error)
        exit 2
        ;;
      read-error-after-unset)
        if [ -f "$BYLINE_FAKE_GIT_STATE" ]; then
          exit 2
        fi
        exit 1
        ;;
      restore)
        if [ -f "$BYLINE_FAKE_GIT_STATE" ]; then
          printf '%s\000' "$BYLINE_FAKE_GIT_VALUE"
          exit 0
        fi
        exit 1
        ;;
      restore-join)
        printf '%s\000' "$BYLINE_FAKE_GIT_VALUE"
        exit 0
        ;;
      foreign)
        if [ -f "$BYLINE_FAKE_GIT_STATE" ]; then
          printf '%s\000' "$BYLINE_FAKE_GIT_VALUE"
          exit 0
        fi
        exit 1
        ;;
    esac
    exit 1
    ;;
  *"--unset-all $BYLINE_FAKE_GIT_KEY"*)
    case "$BYLINE_FAKE_GIT_MODE" in
      read-error-with-unset-error)
        exit 2
        ;;
      restore)
        rm -f "$BYLINE_FAKE_GIT_STATE"
        exit 0
        ;;
      read-error-after-unset)
        rm -f "$BYLINE_FAKE_GIT_STATE"
        exit 0
        ;;
      restore-join)
        exit 0
        ;;
      foreign)
        rm -f "$BYLINE_FAKE_GIT_STATE"
        exit 0
        ;;
    esac
    exit 0
    ;;
  *"--add $BYLINE_FAKE_GIT_KEY"*)
    case "$BYLINE_FAKE_GIT_MODE" in
      restore|restore-join)
        exit 2
        ;;
    esac
    exit 0
    ;;
esac
exit 0
`

func TestCoverageTemplateRootPermissionFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	xdg := templateEnv(t)
	productDir := filepath.Join(xdg, productName)
	if err := os.Mkdir(productDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(productDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(productDir, 0o700)
	})
	if _, _, err := openTemplateRoot(true); err == nil {
		t.Fatal("read-only template parent was accepted")
	}
}

func TestCoverageTemplateConfigVerificationFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	tests := []struct {
		name string
		mode string
	}{
		{name: "remove succeeds", mode: coverageFakeGitReadErrorAfterUnset},
		{name: "remove fails", mode: "read-error-with-unset-error"},
		{name: "foreign value is refused", mode: coverageFakeGitForeign},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			templateEnv(t)
			_, dir, err := templateDir()
			if err != nil {
				t.Fatal(err)
			}
			value := dir
			if test.mode == coverageFakeGitForeign {
				value = filepath.Join(t.TempDir(), "foreign")
			}
			setupCoverageFakeGit(
				t,
				test.mode,
				value,
				test.mode == coverageFakeGitForeign || test.mode == coverageFakeGitReadErrorAfterUnset,
			)
			if err := writeTemplateConfig(true); err == nil {
				t.Fatal("configuration verification failure was accepted")
			}
		})
	}
}

func TestCoverageTemplateConfigRestoreFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	t.Run("template change restore", func(t *testing.T) {
		templateEnv(t)
		_, dir, err := templateDir()
		if err != nil {
			t.Fatal(err)
		}
		setupCoverageFakeGit(t, "restore", dir, true)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "hooks"), []byte("foreign\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := changeTemplateLocked(
			t.TempDir(),
			Options{Git: true, Template: true},
			false,
		); err == nil {
			t.Fatal("template change failure was accepted")
		}
	})

	t.Run("config removal restore", func(t *testing.T) {
		templateEnv(t)
		_, dir, err := templateDir()
		if err != nil {
			t.Fatal(err)
		}
		setupCoverageFakeGit(t, "restore-join", dir, true)
		if err := writeTemplateConfig(false); err == nil {
			t.Fatal("config removal failure was accepted")
		}
	})
}

func TestCoverageGitHookPathRejectsUnresolvedSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, ".hooks")); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "git")
	const fakeGit = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$BYLINE_HOOK_ROOT" ;;
  *"--git-common-dir"*) printf '%s\n' "$BYLINE_HOOK_GIT" ;;
  *"--git-dir"*) printf '%s\n' "$BYLINE_HOOK_GIT" ;;
  *"--git-path hooks"*) printf '%s\n' "$BYLINE_HOOK_ROOT/.hooks" ;;
esac
`
	if err := os.WriteFile(script, []byte(fakeGit), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("BYLINE_HOOK_ROOT", root)
	t.Setenv("BYLINE_HOOK_GIT", filepath.Join(root, ".git"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitHookPath(repo, "post-commit"); err == nil {
		t.Fatal("symlinked hooks path was accepted")
	}
}

func setupCoverageFakeGit(t *testing.T, mode, value string, withState bool) {
	t.Helper()
	root := t.TempDir()
	script := filepath.Join(root, "git")
	if err := os.WriteFile(script, []byte(coverageFakeGitScript), 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	if withState {
		if err := os.WriteFile(state, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("BYLINE_FAKE_GIT_KEY", templateConfigKey)
	t.Setenv("BYLINE_FAKE_GIT_MODE", mode)
	t.Setenv("BYLINE_FAKE_GIT_STATE", state)
	t.Setenv("BYLINE_FAKE_GIT_VALUE", value)
}
