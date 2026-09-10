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

	"github.com/comarch/git-byline/internal/gitcmd"
)

const (
	blockStart = "# >>> git-byline managed >>>"
	blockEnd   = "# <<< git-byline managed <<<"
)

// Options selects hook systems and scope.
type Options struct {
	Agent      string
	Git        bool
	User       bool
	LocalNotes bool
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
		specs := []struct {
			name    string
			command string
			install bool
		}{
			{
				name:    "post-commit",
				command: quoteExecutable(executable) + " annotate || exit 1",
				install: install,
			},
			{
				name:    "pre-push",
				command: notesPushCommand(),
				install: install && !options.LocalNotes,
			},
		}
		for _, spec := range specs {
			path, err := gitHookPath(repo, spec.name)
			if err != nil {
				return Result{}, err
			}
			didChange, err := changeGitHook(path, spec.command, spec.install)
			if err != nil {
				return Result{}, err
			}
			if didChange {
				changed = append(changed, path)
			}
		}
	}
	sort.Strings(changed)
	return Result{Changed: changed}, nil
}

func notesPushCommand() string {
	return `if git show-ref --verify --quiet refs/notes/byline; then
  git push --no-verify -- "$1" refs/notes/byline:refs/notes/byline || exit 1
fi`
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
	var path string
	switch agent {
	case "droid":
		path = filepath.Join(base, ".factory", "hooks.json")
	case "claude":
		path = filepath.Join(base, ".claude", "settings.json")
	default:
		return "", fmt.Errorf("unsupported agent %q", agent)
	}
	if err := rejectSymlinkPath(base, filepath.Dir(path)); err != nil {
		return "", err
	}
	return path, nil
}

