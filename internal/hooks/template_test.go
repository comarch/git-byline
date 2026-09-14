package hooks

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
)

func TestMain(m *testing.M) {
	if os.Getenv("BYLINE_TEST_TEMPLATE_HOOK_HELPER") == "1" {
		path := os.Getenv("BYLINE_TEST_TEMPLATE_HOOK_LOG")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			os.Exit(1)
		}
		_, err = file.WriteString(strings.Join(os.Args[1:], " ") + "\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// templateEnv isolates the user-level Git configuration and the managed
// template directory. The global configuration is pinned to the XDG file
// so Git never falls back to the developer's own ~/.gitconfig.
func templateEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return xdg
}

func templateOptions(localNotes bool) Options {
	return Options{Agent: "none", Git: true, Template: true, LocalNotes: localNotes}
}

func TestInstallTemplateHooks(t *testing.T) {
	xdg := templateEnv(t)
	result, err := Install(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(xdg, "git-byline", "templates")
	assertTemplateChangedPaths(t, result, dir)
	assertTemplateConfig(t, result, dir)
	assertTemplateHooks(t, dir)
	assertTemplateStock(t, dir)

	result, err = Install(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 0 || result.ConfigChanged {
		t.Fatalf("second install changed = %+v", result)
	}
}

func assertTemplateChangedPaths(t *testing.T, result Result, dir string) {
	t.Helper()
	want := []string{
		filepath.Join(filepath.Dir(dir), templateStockMarker),
		filepath.Join(dir, "description"),
		filepath.Join(dir, "hooks", "post-checkout"),
		filepath.Join(dir, "hooks", "post-commit"),
		filepath.Join(dir, "hooks", "post-merge"),
		filepath.Join(dir, "hooks", "post-rewrite"),
		filepath.Join(dir, "hooks", "pre-push"),
		filepath.Join(dir, "hooks", "reference-transaction"),
		filepath.Join(dir, "info", "exclude"),
	}
	if len(result.Changed) != len(want) {
		t.Fatalf("Install changed = %v", result.Changed)
	}
	changed := strings.Join(result.Changed, "\n")
	for _, path := range want {
		if !strings.Contains(changed, path) {
			t.Fatalf("changed list missing %s: %v", path, result.Changed)
		}
	}
}

func assertTemplateConfig(t *testing.T, result Result, dir string) {
	t.Helper()
	if !result.ConfigChanged {
		t.Fatal("install did not report the init.templateDir change")
	}
	current, exists, err := gitcmd.GlobalConfig(templateConfigKey)
	if err != nil || !exists || current != dir {
		t.Fatalf("global config = %q, %v, %v", current, exists, err)
	}
}

func assertTemplateHooks(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{
		"post-commit", "pre-push", "post-rewrite", "post-merge",
		"post-checkout", "reference-transaction",
	} {
		path := filepath.Join(dir, "hooks", name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		assertExecutable(t, name, info)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(data), "#!/bin/sh\n") || !strings.Contains(string(data), blockStart) {
			t.Fatalf("%s = %q", name, data)
		}
	}
}

func assertTemplateStock(t *testing.T, dir string) {
	t.Helper()
	exclude, err := os.ReadFile(filepath.Join(dir, "info", "exclude"))
	if err != nil || string(exclude) != templateInfoExclude {
		t.Fatalf("info/exclude = %q, %v", exclude, err)
	}
}

func assertExecutable(t *testing.T, name string, info os.FileInfo) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if !info.Mode().IsRegular() {
			t.Fatalf("%s is not a regular executable hook: %v", name, info.Mode())
		}
		return
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("%s is not executable: %v", name, info.Mode())
	}
}

func TestInstallTemplateLocalNotes(t *testing.T) {
	xdg := templateEnv(t)
	result, err := Install(t.TempDir(), templateOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(result.Changed, "\n"), "pre-push") {
		t.Fatalf("local-notes template shipped a pre-push hook: %v", result.Changed)
	}
	if _, err := os.Stat(filepath.Join(xdg, "git-byline", "templates", "hooks", "pre-push")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-push exists: %v", err)
	}
}

func TestInstallTemplateRefusesForeignTemplateDir(t *testing.T) {
	xdg := templateEnv(t)
	foreign := filepath.Join(t.TempDir(), "custom-templates")
	if err := gitcmd.SetGlobalConfig(templateConfigKey, foreign); err != nil {
		t.Fatal(err)
	}
	_, err := Install(t.TempDir(), templateOptions(false))
	if err == nil || !strings.Contains(err.Error(), templateConfigKey) {
		t.Fatalf("install accepted a foreign template dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "git-byline")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("template dir was created: %v", err)
	}
	current, exists, err := gitcmd.GlobalConfig(templateConfigKey)
	if err != nil || !exists || current != foreign {
		t.Fatalf("global config was touched: %q, %v, %v", current, exists, err)
	}
}

func TestInstallTemplateRefusesIncludedForeignTemplateDir(t *testing.T) {
	xdg := templateEnv(t)
	foreign := filepath.Join(t.TempDir(), "custom-templates")
	included := filepath.Join(xdg, "included")
	if err := os.WriteFile(
		included,
		[]byte("[init]\n\ttemplateDir = "+foreign+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(xdg, "git", "config"),
		[]byte("[include]\n\tpath = "+included+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	_, err := Install(t.TempDir(), templateOptions(false))
	if err == nil || !strings.Contains(err.Error(), templateConfigKey) {
		t.Fatalf("install accepted an included foreign template dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(xdg, "git-byline")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("template dir was created: %v", err)
	}
}

func TestUninstallTemplateHooks(t *testing.T) {
	xdg := templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(xdg, "git-byline", "templates")
	if len(result.Changed) != len([]string{
		filepath.Join(filepath.Dir(dir), templateStockMarker),
		filepath.Join(dir, "description"),
		filepath.Join(dir, "hooks", "post-checkout"),
		filepath.Join(dir, "hooks", "post-commit"),
		filepath.Join(dir, "hooks", "post-merge"),
		filepath.Join(dir, "hooks", "post-rewrite"),
		filepath.Join(dir, "hooks", "pre-push"),
		filepath.Join(dir, "hooks", "reference-transaction"),
		filepath.Join(dir, "info", "exclude"),
	}) {
		t.Fatalf("uninstall changed = %v", result.Changed)
	}
	if !result.ConfigChanged {
		t.Fatal("uninstall did not report the init.templateDir removal")
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("template dir survived uninstall: %v", err)
	}
	if _, exists, err := gitcmd.GlobalConfig(templateConfigKey); err != nil || exists {
		t.Fatalf("global config survived uninstall: %v, %v", exists, err)
	}

	result, err = Uninstall(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 0 || result.ConfigChanged {
		t.Fatalf("second uninstall changed = %+v", result)
	}
}

func TestUninstallTemplateRemovesDuplicateManagedValues(t *testing.T) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, "git-byline", "templates")
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign")
	config := filepath.Join(xdg, "git", "config")
	if err := os.WriteFile(
		config,
		[]byte("[init]\n\ttemplateDir = "+foreign+"\n\ttemplateDir = "+dir+"\n\ttemplateDir = "+dir+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if !result.ConfigChanged {
		t.Fatal("uninstall did not report the managed value removal")
	}
	current, exists, err := gitcmd.GlobalConfig(templateConfigKey)
	if err != nil || !exists || current != foreign {
		t.Fatalf("remaining config = %q, %v, %v", current, exists, err)
	}
}

func TestUninstallTemplatePreservesForeignContent(t *testing.T) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, "git-byline", "templates")
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := "#!/bin/sh\necho mine\n"
	hookPath := filepath.Join(dir, "hooks", "post-commit")
	if err := os.WriteFile(hookPath, []byte(foreign), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(t.TempDir(), templateOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 9 {
		t.Fatalf("uninstall changed = %v", result.Changed)
	}
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != foreign {
		t.Fatalf("foreign hook = %q, want %q", data, foreign)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("template dir with foreign content was removed: %v", err)
	}
	if _, err := os.Stat(hookPath + ".git-byline.bak"); err != nil {
		t.Fatalf("uninstall backup missing: %v", err)
	}
}

func TestUninstallTemplatePreservesPreexistingStock(t *testing.T) {
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, "git-byline", "templates")
	exclude := filepath.Join(dir, "info", "exclude")
	description := filepath.Join(dir, "description")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte(templateInfoExclude), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(description, []byte(templateDescription), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{exclude, description} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preexisting stock file %s was removed: %v", path, err)
		}
	}
}

func TestTemplateStockRejectsSymlinkedInfoDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	xdg := templateEnv(t)
	dir := filepath.Join(xdg, "git-byline", "templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "info")); err != nil {
		t.Fatal(err)
	}

	_, err := Install(t.TempDir(), templateOptions(false))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("install accepted a symlinked info directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "exclude")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("install wrote through symlink: %v", err)
	}
}

func TestUninstallTemplateBoundsStockReads(t *testing.T) {
	xdg := templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	description := filepath.Join(xdg, "git-byline", "templates", "description")
	if err := os.WriteFile(description, make([]byte, maxHookBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(description); err != nil || info.Size() != maxHookBytes+1 {
		t.Fatalf("foreign description = %v, %v", info, err)
	}
}

func TestInstallTemplateIntoNewRepositories(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	templateEnv(t)
	if _, err := Install(t.TempDir(), templateOptions(false)); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	// Setup commits that do not exercise the copied hook use an empty
	// hooks directory. The fresh repository below runs the real hook.
	noHooks := filepath.Join(work, "no-hooks")
	if err := os.MkdirAll(noHooks, 0o755); err != nil {
		t.Fatal(err)
	}

	initRepo := filepath.Join(work, "fresh")
	templateGit(t, work, "init", "-q", "fresh")
	for _, relative := range []string{
		filepath.Join(".git", "hooks", "post-commit"),
		filepath.Join(".git", "info", "exclude"),
		filepath.Join(".git", "description"),
	} {
		info, err := os.Stat(filepath.Join(initRepo, relative))
		if err != nil {
			t.Fatalf("init did not copy %s: %v", relative, err)
		}
		if strings.HasSuffix(relative, "post-commit") {
			assertExecutable(t, relative, info)
		}
	}
	hookLog := filepath.Join(work, "hook.log")
	t.Setenv("BYLINE_TEST_TEMPLATE_HOOK_HELPER", "1")
	t.Setenv("BYLINE_TEST_TEMPLATE_HOOK_LOG", hookLog)
	templateGit(t, initRepo,
		"-c", "user.email=byline@example.com",
		"-c", "user.name=byline",
		"commit", "-q", "--allow-empty", "-m", "hook")
	hookCalls, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatalf("template hook did not run: %v", err)
	}
	if !bytes.Contains(hookCalls, []byte("annotate")) {
		t.Fatalf("template hook calls = %q", hookCalls)
	}

	src := filepath.Join(work, "src")
	templateGit(t, work, "init", "-q", "src")
	templateGit(t, src,
		"-c", "core.hooksPath="+noHooks,
		"-c", "user.email=byline@example.com",
		"-c", "user.name=byline",
		"commit", "-q", "--allow-empty", "-m", "x")
	clone := filepath.Join(work, "cloned")
	templateGit(t, work,
		"-c", "core.hooksPath="+noHooks,
		"clone", "-q", "file://"+src, "cloned")
	data, err := os.ReadFile(filepath.Join(clone, ".git", "hooks", "post-commit"))
	if err != nil {
		t.Fatalf("clone did not copy the hook: %v", err)
	}
	if !strings.Contains(string(data), blockStart) {
		t.Fatalf("cloned hook = %q", data)
	}
}

func TestInstallTemplateRefusesSymlinkedTemplateDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevated privileges on Windows")
	}
	xdg := templateEnv(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(xdg, "git-byline")); err != nil {
		t.Fatal(err)
	}
	_, err := Install(t.TempDir(), templateOptions(false))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("install accepted a symlinked template dir: %v", err)
	}
}

// templateGit runs one Git command that must see the isolated global
// configuration, without the repository-only defaults of hookGit.
func templateGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(templateGitEnvironment(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_BYLINE_NESTED=1",
		"LC_ALL=C",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

func templateGitEnvironment() []string {
	var environment []string
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if !strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			environment = append(environment, value)
		}
	}
	return environment
}
