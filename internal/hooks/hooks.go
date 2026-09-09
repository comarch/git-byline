// Package hooks installs and removes git-byline-managed agent and Git hooks.
package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mrwogu/git-byline/internal/gitcmd"
)

const (
	blockStart = "# >>> git-byline managed >>>"
	blockEnd   = "# <<< git-byline managed <<<"
)

// Options selects hook systems and scope.
type Options struct {
	Agent string
	Git   bool
	User  bool
}

// Result lists files changed by an operation.
type Result struct {
	Changed []string
}

// Install merges selected hooks.
func Install(dir string, options Options) (Result, error) {
	return change(dir, options, true)
}

// Uninstall removes only selected managed hooks.
func Uninstall(dir string, options Options) (Result, error) {
	return change(dir, options, false)
}

func change(dir string, options Options, install bool) (Result, error) {
	var repo *gitcmd.Repo
	var err error
	if options.Git || !options.User {
		repo, err = gitcmd.Discover(dir)
		if err != nil {
			return Result{}, err
		}
	}
	executable := "git-byline"
	if install {
		executable, err = os.Executable()
		if err != nil {
			return Result{}, fmt.Errorf("resolve git-byline executable: %w", err)
		}
		executable, err = filepath.Abs(executable)
		if err != nil {
			return Result{}, fmt.Errorf("resolve absolute executable path: %w", err)
		}
	}
	var changed []string
	agentExecutable := executable
	if install && !options.User {
		agentExecutable = "git-byline"
	}
	for _, agent := range selectedAgents(options.Agent) {
		root := ""
		if repo != nil {
			root = repo.Root
		}
		path, err := agentConfigPath(root, agent, options.User)
		if err != nil {
			return Result{}, err
		}
		didChange, err := changeAgentConfig(path, agent, agentExecutable, install)
		if err != nil {
			return Result{}, err
		}
		if didChange {
			changed = append(changed, path)
		}
	}
	if options.Git {
		path, err := gitHookPath(repo)
		if err != nil {
			return Result{}, err
		}
		didChange, err := changeGitHook(path, executable, install)
		if err != nil {
			return Result{}, err
		}
		if didChange {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return Result{Changed: changed}, nil
}

func selectedAgents(value string) []string {
	switch value {
	case "all":
		return []string{"claude", "droid"}
	case "droid", "claude":
		return []string{value}
	default:
		return nil
	}
}

func agentConfigPath(root, agent string, user bool) (string, error) {
	base := root
	if user {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		base = home
	}
	switch agent {
	case "droid":
		return filepath.Join(base, ".factory", "hooks.json"), nil
	case "claude":
		return filepath.Join(base, ".claude", "settings.json"), nil
	default:
		return "", fmt.Errorf("unsupported agent %q", agent)
	}
}

func changeAgentConfig(path, agent, executable string, install bool) (bool, error) {
	config, mode, existed, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	eventConfig := config
	if agent == "claude" {
		value := config["hooks"]
		if value == nil {
			eventConfig = map[string]any{}
		} else {
			var ok bool
			eventConfig, ok = value.(map[string]any)
			if !ok {
				return false, fmt.Errorf("read hooks in %s: value is not an object", path)
			}
		}
	}
	specs := agentSpecs(agent, executable)
	changed := false
	for event, spec := range specs {
		values, err := objectArray(eventConfig[event])
		if err != nil {
			return false, fmt.Errorf("read %s in %s: %w", event, path, err)
		}
		filtered := make([]any, 0, len(values)+1)
		found := false
		for _, value := range values {
			remaining, removed, current := removeManagedHook(value, spec)
			if removed {
				if install && current && !found {
					filtered = append(filtered, value)
					found = true
					continue
				}
				if remaining != nil {
					filtered = append(filtered, remaining)
				}
				if install && !found {
					filtered = append(filtered, managedEntry(spec))
					found = true
				}
				changed = true
				continue
			}
			filtered = append(filtered, value)
		}
		if install && !found {
			filtered = append(filtered, managedEntry(spec))
			changed = true
		}
		if !install && len(filtered) != len(values) {
			changed = true
		}
		if len(filtered) == 0 {
			if _, ok := eventConfig[event]; ok {
				delete(eventConfig, event)
			}
		} else {
			eventConfig[event] = filtered
		}
	}
	if agent == "claude" {
		if len(eventConfig) == 0 {
			delete(config, "hooks")
		} else {
			config["hooks"] = eventConfig
		}
	}
	if !changed {
		return false, nil
	}
	if existed {
		if err := createBackup(path, mode); err != nil {
			return false, err
		}
	}
	if !install && len(config) == 0 && !existed {
		return false, nil
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode %s: %w", path, err)
	}
	data = append(data, '\n')
	if err := atomicWrite(path, data, mode); err != nil {
		return false, err
	}
	return true, nil
}

type hookSpec struct {
	matcher string
	command string
}

func agentSpecs(agent, executable string) map[string]hookSpec {
	var matcher string
	switch agent {
	case "droid":
		matcher = "Edit|Create|ApplyPatch"
	case "claude":
		matcher = "Write|Edit|MultiEdit"
	}
	return map[string]hookSpec{
		"PreToolUse": {
			matcher: matcher,
			command: quoteExecutable(executable) + " checkpoint " + agent + " --type human --hook-input stdin",
		},
		"PostToolUse": {
			matcher: matcher,
			command: quoteExecutable(executable) + " checkpoint " + agent + " --type ai --hook-input stdin",
		},
	}
}

func quoteExecutable(path string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.ReplaceAll(path, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
}

func managedEntry(spec hookSpec) map[string]any {
	return map[string]any{
		"matcher": spec.matcher,
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": spec.command,
				"timeout": 30,
			},
		},
	}
}

func removeManagedHook(value any, spec hookSpec) (any, bool, bool) {
	entry, ok := value.(map[string]any)
	if !ok {
		return value, false, false
	}
	matcher, _ := entry["matcher"].(string)
	if matcher != spec.matcher {
		return value, false, false
	}
	hooks, err := objectArray(entry["hooks"])
	if err != nil {
		return value, false, false
	}
	filtered := make([]any, 0, len(hooks))
	removed := false
	current := false
	for _, value := range hooks {
		hook, ok := value.(map[string]any)
		if !ok {
			filtered = append(filtered, value)
			continue
		}
		candidate, _ := hook["command"].(string)
		signature := spec.command[strings.Index(spec.command, " checkpoint "):]
		if hook["type"] == "command" && strings.HasSuffix(candidate, signature) {
			removed = true
			current = len(hooks) == 1 && candidate == spec.command
			continue
		}
		filtered = append(filtered, value)
	}
	if !removed {
		return value, false, false
	}
	if len(filtered) == 0 {
		return nil, true, current
	}
	remaining := make(map[string]any, len(entry))
	for key, value := range entry {
		remaining[key] = value
	}
	remaining["hooks"] = filtered
	return remaining, true, false
}

func objectArray(value any) ([]any, error) {
	if value == nil {
		return nil, nil
	}
	result, ok := value.([]any)
	if !ok {
		return nil, errors.New("value is not an array")
	}
	return result, nil
}

func readJSONObject(path string) (map[string]any, os.FileMode, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, 0o600, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, false, fmt.Errorf("refuse non-regular configuration %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil {
		return nil, 0, false, fmt.Errorf("decode %s: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, 0, false, fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return nil, 0, false, fmt.Errorf("decode %s tail: %w", path, err)
	}
	if object == nil {
		return nil, 0, false, fmt.Errorf("decode %s: root must be an object", path)
	}
	return object, info.Mode().Perm(), true, nil
}

func createBackup(path string, mode os.FileMode) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read backup source %s: %w", path, err)
	}
	backup := path + ".git-byline.bak"
	file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create backup %s: %w", backup, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write backup %s: %w", backup, err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync backup %s: %w", backup, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close backup %s: %w", backup, err)
	}
	return nil
}