func changeAgentConfig(path, agent, executable string, install bool) (bool, error) {
	config, mode, existed, err := readJSONObject(path)
	if err != nil {
		return false, err
	}
	eventConfig := config
	nested := agent == "claude"
	if nested {
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
	changed, err := updateAgentEvents(eventConfig, specs, install)
	if err != nil {
		return false, fmt.Errorf("update hooks in %s: %w", path, err)
	}
	if nested {
		if len(eventConfig) == 0 {
			delete(config, "hooks")
		} else {
			config["hooks"] = eventConfig
		}
	} else if agent == "droid" {
		if legacy, ok := config["hooks"].(map[string]any); ok {
			legacyChanged, err := updateAgentEvents(legacy, specs, false)
			if err != nil {
				return false, fmt.Errorf("update legacy hooks in %s: %w", path, err)
			}
			if legacyChanged {
				changed = true
				if len(legacy) == 0 {
					delete(config, "hooks")
				} else {
					config["hooks"] = legacy
				}
			}
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

func updateAgentEvents(eventConfig map[string]any, specs map[string][]hookSpec, install bool) (bool, error) {
	changed := false
	for event, eventSpecs := range specs {
		values, err := objectArray(eventConfig[event])
		if err != nil {
			return false, fmt.Errorf("read %s: %w", event, err)
		}
		for _, spec := range eventSpecs {
			var specChanged bool
			values, specChanged, err = updateAgentEvent(values, spec, install)
			if err != nil {
				return false, fmt.Errorf("update %s: %w", event, err)
			}
			changed = changed || specChanged
		}
		if len(values) == 0 {
			if _, ok := eventConfig[event]; ok {
				delete(eventConfig, event)
			}
		} else {
			eventConfig[event] = values
		}
	}
	return changed, nil
}

func updateAgentEvent(values []any, spec hookSpec, install bool) ([]any, bool, error) {
	filtered := make([]any, 0, len(values)+1)
	found := false
	changed := false
	for _, value := range values {
		if install && !found {
			updated, managed, current := updateManagedHook(value, spec)
			if managed {
				filtered = append(filtered, updated)
				found = true
				if !current {
					changed = true
				}
				continue
			}
		}
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
	return filtered, changed, nil
}

func updateManagedHook(value any, spec hookSpec) (any, bool, bool) {
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
	updatedHooks := make([]any, 0, len(hooks))
	found := false
	changed := false
	for _, value := range hooks {
		hook, ok := value.(map[string]any)
		if !ok {
			updatedHooks = append(updatedHooks, value)
			continue
		}
		candidate, _ := hook["command"].(string)
		if hook["type"] != "command" || !managedAgentCommand(candidate, spec) {
			updatedHooks = append(updatedHooks, value)
			continue
		}
		if found {
			changed = true
			continue
		}
		found = true
		if candidate == spec.command {
			updatedHooks = append(updatedHooks, value)
			continue
		}
		updatedHook := make(map[string]any, len(hook))
		for key, value := range hook {
			updatedHook[key] = value
		}
		updatedHook["command"] = spec.command
		updatedHooks = append(updatedHooks, updatedHook)
		changed = true
	}
	if !found || !changed {
		return value, found, found
	}
	updatedEntry := make(map[string]any, len(entry))
	for key, value := range entry {
		updatedEntry[key] = value
	}
	updatedEntry["hooks"] = updatedHooks
	return updatedEntry, true, false
}

type hookSpec struct {
	matcher string
	command string
}

func agentSpecs(agent, executable string) map[string][]hookSpec {
	var editMatcher string
	switch agent {
	case "droid":
		editMatcher = "Edit|Create|ApplyPatch"
	case "claude":
		editMatcher = "Write|Edit|MultiEdit"
	}
	editPre := quoteExecutable(executable) + " checkpoint " + agent + " --managed-by git-byline --type human --hook-input stdin"
	editPost := quoteExecutable(executable) + " checkpoint " + agent + " --managed-by git-byline --type ai --hook-input stdin"
	shellMatcher := "Bash|Shell|RunCommand|run_command|Execute|execute_command|Terminal"
	shellPre := quoteExecutable(executable) + " checkpoint " + agent + " --managed-by git-byline --type human --hook-input stdin"
	shellPost := quoteExecutable(executable) + " checkpoint " + agent + " --managed-by git-byline --type ai --hook-input stdin"
	return map[string][]hookSpec{
		"PreToolUse": {{
			matcher: editMatcher,
			command: editPre,
		}, {
			matcher: shellMatcher,
			command: shellPre,
		}},
		"PostToolUse": {{
			matcher: editMatcher,
			command: editPost,
		}, {
			matcher: shellMatcher,
			command: shellPost,
		}},
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
		if hook["type"] == "command" && managedAgentCommand(candidate, spec) {
			removed = true
			current = len(hooks) == 1 && candidate == spec.command
			continue
		}
		filtered = append(filtered, value)
	}
	if !removed {
		return value, false, false
	}
	hasMetadata := false
	for key := range entry {
		if key != "matcher" && key != "hooks" {
			hasMetadata = true
			break
		}
	}
	if len(filtered) == 0 && !hasMetadata {
		return nil, true, current
	}
	remaining := make(map[string]any, len(entry))
	for key, value := range entry {
		remaining[key] = value
	}
	remaining["hooks"] = filtered
	return remaining, true, false
}

func managedAgentCommand(candidate string, spec hookSpec) bool {
	signature := spec.command[strings.Index(spec.command, " checkpoint "):]
	legacySignature := strings.Replace(signature, " --managed-by git-byline", "", 1)
	return commandHasSingleExecutable(candidate, signature) ||
		commandHasGitBylineExecutable(candidate, legacySignature)
}

func commandHasSingleExecutable(command, signature string) bool {
	if !strings.HasSuffix(command, signature) {
		return false
	}
	executable := strings.TrimSuffix(command, signature)
	if len(executable) < 2 ||
		(executable[0] != '\'' && executable[0] != '"') ||
		executable[len(executable)-1] != executable[0] {
		return false
	}
	quote := byte(0)
	escaped := false
	for index := 0; index < len(executable); index++ {
		char := executable[index]
		if escaped {
			escaped = false
			continue
		}
		if quote == '"' && char == '\\' {
			escaped = true
			continue
		}
		if quote == 0 {
			switch char {
			case '\'', '"':
				quote = char
			case ' ', '\t', '\r', '\n', ';', '&', '|', '<', '>':
				return false
			}
			continue
		}
		if char == quote {
			quote = 0
		}
	}
	return quote == 0 && !escaped
}

func commandHasGitBylineExecutable(command, signature string) bool {
	if !commandHasSingleExecutable(command, signature) {
		return false
	}
	executable := strings.TrimSuffix(command, signature)
	value := executable[1 : len(executable)-1]
	value = strings.ReplaceAll(value, `\`, "/")
	base := value
	if index := strings.LastIndexByte(base, '/'); index >= 0 {
		base = base[index+1:]
	}
	return base == "git-byline" || strings.EqualFold(base, "git-byline.exe")
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

func gitHookPath(repo *gitcmd.Repo, name string) (string, error) {
	hooksPath, err := repo.GitPath("hooks")
	if err != nil {
		return "", err
	}
	base := repo.Root
	if pathInside(repo.CommonDir, hooksPath) {
		base = repo.CommonDir
	} else if !pathInside(repo.Root, hooksPath) {
		return "", fmt.Errorf("refuse hooks directory outside repository: %s", hooksPath)
	}
	if err := rejectSymlinkPath(base, hooksPath); err != nil {
		return "", err
	}
	return filepath.Join(hooksPath, name), nil
}

func pathInside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func rejectSymlinkPath(root, path string) error {
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("hook path %s escapes trusted root %s", path, root)
	}
	current := root
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat hook directory %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symlinked hook directory %s", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("hook directory component %s is not a directory", current)
		}
	}
	return nil
}

func changeGitHook(path, command string, install bool) (bool, error) {
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
	if strings.Count(text, blockStart) > 1 || strings.Count(text, blockEnd) > 1 {
		return false, fmt.Errorf("hook %s contains multiple git-byline blocks", path)
	}
	if hasBlock {
		start := strings.Index(text, blockStart)
		end := strings.Index(text, blockEnd)
		if end < start {
			return false, fmt.Errorf("hook %s contains reversed git-byline markers", path)
		}
		block := text[start : end+len(blockEnd)]
		if !managedGitHookBlock(block) {
			return false, fmt.Errorf("hook %s contains an unrecognized git-byline block", path)
		}
	}
	if existed && !isShellHook(data) {
		if !install && !hasBlock {
			return false, nil
		}
		return false, fmt.Errorf("refuse non-shell hook %s", path)
	}
	if install {
		managed := blockStart + "\n" + command + "\n" + blockEnd
		if hasBlock {
			start := strings.Index(text, blockStart)
			end := start + strings.Index(text[start:], blockEnd) + len(blockEnd)
			newline := strings.IndexByte(text, '\n')
			if newline >= 0 && start == newline+1 && text[start:end] == managed {
				return false, nil
			}
			if end < len(text) && text[end] == '\n' {
				end++
			} else if end+1 < len(text) && text[end] == '\r' && text[end+1] == '\n' {
				end += 2
			}
			text = text[:start] + text[end:]
		}
		if !existed {
			text = "#!/bin/sh\n" + managed + "\n"
		} else {
			newline := strings.IndexByte(text, '\n')
			if newline < 0 {
				text += "\n" + managed + "\n"
			} else {
				newline++
				text = text[:newline] + managed + "\n" + text[newline:]
			}
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
		if end < len(text) && text[end] == '\n' {
			end++
		} else if end+1 < len(text) && text[end] == '\r' && text[end+1] == '\n' {
			end += 2
		}
		text = text[:start] + text[end:]
	}
	backupExisted := false
	if _, err := os.Lstat(path + ".git-byline.bak"); err == nil {
		backupExisted = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat backup for %s: %w", path, err)
	}
	removeGeneratedHook := !install && strings.TrimSpace(text) == "#!/bin/sh" && !backupExisted
	if existed && !removeGeneratedHook {
		if err := createBackup(path, mode); err != nil {
			return false, err
		}
	}
	if removeGeneratedHook {
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("remove %s: %w", path, err)
		}
		return true, nil
	}
	if err := atomicWrite(path, []byte(text), mode); err != nil {
		return false, err
	}
	return true, nil
}

func managedGitHookBlock(block string) bool {
	block = strings.ReplaceAll(block, "\r\n", "\n")
	prefix := blockStart + "\n"
	suffix := "\n" + blockEnd
	if !strings.HasPrefix(block, prefix) || !strings.HasSuffix(block, suffix) {
		return false
	}
	command := strings.TrimSuffix(strings.TrimPrefix(block, prefix), suffix)
	return command == notesPushCommand() ||
		commandHasSingleExecutable(command, " annotate || exit 1") ||
		commandHasSingleExecutable(command, " annotate")
}

func isShellHook(data []byte) bool {
	first, _, _ := strings.Cut(strings.TrimSuffix(string(data), "\r"), "\n")
	if !strings.HasPrefix(first, "#!") {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(first, "#!")))
	if len(fields) == 0 {
		return false
	}
	interpreter := filepath.Base(fields[0])
	if interpreter == "env" {
		if len(fields) < 2 {
			return false
		}
		interpreter = filepath.Base(fields[1])
	}
	switch interpreter {
	case "sh", "bash", "dash", "ksh", "zsh":
		return true
	default:
		return false
	}
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
