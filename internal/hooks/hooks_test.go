package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallAndUninstallProjectHooks(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	configPath := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"custom":true,"PreToolUse":[{"matcher":"Read","hooks":[]}]}`
	if err := os.WriteFile(configPath, []byte(existing), 0o640); err != nil {
		t.Fatal(err)
	}
	gitHook := filepath.Join(root, ".git", "hooks", "post-commit")
	originalHook := "#!/bin/sh\necho existing\n"
	if err := os.WriteFile(gitHook, []byte(originalHook), 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{Agent: "droid", Git: true}
	result, err := Install(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 2 {
		t.Fatalf("Install changed = %v", result.Changed)
	}
	config := readObject(t, configPath)
	if config["custom"] != true {
		t.Fatalf("custom config was not preserved: %+v", config)
	}
	if len(config["PreToolUse"].([]any)) != 2 || len(config["PostToolUse"].([]any)) != 1 {
		t.Fatalf("managed hooks missing: %+v", config)
	}
	managed := config["PostToolUse"].([]any)[0].(map[string]any)
	command := managed["hooks"].([]any)[0].(map[string]any)["command"]
	if command != agentSpecs("droid", "git-byline")["PostToolUse"].command {
		t.Fatalf("project hook command = %q", command)
	}
	hookData, err := os.ReadFile(gitHook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hookData), "echo existing") || !strings.Contains(string(hookData), blockStart) {
		t.Fatalf("hook = %q", hookData)
	}
	if _, err := os.Stat(configPath + ".git-byline.bak"); err != nil {
		t.Fatalf("config backup missing: %v", err)
	}
	if result, err := Install(root, options); err != nil || len(result.Changed) != 0 {
		t.Fatalf("second Install = %+v, %v", result, err)
	}

	result, err = Uninstall(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 2 {
		t.Fatalf("Uninstall changed = %v", result.Changed)
	}
	config = readObject(t, configPath)
	if config["custom"] != true || len(config["PreToolUse"].([]any)) != 1 {
		t.Fatalf("uninstall damaged config: %+v", config)
	}
	hookData, err = os.ReadFile(gitHook)
	if err != nil {
		t.Fatal(err)
	}
	if string(hookData) != originalHook {
		t.Fatalf("uninstalled hook = %q, want %q", hookData, originalHook)
	}
	if result, err := Uninstall(root, options); err != nil || len(result.Changed) != 0 {
		t.Fatalf("second Uninstall = %+v, %v", result, err)
	}
}

func TestInstallAllAgentsAndCustomHooksPath(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	hookGit(t, root, "config", "core.hooksPath", ".hooks")
	result, err := Install(root, Options{Agent: "all", Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 3 {
		t.Fatalf("Install changed = %v", result.Changed)
	}
	for _, path := range []string{
		filepath.Join(root, ".factory", "hooks.json"),
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".hooks", "post-commit"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s missing: %v", path, err)
		}
	}
	claude := readObject(t, filepath.Join(root, ".claude", "settings.json"))
	if _, ok := claude["hooks"].(map[string]any); !ok {
		t.Fatalf("Claude hooks are not nested under hooks: %+v", claude)
	}
}

func TestInvalidAgentConfigFailsClosed(t *testing.T) {
	t.Parallel()
	for _, invalid := range []string{"{bad", "{}{}"} {
		root := hookRepo(t)
		path := filepath.Join(root, ".factory", "hooks.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(root, Options{Agent: "droid"}); err == nil {
			t.Fatalf("Install accepted invalid JSON %q", invalid)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != invalid {
			t.Fatalf("invalid config changed: %q, %v", data, err)
		}
	}
}

func TestAgentConfigSymlinkFailsClosed(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	target := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Install(root, Options{Agent: "droid"}); err == nil {
		t.Fatal("Install accepted a symlinked configuration")
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "{}\n" {
		t.Fatalf("symlink target changed: %q, %v", data, err)
	}
}

func TestUninstallPreservesCustomHooksInManagedEntry(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": spec.matcher,
			"hooks": []any{
				map[string]any{"type": "command", "command": spec.command, "timeout": 30},
				map[string]any{"type": "command", "command": "echo custom"},
			},
		}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(root, Options{Agent: "droid"})
	if err != nil || len(result.Changed) != 1 {
		t.Fatalf("Uninstall() = %+v, %v", result, err)
	}
	got := readObject(t, path)
	entries := got["PreToolUse"].([]any)
	hookValues := entries[0].(map[string]any)["hooks"].([]any)
	if len(hookValues) != 1 || hookValues[0].(map[string]any)["command"] != "echo custom" {
		t.Fatalf("custom hooks were not preserved: %+v", got)
	}
}

func TestUninstallPreservesDifferentMatcher(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": "Read",
			"hooks":   []any{map[string]any{"type": "command", "command": spec.command}},
		}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(root, Options{Agent: "droid"})
	if err != nil || len(result.Changed) != 0 {
		t.Fatalf("Uninstall() = %+v, %v", result, err)
	}
}

func TestUserConfigScope(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("home directory environment differs on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	result, err := Install(root, Options{Agent: "droid", User: true})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".factory", "hooks.json")
	if len(result.Changed) != 1 || result.Changed[0] != want {
		t.Fatalf("Install changed = %v, want %s", result.Changed, want)
	}
}

func TestHookFailuresAndGitOnly(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	result, err := Install(root, Options{Agent: "none", Git: true})
	if err != nil || len(result.Changed) != 1 {
		t.Fatalf("Git-only Install = %+v, %v", result, err)
	}
	hook := filepath.Join(root, ".git", "hooks", "post-commit")
	data, err := os.ReadFile(hook)
	if err != nil || !strings.Contains(string(data), blockStart) {
		t.Fatalf("hook = %q, %v", data, err)
	}
	if _, err := Uninstall(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+blockStart+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
		t.Fatal("Install accepted incomplete managed block")
	}

	configPath := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"PreToolUse":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "droid"}); err == nil {
		t.Fatal("Install accepted non-array event")
	}
	claudePath := filepath.Join(root, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(claudePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudePath, []byte(`{"hooks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "claude"}); err == nil {
		t.Fatal("Install accepted non-object Claude hooks")
	}
}

func readObject(t *testing.T, path string) map[string]any {
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

func hookRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "init", "-b", "main")
	return root
}

func hookGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
