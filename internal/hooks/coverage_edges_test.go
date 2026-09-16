package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
)

func TestCoverageTemplateConfigReadFailures(t *testing.T) {
	xdg := templateEnv(t)
	invalid := filepath.Join(xdg, "invalid")
	if err := os.WriteFile(invalid, []byte(coverageInvalidJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), []byte(coverageInvalidJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareTemplate(true); err == nil {
		t.Fatal("invalid global config was accepted")
	}

	templateEnv(t)
	if got, err := prepareTemplate(false); err != nil || got {
		t.Fatalf("unconfigured uninstall = %t, %v", got, err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign")
	if err := gitcmd.SetGlobalConfig(templateConfigKey, foreign); err != nil {
		t.Fatal(err)
	}
	if got, err := prepareTemplate(false); err != nil || got {
		t.Fatalf("foreign uninstall = %t, %v", got, err)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if _, _, err := templateDir(); err == nil {
		t.Fatal("missing HOME was accepted")
	}
	if _, err := agentConfigPath("", "droid", true); err == nil {
		t.Fatal("missing user home was accepted")
	}
}

func TestCoverageTemplateConfigWriteFailures(t *testing.T) {
	xdg := templateEnv(t)
	config := filepath.Join(xdg, "git", "config")
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(config, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTemplateConfig(true); err == nil {
		t.Fatal("unwritable global config was accepted")
	}

	xdg = templateEnv(t)
	config = filepath.Join(xdg, "git", "config")
	included := filepath.Join(xdg, "included")
	dir := filepath.Join(xdg, productName, "templates")
	templateGit(t, xdg, "config", "--file", included, "--add", templateConfigKey, dir)
	templateGit(t, xdg, "config", "--file", included, "--add", templateConfigKey, filepath.Join(xdg, "foreign"))
	templateGit(
		t,
		xdg,
		"config",
		"--file",
		config,
		"include.path",
		included,
	)
	if err := writeTemplateConfig(true); err == nil {
		t.Fatal("included conflicting config was accepted")
	}

	xdg = templateEnv(t)
	t.Run("read-only global config directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory permissions differ on Windows")
		}
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory write permissions")
		}
		if err := os.Chmod(filepath.Join(xdg, "git"), 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(filepath.Join(xdg, "git"), 0o700)
		})
		if _, err := Install(t.TempDir(), templateOptions(false)); err == nil {
			t.Fatal("read-only global config directory was accepted")
		}
	})
}

func TestCoverageAcquireTemplateLockFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	xdg := templateEnv(t)
	t.Cleanup(func() {
		_ = os.Chmod(xdg, 0o700)
	})
	if err := os.Chmod(xdg, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireTemplateLock(); err == nil {
		t.Fatal("read-only template base was accepted")
	}
}

func TestCoverageLegacyMarkerFailures(t *testing.T) {
	t.Run("unreadable legacy marker", coverageLegacyMarkerUnreadable)
	coverageLegacyMarkerMissing(t)
	coverageLegacyMarkerRegularParent(t)
	coverageLegacyMarkerInvalid(t)
}

func coverageLegacyMarkerUnreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	legacy := filepath.Join(filepath.Dir(dir), templateStockLegacyMarker)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(templateStockLegacyOwner), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacy, 0); err != nil {
		t.Fatal(err)
	}
	if err := writeTemplateStock(dir, new([]string)); err == nil {
		t.Fatal("unreadable legacy marker was accepted")
	}
}

func coverageLegacyMarkerMissing(t *testing.T) {
	xdg := templateEnv(t)
	if err := os.RemoveAll(xdg); err != nil {
		t.Fatal(err)
	}
	if owned, err := templateStockLegacyOwnership(); err != nil || owned {
		t.Fatalf("missing base ownership = %t, %v", owned, err)
	}
	if exists, valid, err := readTemplateStockMarker(templateStockFiles[0]); err != nil || exists || valid {
		t.Fatalf("missing base marker = %t, %t, %v", exists, valid, err)
	}
}

