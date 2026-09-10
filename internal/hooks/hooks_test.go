package hooks

import (
	"encoding/json"
	"errors"
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
	pushHook := filepath.Join(root, ".git", "hooks", "pre-push")
	originalHook := "#!/bin/sh\necho existing\n"
	originalPushHook := "#!/bin/sh\necho existing push\n"
	if err := os.WriteFile(gitHook, []byte(originalHook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pushHook, []byte(originalPushHook), 0o755); err != nil {
		t.Fatal(err)
	}
	options := Options{Agent: "droid", Git: true}
	result, err := Install(root, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 3 {
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
	pushData, err := os.ReadFile(pushHook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(pushData), "echo existing push") ||
		!strings.Contains(string(pushData), "refs/notes/byline:refs/notes/byline") ||
		!strings.Contains(string(pushData), "git push --no-verify -- \"$1\"") {
		t.Fatalf("pre-push hook = %q", pushData)
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
	if len(result.Changed) != 3 {
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
	pushData, err = os.ReadFile(pushHook)
	if err != nil {
		t.Fatal(err)
	}
	if string(pushData) != originalPushHook {
		t.Fatalf("uninstalled pre-push hook = %q, want %q", pushData, originalPushHook)
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
	if len(result.Changed) != 4 {
		t.Fatalf("Install changed = %v", result.Changed)
	}
	for _, path := range []string{
		filepath.Join(root, ".factory", "hooks.json"),
		filepath.Join(root, ".claude", "settings.json"),
		filepath.Join(root, ".hooks", "post-commit"),
		filepath.Join(root, ".hooks", "pre-push"),
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
	parentRoot := hookRepo(t)
	parentTarget := t.TempDir()
	if err := os.Symlink(parentTarget, filepath.Join(parentRoot, ".factory")); err != nil {
		t.Skipf("parent symlink unavailable: %v", err)
	}
	if _, err := Install(parentRoot, Options{Agent: "droid"}); err == nil {
		t.Fatal("Install accepted a symlinked configuration parent")
	}
	entries, err := os.ReadDir(parentTarget)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("configuration parent target changed: %v", entries)
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

func TestUninstallPreservesManagedEntryMetadata(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher":      spec.matcher,
			"commandRegex": "^edit",
			"hooks":        []any{map[string]any{"type": "command", "command": spec.command}},
		}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root, Options{Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	entry := readObject(t, path)["PreToolUse"].([]any)[0].(map[string]any)
	if entry["commandRegex"] != "^edit" || len(entry["hooks"].([]any)) != 0 {
		t.Fatalf("matcher metadata changed: %+v", entry)
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

func TestInstallDroidUsesRootSchema(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if _, err := Install(root, Options{Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	config := readObject(t, path)
	if _, nested := config["hooks"]; nested {
		t.Fatalf("Droid hooks unexpectedly nested: %+v", config)
	}
	if len(config["PreToolUse"].([]any)) != 1 || len(config["PostToolUse"].([]any)) != 1 {
		t.Fatalf("root Droid hooks missing: %+v", config)
	}
}

func TestInstallDroidPreservesNestedValues(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"hooks":{"custom":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	config := readObject(t, path)
	if len(config["PreToolUse"].([]any)) != 1 || len(config["PostToolUse"].([]any)) != 1 {
		t.Fatalf("root Droid hooks missing: %+v", config)
	}
	nested, ok := config["hooks"].(map[string]any)
	if !ok || nested["custom"] != true || len(nested) != 1 {
		t.Fatalf("nested values changed: %+v", config)
	}
}

func TestUninstallRemovesLegacyNestedDroidHooks(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	config := map[string]any{
		"hooks": map[string]any{
			"custom": true,
			"PreToolUse": []any{map[string]any{
				"matcher": spec.matcher,
				"hooks":   []any{map[string]any{"type": "command", "command": spec.command}},
			}},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(root, Options{Agent: "droid"}); err != nil {
		t.Fatal(err)
	}
	nested := readObject(t, path)["hooks"].(map[string]any)
	if nested["custom"] != true || len(nested) != 1 {
		t.Fatalf("legacy nested hooks not cleaned: %+v", nested)
	}
}

func TestCustomHookWithManagedSuffixIsPreserved(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	spec := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	custom := "audit-wrapper && " + spec.command
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": spec.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": custom}},
		}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := Uninstall(root, Options{Agent: "droid"}); err != nil || len(result.Changed) != 0 {
		t.Fatalf("Uninstall() = %+v, %v", result, err)
	}
	got := readObject(t, path)
	command := got["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"]
	if command != custom {
		t.Fatalf("custom wrapper = %q, want %q", command, custom)
	}
}

func TestLegacyManagedAgentHookIsMigrated(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	current := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	legacy := strings.Replace(current.command, " --managed-by git-byline", "", 1)
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": current.matcher,
			"hooks":   []any{map[string]any{"type": "command", "command": legacy, "timeout": 30}},
		}},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if result, err := Install(root, Options{Agent: "droid"}); err != nil || len(result.Changed) != 1 {
		t.Fatalf("Install() = %+v, %v", result, err)
	}
	got := readObject(t, path)
	command := got["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)[0].(map[string]any)["command"].(string)
	if !strings.Contains(command, "--managed-by git-byline") || strings.Contains(command, "/old/git-byline") {
		t.Fatalf("migrated command = %q", command)
	}
}

func TestManagedAgentHookUpdatesInPlace(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".factory", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := agentSpecs("droid", "/old/git-byline")["PreToolUse"]
	config := map[string]any{
		"PreToolUse": []any{map[string]any{
			"matcher": old.matcher,
			"hooks": []any{
				map[string]any{"type": "command", "command": "echo before"},
				map[string]any{"type": "command", "command": old.command, "timeout": 30},
				map[string]any{"type": "command", "command": "echo after"},
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
	if result, err := Install(root, Options{Agent: "droid"}); err != nil || len(result.Changed) != 1 {
		t.Fatalf("Install() = %+v, %v", result, err)
	}
	got := readObject(t, path)
	hooks := got["PreToolUse"].([]any)[0].(map[string]any)["hooks"].([]any)
	commands := make([]string, 0, len(hooks))
	for _, value := range hooks {
		commands = append(commands, value.(map[string]any)["command"].(string))
	}
	want := []string{"echo before", agentSpecs("droid", "git-byline")["PreToolUse"].command, "echo after"}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %q, want %q", commands, want)
	}
	if result, err := Install(root, Options{Agent: "droid"}); err != nil || len(result.Changed) != 0 {
		t.Fatalf("second Install() = %+v, %v", result, err)
	}
}

func TestSingleExecutableCommandRecognition(t *testing.T) {
	t.Parallel()
	signature := " checkpoint droid --type ai --hook-input stdin"
	tests := []struct {
		command string
		want    bool
	}{
		{`'/path with space/git-byline'` + signature, true},
		{`"C:\Program Files\git-byline.exe"` + signature, true},
		{`'/path/audit-wrapper'` + signature, true},
		{`audit-wrapper && '/path/git-byline'` + signature, false},
		{`'/path/git-byline'; echo unsafe` + signature, false},
	}
	for _, test := range tests {
		if got := commandHasSingleExecutable(test.command, signature); got != test.want {
			t.Errorf("commandHasSingleExecutable(%q) = %t, want %t", test.command, got, test.want)
		}
	}
}

func TestManagedAgentCommandRequiresMarkerOrBinary(t *testing.T) {
	t.Parallel()
	spec := agentSpecs("droid", "/old/git-byline")["PostToolUse"]
	signature := spec.command[strings.Index(spec.command, " checkpoint "):]
	legacy := strings.Replace(signature, " --managed-by git-byline", "", 1)
	if managedAgentCommand(`'/path/audit-wrapper'`+legacy, spec) {
		t.Fatal("managedAgentCommand accepted unmarked custom executable")
	}
	if !managedAgentCommand(`'/path/audit-wrapper'`+signature, spec) {
		t.Fatal("managedAgentCommand rejected explicit ownership marker")
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
	if err != nil || len(result.Changed) != 2 {
		t.Fatalf("Git-only Install = %+v, %v", result, err)
	}
	hook := filepath.Join(root, ".git", "hooks", "post-commit")
	data, err := os.ReadFile(hook)
	if err != nil || !strings.Contains(string(data), blockStart) {
		t.Fatalf("hook = %q, %v", data, err)
	}
	pushHook := filepath.Join(root, ".git", "hooks", "pre-push")
	data, err = os.ReadFile(pushHook)
	if err != nil || !strings.Contains(string(data), "refs/notes/byline") {
		t.Fatalf("pre-push hook = %q, %v", data, err)
	}
	if _, err := Uninstall(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{hook, pushHook} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("uninstalled hook %s still exists: %v", path, err)
		}
	}
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n"+blockStart+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
		t.Fatal("Install accepted incomplete managed block")
	}
	for _, content := range []string{
		"#!/bin/sh\n" + blockStart + "\necho custom\n" + blockEnd + "\n",
		"#!/bin/sh\n" + blockEnd + "\n" + blockStart + "\n",
	} {
		if err := os.WriteFile(hook, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
			t.Fatalf("Install accepted unrecognized managed block %q", content)
		}
		if got := string(mustRead(t, hook)); got != content {
			t.Fatalf("unrecognized managed block changed to %q", got)
		}
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

func TestLocalNotesOptOut(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	defaults := Options{Agent: "none", Git: true}
	if result, err := Install(root, defaults); err != nil || len(result.Changed) != 2 {
		t.Fatalf("Install(defaults) = %+v, %v", result, err)
	}
	prePush := filepath.Join(root, ".git", "hooks", "pre-push")
	if _, err := os.Stat(prePush); err != nil {
		t.Fatal(err)
	}
	local := Options{Agent: "none", Git: true, LocalNotes: true}
	if result, err := Install(root, local); err != nil || len(result.Changed) != 1 {
		t.Fatalf("Install(local) = %+v, %v", result, err)
	}
	if _, err := os.Stat(prePush); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("local notes left pre-push hook: %v", err)
	}
	if result, err := Install(root, local); err != nil || len(result.Changed) != 0 {
		t.Fatalf("second Install(local) = %+v, %v", result, err)
	}
	if result, err := Install(root, defaults); err != nil || len(result.Changed) != 1 {
		t.Fatalf("restore sharing = %+v, %v", result, err)
	}
}

func TestInstallUpdatesManagedGitHook(t *testing.T) {
	t.Parallel()
	root := hookRepo(t)
	path := filepath.Join(root, ".git", "hooks", "pre-push")
	stale := "#!/bin/sh\nexit 0\n\n" + blockStart + "\n" + notesPushCommand() + "\n" + blockEnd + "\n"
	if err := os.WriteFile(path, []byte(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	result, err := Install(root, Options{Agent: "none", Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 2 {
		t.Fatalf("Install changed = %v", result.Changed)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "#!/bin/sh\n"+blockStart) ||
		!strings.Contains(string(data), notesPushCommand()) ||
		!strings.Contains(string(data), "exit 0") {
		t.Fatalf("updated hook = %q", data)
	}
	if result, err := Install(root, Options{Agent: "none", Git: true}); err != nil || len(result.Changed) != 0 {
		t.Fatalf("second Install() = %+v, %v", result, err)
	}
}

func TestGitHookSafetyAndLinkedWorktree(t *testing.T) {
	t.Parallel()
	t.Run("non-shell", func(t *testing.T) {
		root := hookRepo(t)
		path := filepath.Join(root, ".git", "hooks", "post-commit")
		original := "#!/usr/bin/env python3\nprint('custom')\n"
		if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
			t.Fatal("Install accepted non-shell hook")
		}
		if got := string(mustRead(t, path)); got != original {
			t.Fatalf("non-shell hook changed to %q", got)
		}
	})
	t.Run("pre-existing shell stub", func(t *testing.T) {
		root := hookRepo(t)
		path := filepath.Join(root, ".git", "hooks", "post-commit")
		original := "#!/bin/sh\n"
		if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
			t.Fatal(err)
		}
		if _, err := Uninstall(root, Options{Agent: "none", Git: true}); err != nil {
			t.Fatal(err)
		}
		if got := string(mustRead(t, path)); got != original {
			t.Fatalf("pre-existing hook changed to %q", got)
		}
	})
	t.Run("unmanaged non-shell uninstall", func(t *testing.T) {
		root := hookRepo(t)
		original := "#!/usr/bin/env python3\nprint('custom')\n"
		for _, name := range []string{"post-commit", "pre-push"} {
			path := filepath.Join(root, ".git", "hooks", name)
			if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		result, err := Uninstall(root, Options{Agent: "none", Git: true})
		if err != nil || len(result.Changed) != 0 {
			t.Fatalf("Uninstall() = %+v, %v", result, err)
		}
		if got := string(mustRead(t, filepath.Join(root, ".git", "hooks", "pre-push"))); got != original {
			t.Fatalf("pre-push hook changed to %q", got)
		}
	})
	t.Run("local notes preserves unmanaged non-shell pre-push", func(t *testing.T) {
		root := hookRepo(t)
		if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".git", "hooks", "pre-push")
		original := "#!/usr/bin/env python3\nprint('custom')\n"
		if err := os.WriteFile(path, []byte(original), 0o755); err != nil {
			t.Fatal(err)
		}
		result, err := Install(root, Options{Agent: "none", Git: true, LocalNotes: true})
		if err != nil || len(result.Changed) != 0 {
			t.Fatalf("Install(local notes) = %+v, %v", result, err)
		}
		if got := string(mustRead(t, path)); got != original {
			t.Fatalf("pre-push hook changed to %q", got)
		}
	})
	t.Run("symlinked custom hooks path", func(t *testing.T) {
		root := hookRepo(t)
		target := t.TempDir()
		link := filepath.Join(root, ".hooks")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		hookGit(t, root, "config", "core.hooksPath", ".hooks")
		if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
			t.Fatal("Install accepted symlinked hooks directory")
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("symlink target changed: %v", entries)
		}
	})
	t.Run("external custom hooks path", func(t *testing.T) {
		root := hookRepo(t)
		target := t.TempDir()
		hookGit(t, root, "config", "core.hooksPath", target)
		if _, err := Install(root, Options{Agent: "none", Git: true}); err == nil {
			t.Fatal("Install accepted external hooks directory")
		}
		entries, err := os.ReadDir(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("external hooks directory changed: %v", entries)
		}
	})
	t.Run("linked worktree", func(t *testing.T) {
		root := hookRepo(t)
		hookGit(t, root, "config", "user.name", "Test User")
		hookGit(t, root, "config", "user.email", "test@example.invalid")
		if err := os.WriteFile(filepath.Join(root, "file"), []byte("base\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		hookGit(t, root, "add", "file")
		hookGit(t, root, "commit", "-m", "base")
		linked := filepath.Join(t.TempDir(), "linked")
		hookGit(t, root, "worktree", "add", "-b", "linked", linked)
		if _, err := Install(linked, Options{Agent: "none", Git: true}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"post-commit", "pre-push"} {
			if _, err := os.Stat(filepath.Join(root, ".git", "hooks", name)); err != nil {
				t.Fatalf("common %s missing: %v", name, err)
			}
		}
	})
}

func TestPrePushSharesNotes(t *testing.T) {
	t.Parallel()
	root, remote, _ := pushFixture(t)
	if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "push", "origin", "main")
	localNote := strings.TrimSpace(hookGit(t, root, "rev-parse", "refs/notes/byline"))
	remoteNote := strings.TrimSpace(hookGit(t, remote, "rev-parse", "refs/notes/byline"))
	if localNote != remoteNote {
		t.Fatalf("remote note = %q, want %q", remoteNote, localNote)
	}
	localHead := strings.TrimSpace(hookGit(t, root, "rev-parse", "HEAD"))
	remoteHead := strings.TrimSpace(hookGit(t, remote, "rev-parse", "refs/heads/main"))
	if localHead != remoteHead {
		t.Fatalf("remote HEAD = %q, want %q", remoteHead, localHead)
	}
}

func TestPrePushRunsCustomHookOnce(t *testing.T) {
	t.Parallel()
	root, _, _ := pushFixture(t)
	hook := filepath.Join(root, ".git", "hooks", "pre-push")
	script := "#!/bin/sh\nprintf 'run\\n' >> .git/pre-push-runs\n"
	if err := os.WriteFile(hook, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "push", "origin", "main")
	data, err := os.ReadFile(filepath.Join(root, ".git", "pre-push-runs"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "run\n" {
		t.Fatalf("custom pre-push runs = %q", data)
	}
}

func TestPrePushStopsBranchWhenNotesFail(t *testing.T) {
	t.Parallel()
	root, remote, oldRemoteHead := pushFixture(t)
	hook := filepath.Join(remote, "hooks", "pre-receive")
	script := "#!/bin/sh\nwhile read old new ref; do\n" +
		"  if [ \"$ref\" = refs/notes/byline ]; then exit 1; fi\n" +
		"done\n"
	if err := os.WriteFile(hook, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "push", "origin", "main")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("push succeeded despite rejected notes: %s", out)
	}
	remoteHead := strings.TrimSpace(hookGit(t, remote, "rev-parse", "refs/heads/main"))
	if remoteHead != oldRemoteHead {
		t.Fatalf("remote HEAD advanced to %q, want %q", remoteHead, oldRemoteHead)
	}
	if command := exec.Command("git", "show-ref", "--verify", "refs/notes/byline"); func() bool {
		command.Dir = remote
		return command.Run() == nil
	}() {
		t.Fatal("remote received rejected attribution notes")
	}
}

func TestPrePushRejectsDivergentNotes(t *testing.T) {
	t.Parallel()
	remote := filepath.Join(t.TempDir(), "remote.git")
	first := hookRepo(t)
	hookGit(t, first, "config", "user.name", "Test User")
	hookGit(t, first, "config", "user.email", "test@example.invalid")
	hookGit(t, first, "init", "--bare", remote)
	if err := os.WriteFile(filepath.Join(first, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookGit(t, first, "add", "file.txt")
	hookGit(t, first, "commit", "-m", "base")
	base := strings.TrimSpace(hookGit(t, first, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(first, "file.txt"), []byte("base\nchange\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookGit(t, first, "add", "file.txt")
	hookGit(t, first, "commit", "-m", "change")
	head := strings.TrimSpace(hookGit(t, first, "rev-parse", "HEAD"))
	hookGit(t, first, "remote", "add", "origin", remote)
	hookGit(t, first, "push", "-u", "origin", "main")

	second := filepath.Join(t.TempDir(), "second")
	hookGit(t, t.TempDir(), "clone", "--branch", "main", remote, second)
	hookGit(t, second, "config", "user.name", "Test User")
	hookGit(t, second, "config", "user.email", "test@example.invalid")
	hookGit(t, first, "notes", "--ref=refs/notes/byline", "add", "-m", "first note", base)
	hookGit(t, first, "push", "origin", "refs/notes/byline:refs/notes/byline")
	hookGit(t, second, "notes", "--ref=refs/notes/byline", "add", "-m", "second note", head)
	if _, err := Install(second, Options{Agent: "none", Git: true}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "push", "origin", "main")
	command.Dir = second
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("push accepted divergent notes: %s", out)
	}
	if got := strings.TrimSpace(hookGit(t, remote, "notes", "--ref=refs/notes/byline", "show", base)); got != "first note" {
		t.Fatalf("remote first note = %q", got)
	}
	show := exec.Command("git", "notes", "--ref=refs/notes/byline", "show", head)
	show.Dir = remote
	if err := show.Run(); err == nil {
		t.Fatal("remote received note from divergent push")
	}
}

func pushFixture(t *testing.T) (root, remote, oldRemoteHead string) {
	t.Helper()
	root = hookRepo(t)
	remote = filepath.Join(t.TempDir(), "remote.git")
	hookGit(t, root, "config", "user.name", "Test User")
	hookGit(t, root, "config", "user.email", "test@example.invalid")
	hookGit(t, root, "init", "--bare", remote)
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "add", "file.txt")
	hookGit(t, root, "commit", "-m", "base")
	hookGit(t, root, "remote", "add", "origin", remote)
	hookGit(t, root, "push", "-u", "origin", "main")
	oldRemoteHead = strings.TrimSpace(hookGit(t, remote, "rev-parse", "refs/heads/main"))
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("base\nchange\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hookGit(t, root, "add", "file.txt")
	hookGit(t, root, "commit", "-m", "change")
	hookGit(t, root, "notes", "--ref=refs/notes/byline", "add", "-m", "attribution", "HEAD")
	return root, remote, oldRemoteHead
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

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
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
