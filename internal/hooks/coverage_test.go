package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const (
	coverageRelativePath = "relative"
	coverageConfigJSON   = "settings.json"
	coverageForeignText  = "foreign\n"
	coverageInvalidJSON  = "{bad"
)

func TestCoverageEnvironmentErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", coverageRelativePath)
	file := templateStockFiles[0]
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "template directory", run: func() error {
			_, _, err := templateDir()
			return err
		}},
		{name: "acquire lock", run: func() error {
			lockFile, err := acquireTemplateLock()
			if lockFile != nil {
				_ = lockFile.Release()
			}
			return err
		}},
		{name: "hook path", run: func() error {
			_, err := templateHookPath("hook")
			return err
		}},
		{name: "prepare", run: func() error {
			_, err := prepareTemplate(true)
			return err
		}},
		{name: "finalize", run: func() error {
			return finalizeTemplate(true, nil)
		}},
		{name: "write config", run: func() error {
			return writeTemplateConfig(true)
		}},
		{name: "restore config", run: restoreTemplateConfig},
		{name: "open root", run: func() error {
			root, _, err := openTemplateRoot(true)
			if root != nil {
				_ = root.Close()
			}
			return err
		}},
		{name: "stock marker path", run: func() error {
			_, _, err := templateStockMarkerPath(file)
			return err
		}},
		{name: "legacy marker path", run: func() error {
			_, _, err := templateStockLegacyMarkerPath()
			return err
		}},
		{name: "legacy ownership", run: func() error {
			_, err := templateStockLegacyOwnership()
			return err
		}},
		{name: "read marker", run: func() error {
			_, _, err := readTemplateStockMarker(file)
			return err
		}},
		{name: "write marker", run: func() error {
			return writeTemplateStockMarker(file)
		}},
		{name: "remove marker", run: func() error {
			_, err := removeTemplateStockMarker(file)
			return err
		}},
		{name: "remove legacy marker", run: func() error {
			_, err := removeTemplateStockLegacyMarker()
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err == nil {
				t.Fatal("operation accepted relative XDG_CONFIG_HOME")
			}
		})
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	if runtime.GOOS != "windows" {
		t.Setenv("HOME", coverageRelativePath)
		if _, _, err := templateDir(); err == nil {
			t.Fatal("relative HOME was accepted")
		}
	}
	if _, err := agentConfigPath("", "unsupported", false); err == nil {
		t.Fatal("unsupported agent was accepted")
	}
}

func TestCoverageChangeErrorReturns(t *testing.T) {
	if _, err := changeUnlocked("", Options{Template: true, Agent: "droid"}, true); err == nil {
		t.Fatal("template project agent was accepted")
	}
	if _, err := changeUnlocked(t.TempDir(), Options{Agent: "droid"}, true); err == nil {
		t.Fatal("non-repository agent scope was accepted")
	}
	t.Setenv("XDG_CONFIG_HOME", coverageRelativePath)
	if _, err := changeUnlocked("", Options{Template: true}, true); err == nil {
		t.Fatal("relative template setup was accepted")
	}
}