func coverageLegacyMarkerRegularParent(t *testing.T) {
	xdg := templateEnv(t)
	if err := os.WriteFile(filepath.Join(xdg, productName), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	// Only unix rejects traversal through a regular file with the
	// error these helpers report.
	if runtime.GOOS != "windows" {
		if _, err := templateStockLegacyOwnership(); err == nil {
			t.Fatal("regular template marker parent was accepted")
		}
		if _, _, err := readTemplateStockMarker(templateStockFiles[0]); err == nil {
			t.Fatal("regular template marker parent was accepted")
		}
	}
}

func coverageLegacyMarkerInvalid(t *testing.T) {
	xdg := templateEnv(t)
	file := templateStockFiles[0]
	legacy := filepath.Join(xdg, productName, templateStockLegacyMarker)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if owned, err := templateStockLegacyOwnership(); err != nil || owned {
		t.Fatalf("invalid legacy marker = %t, %v", owned, err)
	}
	if err := os.WriteFile(filepath.Join(xdg, productName, templateStockMarkerPrefix+file.marker), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if exists, valid, err := readTemplateStockMarker(file); err != nil || !exists || valid {
		t.Fatalf("invalid stock marker = %t, %t, %v", exists, valid, err)
	}
}

func TestCoverageMarkerPathErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", coverageRelativePath)
	file := templateStockFiles[0]
	if err := ensureTemplateStockMarker(file, false, new([]string)); err == nil {
		t.Fatal("relative marker path was accepted")
	}
	if _, _, err := readTemplateStockMarker(file); err == nil {
		t.Fatal("relative marker read was accepted")
	}
	if err := writeTemplateStockMarker(file); err == nil {
		t.Fatal("relative marker write was accepted")
	}
	if _, err := removeTemplateStockMarker(file); err == nil {
		t.Fatal("relative marker removal was accepted")
	}
	if _, err := removeTemplateStockLegacyMarker(); err == nil {
		t.Fatal("relative legacy marker removal was accepted")
	}

	base := filepath.Join(t.TempDir(), coverageConfigJSON)
	if err := os.WriteFile(base, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", base)
	if _, err := templateStockLegacyOwnership(); err == nil {
		t.Fatal("regular marker base was accepted")
	}
	if _, _, err := readTemplateStockMarker(file); err == nil {
		t.Fatal("regular marker base was accepted")
	}
	if err := writeTemplateStockMarker(file); err == nil {
		t.Fatal("regular marker base was accepted")
	}
	if _, err := removeTemplateStockMarker(file); err == nil {
		t.Fatal("regular marker base was accepted")
	}
	if _, err := removeTemplateStockLegacyMarker(); err == nil {
		t.Fatal("regular marker base was accepted")
	}
}

func TestCoverageStockHelperErrors(t *testing.T) {
	base := filepath.Join(t.TempDir(), coverageConfigJSON)
	if err := os.WriteFile(base, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", base)
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := templateStockFiles[0]
	dir := filepath.Join(base, productName, "templates")
	if err := writeTemplateStockFile(root, dir, file, false, new([]string)); err == nil {
		t.Fatal("regular marker base was accepted during stock write")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageStockReadAndRemoveFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	_, dir, err := templateDir()
	if err != nil {
		t.Fatal(err)
	}
	file := templateStockByRelative(t, "description")
	marker, _, err := templateStockMarkerPath(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(marker, 0); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	if err := writeTemplateStockFile(root, dir, file, false, new([]string)); err == nil {
		_ = root.Close()
		t.Fatal("unreadable stock marker was accepted")
	}
	_ = root.Close()

	templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	_, dir, err = templateDir()
	if err != nil {
		t.Fatal(err)
	}
	root, _ = coverageTemplateRoot(t)
	if err := os.Chmod(dir, 0o500); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
	})
	if err := pruneTemplateStockFile(root, dir, file, false, new([]string)); err == nil {
		t.Fatal("read-only stock root accepted removal")
	}
	_ = root.Close()
}

func TestCoverageRootOperationErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regular"), []byte(coverageForeignText), 0o600); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := removeEmptyRootDir(root, "regular"); err == nil {
		_ = root.Close()
		t.Fatal("regular root entry was accepted")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	xdg := templateEnv(t)
	if err := os.MkdirAll(filepath.Join(xdg, productName), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, productName, "templates"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyTemplateRoot(); err == nil {
		t.Fatal("regular template entry was accepted")
	}

	xdg = templateEnv(t)
	if err := os.WriteFile(filepath.Join(xdg, productName), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openTemplateRoot(true); err == nil {
		t.Fatal("regular template parent was accepted")
	}

	xdg = templateEnv(t)
	dir = filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	markerDir := filepath.Join(filepath.Dir(dir), templateStockMarkerPrefix+templateStockFiles[0].marker)
	if err := os.Mkdir(markerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTemplateStockMarker(templateStockFiles[0]); err == nil {
		t.Fatal("directory marker was accepted")
	}
}

func TestCoverageMarkerAndStockPathFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(filepath.Dir(dir), templateStockLegacyMarker)
	if err := os.WriteFile(legacy, []byte(templateStockLegacyOwner), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, productName, "templates", "info"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	file := templateStockByRelative(t, "info/exclude")
	if err := writeTemplateStockFile(mustTemplateRoot(t), dir, file, true, new([]string)); err == nil {
		t.Fatal("regular stock path component was accepted")
	}

	xdg = templateEnv(t)
	dir = filepath.Join(xdg, productName, "templates")
	if err := os.WriteFile(filepath.Join(xdg, productName), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := templateStockLegacyOwnership(); err == nil {
		t.Fatal("regular legacy marker parent was accepted")
	}
	if _, _, err := readTemplateStockMarker(templateStockFiles[0]); err == nil {
		t.Fatal("regular marker parent was accepted")
	}
}

func TestCoveragePruneFailureReturns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	xdg := templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	path := filepath.Join(dir, filepath.FromSlash(file.relative))
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), path); err != nil {
		t.Fatal(err)
	}
	changed := []string{}
	if err := pruneTemplate(dir, &changed); err == nil {
		t.Fatal("template prune accepted symlinked stock")
	}

	xdg = templateEnv(t)
	dir = filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	info := templateStockByRelative(t, "info/exclude")
	if err := writeTemplateStockMarker(info); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "info"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplateStockFile(mustTemplateRoot(t), dir, info, false, &changed); err == nil {
		t.Fatal("prune accepted regular stock path")
	}

	xdg = templateEnv(t)
	dir = filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "child"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplate(dir, &changed); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageRootCloseAndCleanupFailures(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	_ = closeTemplateRoot(root, nil)

	root, err = os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("partial", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("partial/child", 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := root.Open("partial")
	if err != nil {
		t.Fatal(err)
	}
	if err := finishRootFileWrite(root, file, "partial", true, errors.New("write failed")); err == nil {
		t.Fatal("non-empty incomplete directory was accepted")
	}
	partial, err := root.OpenFile("closed", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := finishRootFileWrite(root, partial, "closed", false, errors.New("write failed")); err == nil {
		_ = root.Close()
		t.Fatal("closed partial file was accepted")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageClosedRootBranches(t *testing.T) {
	templateEnv(t)
	root, _ := coverageTemplateRoot(t)
	file := templateStockFiles[0]
	if err := writeTemplateStockMarker(file); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	changed := []string{}
	if err := writeTemplateStockFile(root, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), productName, "templates"), file, false, &changed); err == nil {
		t.Fatal("closed root stock write was accepted")
	}
	if err := pruneTemplateStockFile(root, filepath.Join(os.Getenv("XDG_CONFIG_HOME"), productName, "templates"), file, false, &changed); err == nil {
		t.Fatal("closed root stock prune was accepted")
	}
}

func TestCoverageRootDirectoryFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := mkdirAllRoot(root, "new", 0o700); err == nil {
		t.Fatal("read-only rooted directory accepted")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	root, err = os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "regular"), []byte(coverageForeignText), 0o600); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := mkdirAllRoot(root, filepath.Join("regular", "child"), 0o700); err == nil {
		t.Fatal("regular rooted component accepted")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	if err := removeEmptyRootDirMustFail(dir); err == nil {
		t.Fatal("invalid rooted directory was accepted")
	}

	templateEnv(t)
	xdg, _, _ := templateDir()
	if err := os.RemoveAll(xdg); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyTemplateRoot(); err != nil {
		t.Fatal(err)
	}
	xdg = templateEnv(t)
	if err := os.WriteFile(filepath.Join(xdg, productName), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyTemplateRoot(); err == nil {
		t.Fatal("regular template root was accepted")
	}
}

func TestCoverageRootRemovalFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := root.Mkdir("empty", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
	})
	if err := removeEmptyRootDir(root, "empty"); err == nil {
		t.Fatal("read-only root removed directory")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	templateEnv(t)
	if err := removeEmptyTemplateRoot(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", coverageRelativePath)
	if err := removeEmptyTemplateRoot(); err == nil {
		t.Fatal("relative template root was accepted")
	}

	xdg := templateEnv(t)
	template := filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(template, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(xdg, productName), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(filepath.Join(xdg, productName), 0o700)
	})
	if err := removeEmptyTemplateRoot(); err == nil {
		t.Fatal("read-only template parent removed root")
	}
}

func TestCoverageChangeAgentNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), coverageConfigJSON)
	changed, err := changeAgentConfig(path, "droid", productName, false)
	if err != nil || changed {
		t.Fatalf("uninstall of missing config = %t, %v", changed, err)
	}
}

func TestCoverageAgentAtomicWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission behavior differs on Windows")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
	})
	path := filepath.Join(dir, coverageConfigJSON)
	if _, err := changeAgentConfig(path, "droid", productName, true); err == nil {
		t.Fatal("read-only config directory was accepted")
	}
}