func gitHookPath(repo *gitcmd.Repo) (string, error) {
	hooksPath, ok, err := repo.ConfigPath("core.hooksPath")
	if err != nil {
		return "", err
	}
	if !ok || hooksPath == "" {
		return filepath.Join(repo.GitDir, "hooks", "post-commit"), nil
	}
	if !filepath.IsAbs(hooksPath) {
		hooksPath = filepath.Join(repo.Root, hooksPath)
	}
	return filepath.Join(filepath.Clean(hooksPath), "post-commit"), nil
}

func changeGitHook(path, executable string, install bool) (bool, error) {
	existed := true
	mode := os.FileMode(0o755)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		existed = false
	} else if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	} else if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refuse non-regular hook %s", path)
	} else {
		mode = info.Mode().Perm() | 0o100
	}
	var data []byte
	if existed {
		data, err = os.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return false, fmt.Errorf("hook %s is not a text file", path)
	}
	text := string(data)
	hasBlock := strings.Contains(text, blockStart) || strings.Contains(text, blockEnd)
	if hasBlock && (!strings.Contains(text, blockStart) || !strings.Contains(text, blockEnd)) {
		return false, fmt.Errorf("hook %s contains an incomplete git-byline block", path)
	}
	if install {
		if hasBlock {
			return false, nil
		}
		managed := blockStart + "\n" + quoteExecutable(executable) + " annotate\n" + blockEnd
		if !existed {
			text = "#!/bin/sh\n\n" + managed + "\n"
		} else {
			text = strings.TrimRight(text, "\r\n") + "\n\n" + managed + "\n"
		}
	} else {
		if !hasBlock {
			return false, nil
		}
		start := strings.Index(text, blockStart)
		end := strings.Index(text[start:], blockEnd)
		if end < 0 {
			return false, fmt.Errorf("hook %s contains an incomplete git-byline block", path)
		}
		end = start + end + len(blockEnd)
		text = strings.TrimRight(text[:start], "\r\n") + strings.TrimLeft(text[end:], "\r\n")
		if text != "" {
			text += "\n"
		}
	}
	if existed {
		if err := createBackup(path, mode); err != nil {
			return false, err
		}
	}
	if !install && strings.TrimSpace(text) == "#!/bin/sh" && !existed {
		return false, nil
	}
	if err := atomicWrite(path, []byte(text), mode); err != nil {
		return false, err
	}
	return true, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create directory for %s: %w", path, err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", path, err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return fmt.Errorf("chmod temporary file for %s: %w", path, err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary file for %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary file for %s: %w", path, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}