func TestCoverageTemplateLockErrors(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.Mkdir(filepath.Join(xdg, templateLockName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := acquireTemplateLock(); err == nil {
		t.Fatal("template lock directory was accepted")
	}
	if _, err := changeTemplateLocked(t.TempDir(), templateOptions(false), true); err == nil {
		t.Fatal("template lock directory was accepted by change")
	}

	blockedBase := filepath.Join(t.TempDir(), coverageConfigJSON)
	if err := os.WriteFile(blockedBase, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blockedBase)
	if _, err := acquireTemplateLock(); err == nil {
		t.Fatal("regular XDG base was accepted")
	}
}

func TestCoverageTemplateRootErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("XDG_CONFIG_HOME", missing)
	root, exists, err := openTemplateRoot(false)
	if err != nil || exists || root != nil {
		t.Fatalf("missing base root = %v, %v, %v", root, exists, err)
	}
	if err := removeEmptyTemplateRoot(); err != nil {
		t.Fatal(err)
	}

	baseFile := filepath.Join(t.TempDir(), coverageConfigJSON)
	if err := os.WriteFile(baseFile, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", baseFile)
	if _, _, err := openTemplateRoot(false); err == nil {
		t.Fatal("regular base was opened")
	}
	if _, _, err := openTemplateRoot(true); err == nil {
		t.Fatal("regular base was created")
	}
	if err := writeTemplateStock(filepath.Join(baseFile, productName, "templates"), nil); err == nil {
		t.Fatal("stock write accepted regular base")
	}
	if err := pruneTemplate(filepath.Join(baseFile, productName, "templates"), nil); err == nil {
		t.Fatal("stock prune accepted regular base")
	}
	if err := removeEmptyTemplateRoot(); err == nil {
		t.Fatal("empty-root cleanup accepted regular base")
	}

	if runtime.GOOS == "windows" {
		return
	}
	xdg := templateEnv(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(xdg, productName)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := openTemplateRoot(false); err == nil {
		t.Fatal("symlinked template directory was accepted")
	}
}

func TestCoverageTemplateStockWriteErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission and symlink errors differ on Windows")
	}
	xdg := templateEnv(t)
	root, dir := coverageTemplateRoot(t)
	file := templateStockByRelative(t, "description")
	stockPath := filepath.Join(dir, filepath.FromSlash(file.relative))
	if err := os.MkdirAll(filepath.Dir(stockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, stockPath); err != nil {
		t.Fatal(err)
	}
	var changed []string
	if err := writeTemplateStockFile(root, dir, file, false, &changed); err == nil {
		t.Fatal("symlinked stock file was accepted")
	}
	_ = root.Close()

	xdg = templateEnv(t)
	root, dir = coverageTemplateRoot(t)
	file = templateStockByRelative(t, "description")
	if err := writeTemplateStockMarker(file); err != nil {
		t.Fatal(err)
	}
	stockPath = filepath.Join(dir, filepath.FromSlash(file.relative))
	if err := os.WriteFile(stockPath, []byte(file.content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stockPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := writeTemplateStockFile(root, dir, file, false, &changed); err == nil {
		t.Fatal("unreadable owned stock file was accepted")
	}
	_ = root.Close()

	root, _ = coverageTemplateRoot(t)
	info, err := os.Stat(filepath.Dir(stockPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := templateStockFileCurrent(root, "description", file, info); err == nil {
		t.Fatal("directory was accepted as owned stock")
	}
	if _, err := readRootFile(root, "missing", 1); err == nil {
		t.Fatal("missing rooted file was read")
	}
	_ = root.Close()

	invalidBase := filepath.Join(t.TempDir(), coverageConfigJSON)
	if err := os.WriteFile(invalidBase, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", invalidBase)
	unrelated, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := writeMissingTemplateStockFile(
		unrelated,
		filepath.Join(invalidBase, productName, "templates", file.relative),
		file,
		templateStockOwnership{},
		&changed,
	); err == nil {
		t.Fatal("marker write accepted regular base")
	}
	_ = unrelated.Close()

	xdg = templateEnv(t)
	root, _ = coverageTemplateRoot(t)
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeMissingTemplateStockFile(
		root,
		filepath.Join(xdg, productName, "templates", file.relative),
		file,
		templateStockOwnership{},
		&changed,
	); err == nil {
		t.Fatal("closed rooted file was written")
	}

	root, _ = coverageTemplateRoot(t)
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := createOwnedTemplateStockFile(
		root,
		filepath.Join(xdg, productName, "templates", file.relative),
		file,
		false,
		&changed,
	); err == nil {
		t.Fatal("closed owned stock file was written")
	}

	root, _ = coverageTemplateRoot(t)
	_, dir, _ = templateDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeRootFile(root, filepath.Join("regular", "child"), nil, 0o600); err == nil {
		t.Fatal("rooted write traversed regular component")
	}
	_ = root.Close()
}

func TestCoverageTemplatePruneErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission and symlink errors differ on Windows")
	}
	var changed []string
	coveragePruneMissingStock(t, &changed)
	coveragePruneEditedStock(t, &changed)
	coveragePruneSymlinkedStock(t, &changed)
	coveragePruneUnreadableStock(t, &changed)
	coveragePruneUnreadableMarker(t, &changed)
	coveragePruneSymlinkedStockDirectory(t, &changed)
	coveragePruneLegacyMarker(t, &changed)
	coveragePruneHooksPath(t, &changed)
	coveragePruneNonEmptyMarker(t, &changed)
	coveragePruneRegularRoot(t)
}

func coverageInstallTemplate(t *testing.T) {
	t.Helper()
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
}

func coveragePruneMissingStock(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	coverageInstallTemplate(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	markerPath, _, err := templateStockMarkerPath(file)
	if err != nil {
		t.Fatal(err)
	}
	stockPath := filepath.Join(dir, filepath.FromSlash(file.relative))
	root, _ := coverageTemplateRoot(t)
	if err := os.Remove(stockPath); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplateStockFile(root, dir, file, false, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing stock marker survived: %v", err)
	}
	_ = root.Close()
}

func coveragePruneEditedStock(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	stockPath := filepath.Join(dir, filepath.FromSlash(file.relative))
	coverageInstallTemplate(t)
	if err := os.WriteFile(stockPath, []byte(strings.Repeat("x", len(file.content))), 0o600); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	*changed = nil
	if err := pruneTemplateStockFile(root, dir, file, false, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stockPath); err != nil {
		t.Fatalf("edited stock was removed: %v", err)
	}
	_ = root.Close()
}

func coveragePruneSymlinkedStock(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	if _, _, err := templateStockMarkerPath(file); err != nil {
		t.Fatal(err)
	}
	stockPath := filepath.Join(dir, filepath.FromSlash(file.relative))
	coverageInstallTemplate(t)
	if err := os.Remove(stockPath); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, stockPath); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	if err := pruneTemplateStockFile(root, dir, file, false, changed); err == nil {
		t.Fatal("symlinked stock file was removed")
	}
	_ = root.Close()
}

func coveragePruneUnreadableStock(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	if _, _, err := templateStockMarkerPath(file); err != nil {
		t.Fatal(err)
	}
	stockPath := filepath.Join(dir, filepath.FromSlash(file.relative))
	coverageInstallTemplate(t)
	if err := os.WriteFile(stockPath, []byte(file.content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stockPath, 0); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	if err := pruneTemplateStockFile(root, dir, file, false, changed); err == nil {
		t.Fatal("unreadable stock file was accepted")
	}
	_ = root.Close()
}

func coveragePruneUnreadableMarker(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	markerPath, _, err := templateStockMarkerPath(file)
	if err != nil {
		t.Fatal(err)
	}
	coverageInstallTemplate(t)
	if err := os.Chmod(markerPath, 0); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	if err := pruneTemplateStockFile(root, dir, file, false, changed); err == nil {
		t.Fatal("unreadable stock marker was accepted")
	}
	_ = root.Close()
}

func coveragePruneSymlinkedStockDirectory(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	file := templateStockByRelative(t, "description")
	if _, _, err := templateStockMarkerPath(file); err != nil {
		t.Fatal(err)
	}
	coverageInstallTemplate(t)
	infoFile := templateStockByRelative(t, "info/exclude")
	if err := os.Remove(filepath.Join(dir, "info", "exclude")); err != nil {
		t.Fatal(err)
	}
	infoTarget := t.TempDir()
	if err := os.Remove(filepath.Join(dir, "info")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(infoTarget, filepath.Join(dir, "info")); err != nil {
		t.Fatal(err)
	}
	root, _ := coverageTemplateRoot(t)
	infoFile = templateStockByRelative(t, "info/exclude")
	if err := pruneTemplateStockFile(root, dir, infoFile, false, changed); err == nil {
		t.Fatal("symlinked stock directory was accepted")
	}
	_ = root.Close()
}

func coveragePruneLegacyMarker(t *testing.T, changed *[]string) {
	templateEnv(t)
	root, dir := coverageTemplateRoot(t)
	legacyPath := filepath.Join(filepath.Dir(dir), templateStockLegacyMarker)
	if err := os.MkdirAll(legacyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyPath, coverageConfigJSON), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplateStock(root, dir, true, changed); err == nil {
		t.Fatal("non-empty legacy marker was removed")
	}
	_ = root.Close()
}

func coveragePruneHooksPath(t *testing.T, changed *[]string) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(filepath.Dir(dir), templateStockLegacyMarker)
	if err := os.WriteFile(legacyPath, []byte(templateStockLegacyOwner), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(legacyPath, 0); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplate(dir, changed); err == nil {
		t.Fatal("unreadable legacy marker was accepted")
	}

	xdg = templateEnv(t)
	dir = filepath.Join(xdg, productName, "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := pruneTemplate(dir, changed); err == nil {
		t.Fatal("regular hooks path was accepted")
	}
}

func coveragePruneNonEmptyMarker(t *testing.T, changed *[]string) {
	templateEnv(t)
	file := templateStockByRelative(t, "description")
	markerPath, _, err := templateStockMarkerPath(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(markerPath, coverageConfigJSON), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := removeChangedTemplateStockMarker(file, true, changed); err == nil {
		t.Fatal("non-empty marker was removed")
	}
}

func coveragePruneRegularRoot(t *testing.T) {
	xdg := templateEnv(t)
	root, _ := coverageTemplateRoot(t)
	regular := filepath.Join(xdg, productName, "templates", coverageConfigJSON)
	if err := os.WriteFile(regular, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeEmptyRootDir(root, coverageConfigJSON); err == nil {
		t.Fatal("regular root path was accepted as directory")
	}
	_ = root.Close()
}

func TestCoverageTemplateConfigErrors(t *testing.T) {
	xdg := templateEnv(t)
	configPath := filepath.Join(xdg, "git", "config")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTemplateConfig(true); err == nil {
		t.Fatal("directory git config was accepted")
	}

	xdg = templateEnv(t)
	dir := filepath.Join(xdg, productName, "templates")
	included := filepath.Join(xdg, "included")
	foreign := filepath.Join(xdg, "foreign")
	templateGit(t, xdg, "config", "--file", included, "--add", templateConfigKey, dir)
	templateGit(t, xdg, "config", "--file", included, "--add", templateConfigKey, foreign)
	templateGit(
		t,
		xdg,
		"config",
		"--file",
		filepath.Join(xdg, "git", "config"),
		"include.path",
		included,
	)
	if err := writeTemplateConfig(true); err == nil {
		t.Fatal("included foreign template value was replaced")
	}
}

func TestCoverageTemplateRootHelpers(t *testing.T) {
	templateEnv(t)
	root, _ := coverageTemplateRoot(t)
	temp, err := root.OpenFile("partial", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		_ = root.Close()
		t.Fatal(err)
	}
	if err := finishRootFileWrite(root, temp, "partial", false, errors.New("write failed")); err == nil {
		_ = root.Close()
		t.Fatal("failed rooted write was accepted")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageAgentShapes(t *testing.T) {
	spec := agentSpecs("droid", productName)["PreToolUse"][0]
	managed := managedEntry(spec)
	tests := []struct {
		name  string
		value any
	}{
		{name: "scalar", value: coverageForeignText},
		{name: "different matcher", value: map[string]any{"matcher": "other"}},
		{name: "invalid hooks", value: map[string]any{"matcher": spec.matcher, "hooks": coverageForeignText}},
	}
	for _, test := range tests {
		t.Run("update "+test.name, func(t *testing.T) {
			if _, managed, _ := updateManagedHook(test.value, spec); managed {
				t.Fatal("invalid managed hook was accepted")
			}
		})
		t.Run("remove "+test.name, func(t *testing.T) {
			if _, removed, _ := removeManagedHook(test.value, spec); removed {
				t.Fatal("invalid managed hook was removed")
			}
		})
	}

	duplicate := map[string]any{
		"matcher": spec.matcher,
		"hooks": []any{
			managed["hooks"].([]any)[0],
			managedEntry(spec)["hooks"].([]any)[0],
			coverageForeignText,
		},
	}
	if _, managed, current := updateManagedHook(duplicate, spec); !managed || current {
		t.Fatalf("duplicate managed hook result = %t, %t", managed, current)
	}
	if _, removed, _ := removeManagedHook(duplicate, spec); !removed {
		t.Fatal("duplicate managed hook was not removed")
	}
}

func TestCoverageChangeAgentConfigBranches(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, coverageConfigJSON)
	claudeSpec := agentSpecs("claude", productName)["PreToolUse"][0]
	claudeConfig := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{managedEntry(claudeSpec)},
		},
	}
	writeCoverageJSON(t, claudePath, claudeConfig)
	changed, err := changeAgentConfig(claudePath, "claude", productName, false)
	if err != nil || !changed {
		t.Fatalf("Claude uninstall = %t, %v", changed, err)
	}
	if got := readCoverageJSON(t, claudePath); len(got) != 0 {
		t.Fatalf("empty Claude hooks survived: %+v", got)
	}

	legacyPath := filepath.Join(root, "legacy.json")
	writeCoverageJSON(t, legacyPath, map[string]any{
		"hooks": map[string]any{"PreToolUse": coverageForeignText},
	})
	if _, err := changeAgentConfig(legacyPath, "droid", productName, false); err == nil {
		t.Fatal("invalid legacy hooks were accepted")
	}

	deletePath := filepath.Join(root, "delete.json")
	writeCoverageJSON(t, deletePath, map[string]any{
		"hooks": map[string]any{"PreToolUse": []any{managedEntry(agentSpecs("droid", productName)["PreToolUse"][0])}},
	})
	changed, err = changeAgentConfig(deletePath, "droid", productName, false)
	if err != nil || !changed {
		t.Fatalf("legacy uninstall = %t, %v", changed, err)
	}
	if _, ok := readCoverageJSON(t, deletePath)["hooks"]; ok {
		t.Fatal("empty legacy hooks survived")
	}

	backupPath := filepath.Join(root, "backup.json")
	writeCoverageJSON(t, backupPath, map[string]any{})
	if err := os.Mkdir(backupPath+".git-byline.bak", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeAgentConfig(backupPath, "droid", productName, true); err == nil {
		t.Fatal("backup directory was accepted during install")
	}

	t.Run("uninstall with existing backup directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("existing backup directory errors differ on Windows")
		}
		uninstallBackup := filepath.Join(root, "uninstall.json")
		writeCoverageJSON(t, uninstallBackup, map[string]any{
			"PreToolUse": []any{managedEntry(agentSpecs("droid", productName)["PreToolUse"][0])},
		})
		if err := os.Mkdir(uninstallBackup+".git-byline.bak", 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := changeAgentConfig(uninstallBackup, "droid", productName, false); err != nil {
			t.Fatal(err)
		}
	})

	parent := filepath.Join(root, coverageConfigJSON)
	if err := os.WriteFile(parent, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := changeAgentConfig(filepath.Join(parent, "child.json"), "droid", productName, true); err == nil {
		t.Fatal("regular config parent was accepted")
	}
}

func TestCoverageJSONBranches(t *testing.T) {
	dir := t.TempDir()
	tailPath := filepath.Join(dir, "tail.json")
	if err := os.WriteFile(tailPath, []byte("{}\n{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readJSONObject(tailPath); err == nil {
		t.Fatal("invalid JSON tail was accepted")
	}
	multiplePath := filepath.Join(dir, "multiple.json")
	if err := os.WriteFile(multiplePath, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readJSONObject(multiplePath); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
	nullPath := filepath.Join(dir, "null.json")
	if err := os.WriteFile(nullPath, []byte("null\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := readJSONObject(nullPath); err == nil {
		t.Fatal("null JSON root was accepted")
	}
	if _, _, _, err := readJSONObject(filepath.Join(dir, "missing.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageBackupAndCappedFileErrors(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(source, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createBackup(source, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCappedFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing capped file was read")
	}
	if _, err := readCappedFile(dir); err == nil {
		t.Fatal("directory capped file was read")
	}
	large := filepath.Join(dir, "large")
	if err := os.WriteFile(large, make([]byte, maxHookBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCappedFile(large); err == nil {
		t.Fatal("oversized capped file was read")
	}
	t.Run("directory backup", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("existing backup directory errors differ on Windows")
		}
		backupDir := source + ".git-byline.bak"
		if err := os.Remove(backupDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(backupDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := createBackup(source, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := refreshBackup(source, 0o600); err == nil {
			t.Fatal("refresh over backup directory was accepted")
		}
	})
	if err := refreshBackup(filepath.Join(dir, "missing"), 0o600); err == nil {
		t.Fatal("missing refresh source was accepted")
	}
}

func TestCoverageGitPathAndSafetyErrors(t *testing.T) {
	if _, err := gitHookPath(&gitcmd.Repo{}, "post-commit"); err == nil {
		t.Fatal("invalid repository was accepted")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file"), []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rejectSymlinkPath(root, filepath.Join(root, "file", "child")); err == nil {
		t.Fatal("regular path component was accepted")
	}
	if err := rejectSymlinkPath(root, filepath.Join(root, "..", "outside")); err == nil {
		t.Fatal("escaping path was accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	if err := rejectSymlinkPath(root, filepath.Join(blocked, "child")); err == nil {
		t.Fatal("unreadable path component was accepted")
	}
}

func TestCoverageAtomicWriteErrors(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "regular")
	if err := os.WriteFile(regular, []byte(coverageForeignText), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(regular, "child"), nil, 0o600); err == nil {
		t.Fatal("regular parent was accepted")
	}
	targetDir := filepath.Join(dir, "target")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(targetDir, nil, 0o600); err == nil {
		t.Fatal("directory replacement was accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	readOnly := filepath.Join(dir, "readonly")
	if err := os.Mkdir(readOnly, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(readOnly, coverageConfigJSON), nil, 0o600); err == nil {
		t.Fatal("read-only temporary directory was accepted")
	}
}

func TestCoverageGitHookShapes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook")
	command := rewriteHookCommand(productName, "post-checkout", false)
	if err := os.WriteFile(path, []byte("#!/bin/sh\r\n"+blockStart+"\r\n"+command+"\r\n"+blockEnd+"\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(path, command, true); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(path, command, false); err != nil {
		t.Fatal(err)
	}

	noNewline := filepath.Join(dir, "no-newline")
	if err := os.WriteFile(noNewline, []byte("#!/bin/sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(noNewline, command, true); err != nil {
		t.Fatal(err)
	}

	multiple := filepath.Join(dir, "multiple")
	block := blockStart + "\n" + command + "\n" + blockEnd
	if err := os.WriteFile(multiple, []byte("#!/bin/sh\n"+block+"\n"+block+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(multiple, command, true); err == nil {
		t.Fatal("multiple managed blocks were accepted")
	}

	binaryPath := filepath.Join(dir, "binary")
	if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n\x00"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(binaryPath, command, true); err == nil {
		t.Fatal("binary hook was accepted")
	}

	if runtime.GOOS == "windows" {
		return
	}
	permissionPath := filepath.Join(dir, "permission")
	if err := os.WriteFile(permissionPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(permissionPath, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := changeGitHook(permissionPath, command, true); err == nil {
		t.Fatal("unreadable hook was accepted")
	}
}

func TestCoverageShellAndCommandRecognition(t *testing.T) {
	tests := []struct {
		data []byte
		want bool
	}{
		{data: []byte("echo no\n"), want: false},
		{data: []byte("#!\n"), want: false},
		{data: []byte("#!/usr/bin/env\n"), want: false},
		{data: []byte("#!/usr/bin/env sh\n"), want: true},
		{data: []byte("#!/bin/python\n"), want: false},
	}
	for _, test := range tests {
		if got := isShellHook(test.data); got != test.want {
			t.Errorf("isShellHook(%q) = %t, want %t", test.data, got, test.want)
		}
	}
	signature := " checkpoint droid --type ai --hook-input stdin"
	if commandHasSingleExecutable(`'/path' bad'`+signature, "") {
		t.Fatal("unquoted command whitespace was accepted")
	}
	if managedGitHookBlock(blockStart + "\ninvalid\n" + blockEnd) {
		t.Fatal("invalid managed block was accepted")
	}
}

func coverageTemplateRoot(t *testing.T) (*os.Root, string) {
	t.Helper()
	_, dir, err := templateDir()
	if err != nil {
		t.Fatal(err)
	}
	root, exists, err := openTemplateRoot(true)
	if err != nil || !exists {
		t.Fatalf("template root = %v, %v, %v", root, exists, err)
	}
	return root, dir
}

func writeCoverageJSON(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCoverageJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}