func TestCoverageCommandAndHookErrors(t *testing.T) {
	command := rewriteHookCommand(productName, "post-checkout", false)
	coverageCommandRecognitionErrors(t, command)

	dir := t.TempDir()
	coverageHookPathErrors(t, dir, command)
	block := blockStart + "\r\n" + command + "\r\n" + blockEnd
	coverageHookUpdateErrors(t, dir, command, block)
	t.Run("read-only hook directory", func(t *testing.T) {
		coverageReadOnlyHookDirectory(t, dir, command, block)
	})
}

func coverageCommandRecognitionErrors(t *testing.T, command string) {
	if commandHasSingleExecutable(`'/path' bad'`, "") {
		t.Fatal("command with outside whitespace was accepted")
	}
	if managedGitHookBlock(blockStart + "\ninvalid") {
		t.Fatal("malformed managed block was accepted")
	}
}

func coverageHookPathErrors(t *testing.T, dir, command string) {
	path := filepath.Join(dir, "hook")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(path, command, true); err == nil {
		t.Fatal("directory hook was accepted")
	}
	if _, err := changeGitHook("bad\x00", command, true); err == nil {
		t.Fatal("invalid hook stat was accepted")
	}
}

func coverageHookUpdateErrors(t *testing.T, dir, command, block string) {
	nonShell := filepath.Join(dir, "non-shell")
	if err := os.WriteFile(nonShell, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(nonShell, command, true); err != nil {
		t.Fatal(err)
	}
	crlf := filepath.Join(dir, "crlf")
	if err := os.WriteFile(crlf, []byte("#!/bin/sh\r\n"+block+"\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(crlf, command, false); err != nil {
		t.Fatal(err)
	}
}

func coverageReadOnlyHookDirectory(t *testing.T, dir, command, block string) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	readonly := filepath.Join(dir, "readonly")
	t.Cleanup(func() {
		_ = os.Chmod(readonly, 0o700)
	})
	if err := os.Mkdir(readonly, 0o700); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(readonly, "hook")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+blockStart+"\n"+command+"\n"+blockEnd+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	installHook := filepath.Join(readonly, "install")
	stale := rewriteHookCommand("/old/git-byline", "post-checkout", false)
	if err := os.WriteFile(installHook, []byte("#!/bin/sh\n"+blockStart+"\n"+stale+"\n"+blockEnd+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	uninstallHook := filepath.Join(readonly, "uninstall")
	if err := os.WriteFile(uninstallHook, []byte("#!/bin/sh\n"+blockStart+"\n"+command+"\n"+blockEnd+"\necho custom\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	atomicHook := filepath.Join(readonly, "atomic")
	if err := os.WriteFile(atomicHook, []byte("#!/bin/sh\n"+blockStart+"\n"+command+"\n"+blockEnd+"\necho custom\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(atomicHook+".git-byline.bak", []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(hook, command, false); err == nil {
		t.Fatal("read-only generated hook was removed")
	}
	if _, err := changeGitHook(installHook, command, true); err == nil {
		t.Fatal("read-only backup refresh was accepted")
	}
	if _, err := changeGitHook(uninstallHook, command, false); err == nil {
		t.Fatal("read-only backup creation was accepted")
	}
	if _, err := changeGitHook(atomicHook, command, false); err == nil {
		t.Fatal("read-only atomic hook replacement was accepted")
	}
}

func TestCoverageBackupSymlinkFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink and permission behavior differs on Windows")
	}
	realDir := t.TempDir()
	source := filepath.Join(realDir, "source")
	if err := os.WriteFile(source, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	readonly := t.TempDir()
	t.Cleanup(func() {
		_ = os.Chmod(readonly, 0o700)
	})
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(readonly, link); err != nil {
		t.Fatal(err)
	}
	sourceThroughLink := filepath.Join(link, "source")
	if err := os.Symlink(source, sourceThroughLink); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(sourceThroughLink, 0o600); err == nil {
		t.Fatal("backup through read-only symlink target was accepted")
	}
	if err := refreshBackup(sourceThroughLink, 0o600); err == nil {
		t.Fatal("refresh through read-only symlink target was accepted")
	}
	if err := createBackup(filepath.Join(realDir, "missing"), 0o600); err == nil {
		t.Fatal("missing backup source was accepted")
	}
	if err := createBackup(realDir, 0o600); err == nil {
		t.Fatal("directory backup source was accepted")
	}
}

func TestCoverageGitHookPathRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink behavior differs on Windows")
	}
	root := hookRepo(t)
	target := t.TempDir()
	link := filepath.Join(root, ".hooks")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "config", "core.hooksPath", ".hooks")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitHookPath(repo, "post-commit"); err == nil {
		t.Fatal("symlinked hooks path was accepted")
	}

	hookGit(t, root, "config", "--unset", "core.hooksPath")
	realHooks := filepath.Join(root, ".git", "hooks")
	if err := os.RemoveAll(realHooks); err != nil {
		t.Fatal(err)
	}
	target = t.TempDir()
	if err := os.Symlink(target, realHooks); err != nil {
		t.Fatal(err)
	}
	repo, err = gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitHookPath(repo, "post-commit"); err == nil {
		t.Fatal("symlinked repository hooks path was accepted")
	}
}

func TestCoverageWriteResourceFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory write permissions")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "backup-source")
	if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A read-only parent directory is deterministic and process-local:
	// unlike RLIMIT_FSIZE it cannot break Go's internal testlog.txt
	// appends, which run in the same process as the test.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(dir, 0o700)
	})

	if err := atomicWrite(filepath.Join(dir, "atomic"), []byte("x"), 0o600); err == nil {
		t.Fatal("read-only directory did not reject atomic write")
	}
	if err := createBackup(source, 0o600); err == nil {
		t.Fatal("read-only directory did not reject backup creation")
	}
	if err := refreshBackup(source, 0o600); err == nil {
		t.Fatal("read-only directory did not reject backup refresh")
	}
	if _, err := changeAgentConfig(filepath.Join(dir, "agent.json"), "droid", productName, true); err == nil {
		t.Fatal("read-only directory did not reject agent config write")
	}
	if _, err := changeGitHook(filepath.Join(dir, "hook"), postCommitCommand(productName), true); err == nil {
		t.Fatal("read-only directory did not reject hook write")
	}

	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// An existing directory at the rooted target forces the exclusive
	// open to fail inside writeRootFile.
	if err := os.Mkdir(filepath.Join(dir, "root-file"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := writeRootFile(root, "root-file", []byte("x"), 0o600); err == nil {
		t.Fatal("existing directory did not reject rooted write")
	}
}

func mustTemplateRoot(t *testing.T) *os.Root {
	t.Helper()
	root, exists, err := openTemplateRoot(true)
	if err != nil || !exists {
		t.Fatalf("template root = %v, %v, %v", root, exists, err)
	}
	return root
}

func removeEmptyRootDirMustFail(base string) error {
	root, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer root.Close()
	return removeEmptyRootDir(root, "\x00")
}
