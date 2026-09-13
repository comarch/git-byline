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
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const (
	blockStart   = "# >>> git-byline managed >>>"
	blockEnd     = "# <<< git-byline managed <<<"
	maxHookBytes = 1 << 20
	// templateConfigKey is the user-level Git configuration entry that
	// points every new git init and git clone at the managed template.
	templateConfigKey = "init.templateDir"
	// templateInfoExclude and templateDescription mirror the files Git
	// normally copies from its default template directory. A custom
	// template replaces that directory entirely, so without these files
	// every new repository would miss .git/info/exclude.
	templateInfoExclude = `# git ls-files --others --exclude-from=.git/info/exclude
# Lines that start with '#' are comments.
# For a project with two submodules a and b:
# a/b
# *.[oa]
# *~
`
	templateDescription = "Unnamed repository; edit this file 'description' to name the repository.\n"
)

// Options selects hook systems and scope.
type Options struct {
	Agent      string
	Git        bool
	User       bool
	LocalNotes bool
	Template   bool
}

// Result lists files changed by an operation.
type Result struct {
	Changed []string
	// ConfigChanged reports that the user-level init.templateDir value was
	// written or removed by a template-scope operation.
	ConfigChanged bool
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
	agents := selectedAgents(options.Agent)
	var repo *gitcmd.Repo
	// Template-scope Git hooks live in the user-level template directory,
	// so only repository-scoped work discovers a worktree.
	needRepo := (options.Git && !options.Template) || (len(agents) > 0 && !options.User && !options.Template)
	if needRepo {
		discovered, err := gitcmd.Discover(dir)
		if err != nil {
			return Result{}, err
		}
		repo = discovered
	}
	executable := "git-byline"
	if install {
		var err error
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
	for _, agent := range agents {
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
	configChanged := false
	if options.Template {
		setConfig, err := prepareTemplate(install)
		if err != nil {
			return Result{}, err
		}
		configChanged = setConfig
	}
	if options.Git {
		specs := []struct {
			name    string
			command string
			install bool
		}{
			{
				name:    "post-commit",
				command: postCommitCommand(executable),
				install: install,
			},
			{
				name:    "pre-push",
				command: notesPushCommand(),
				install: install && !options.LocalNotes,
			},
			{
				name:    "post-rewrite",
				command: rewriteHookCommand(executable, "post-rewrite", true),
				install: install,
			},
			{
				name:    "post-merge",
				command: postMergeCommand(executable),
				install: install,
			},
			{
				name:    "post-checkout",
				command: rewriteHookCommand(executable, "post-checkout", false),
				install: install,
			},
			{
				name:    "reference-transaction",
				command: referenceTransactionHookCommand(executable),
				install: install,
			},
		}
		for _, spec := range specs {
			var path string
			var err error
			if options.Template {
				path, err = templateHookPath(spec.name)
			} else {
				path, err = gitHookPath(repo, spec.name)
			}
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
	if options.Template {
		if err := finalizeTemplate(install, &changed); err != nil {
			return Result{}, err
		}
		if configChanged {
			if err := writeTemplateConfig(install); err != nil {
				return Result{}, err
			}
		}
	}
	sort.Strings(changed)
	return Result{Changed: changed, ConfigChanged: configChanged}, nil
}

func notesPushCommand() string {
	return `if git show-ref --verify --quiet refs/notes/byline; then
  git push --no-verify -- "$1" refs/notes/byline:refs/notes/byline || exit 1
fi`
}

func postCommitCommand(executable string) string {
	return quoteExecutable(executable) + ` rewrite --mode post-merge --hook-input stdin || exit 1
` + quoteExecutable(executable) + " annotate || exit 1"
}

// postMergeCommand annotates merge commits right after Git records them.
// git merge runs no post-commit hook, so without the annotate step every
// merge would leave the annotated boundary behind and the next commit
// would fail with a commit gap.
func postMergeCommand(executable string) string {
	return quoteExecutable(executable) + ` rewrite --mode post-merge --hook-input stdin "$@" || exit 1
` + quoteExecutable(executable) + " annotate || exit 1"
}

func rewriteHookCommand(executable, mode string, failClosed bool) string {
	command := quoteExecutable(executable) + " rewrite --mode " + mode + ` --hook-input stdin "$@"`
	if failClosed {
		return command + " || exit 1"
	}
	return command + " || true"
}

func referenceTransactionHookCommand(executable string) string {
	return `if [ -n "${GIT_BYLINE_NESTED:-}" ]; then
  exit 0
fi
` + quoteExecutable(executable) + ` rewrite --mode ref-txn --hook-input stdin "$@" || true`
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

// templateDir returns the trusted base directory and the managed Git
// template directory inside it. XDG_CONFIG_HOME takes precedence so tests
// and custom setups can relocate the template.
func templateDir() (string, string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		if !filepath.IsAbs(xdg) {
			return "", "", fmt.Errorf("XDG_CONFIG_HOME is not an absolute path: %s", xdg)
		}
		return xdg, filepath.Join(xdg, "git-byline", "templates"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, filepath.Join(home, ".config", "git-byline", "templates"), nil
}

// templateHookPath resolves one hook file inside the managed template
// directory and refuses symlinked path components.
func templateHookPath(name string) (string, error) {
	base, dir, err := templateDir()
	if err != nil {
		return "", err
	}
	hooksDir := filepath.Join(dir, "hooks")
	if err := rejectSymlinkPath(base, hooksDir); err != nil {
		return "", err
	}
	return filepath.Join(hooksDir, name), nil
}

// prepareTemplate guards the user-level init.templateDir value before
// any template file is touched. An install refuses a foreign value; the
// return value reports whether the configuration still has to be
// written or removed.
func prepareTemplate(install bool) (bool, error) {
	_, dir, err := templateDir()
	if err != nil {
		return false, err
	}
	current, exists, err := gitcmd.GlobalConfig(templateConfigKey)
	if err != nil {
		return false, err
	}
	if !exists {
		return install, nil
	}
	if filepath.Clean(current) != filepath.Clean(dir) {
		if !install {
			return false, nil
		}
		return false, fmt.Errorf("refuse to replace existing %s %s", templateConfigKey, current)
	}
	return !install, nil
}

// finalizeTemplate runs the file work that follows the hook loop: an
// install writes the stock template files, an uninstall removes them
// and any directory that became empty.
func finalizeTemplate(install bool, changed *[]string) error {
	_, dir, err := templateDir()
	if err != nil {
		return err
	}
	if install {
		return writeTemplateStock(dir, changed)
	}
	return pruneTemplate(dir, changed)
}

func writeTemplateStock(dir string, changed *[]string) error {
	stock := []struct {
		relative string
		content  string
	}{
		{"info/exclude", templateInfoExclude},
		{"description", templateDescription},
	}
	for _, file := range stock {
		path := filepath.Join(dir, filepath.FromSlash(file.relative))
		if _, err := os.Lstat(path); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		if err := atomicWrite(path, []byte(file.content), 0o644); err != nil {
			return err
		}
		*changed = append(*changed, path)
	}
	return nil
}

// pruneTemplate removes the stock files git-byline wrote and any
// directory that became empty. Foreign files and edits keep their place.
func pruneTemplate(dir string, changed *[]string) error {
	stock := []struct {
		relative string
		content  string
	}{
		{"info/exclude", templateInfoExclude},
		{"description", templateDescription},
	}
	for _, file := range stock {
		path := filepath.Join(dir, filepath.FromSlash(file.relative))
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if string(data) != file.content {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		*changed = append(*changed, path)
	}
	for _, relative := range []string{"hooks", "info", "."} {
		path := filepath.Join(dir, relative)
		entries, err := os.ReadDir(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read directory %s: %w", path, err)
		}
		if len(entries) > 0 {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
	}
	return nil
}

// writeTemplateConfig writes or removes the user-level init.templateDir
// value after the template directory reached the requested state.
func writeTemplateConfig(install bool) error {
	_, dir, err := templateDir()
	if err != nil {
		return err
	}
	if install {
		return gitcmd.SetGlobalConfig(templateConfigKey, dir)
	}
	if _, err := gitcmd.UnsetGlobalConfig(templateConfigKey); err != nil {
		return err
	}
	return nil
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
		var backupErr error
		if install {
			backupErr = refreshBackup(path, mode)
		} else {
			backupErr = createBackup(path, mode)
		}
		if backupErr != nil {
			return false, backupErr
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
	return commandHasGitBylineExecutable(candidate, signature) ||
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
	if base == "git-byline" || strings.EqualFold(base, "git-byline.exe") {
		return true
	}
	current, err := os.Executable()
	if err != nil {
		return false
	}
	current, err = filepath.Abs(current)
	if err != nil {
		return false
	}
	return filepath.Clean(filepath.FromSlash(value)) == filepath.Clean(current)
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
	data, err := readCappedFile(path)
	if err != nil {
		return nil, 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxHookBytes {
		return nil, 0, false, fmt.Errorf("configuration %s exceeds %d bytes", path, maxHookBytes)
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
	data, err := readCappedFile(path)
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

func refreshBackup(path string, mode os.FileMode) error {
	data, err := readCappedFile(path)
	if err != nil {
		return fmt.Errorf("read backup source %s: %w", path, err)
	}
	if err := atomicWrite(path+".git-byline.bak", data, mode); err != nil {
		return fmt.Errorf("refresh backup %s: %w", path+".git-byline.bak", err)
	}
	return nil
}

func readCappedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("refuse non-regular file %s", path)
	}
	if info.Size() > maxHookBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxHookBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHookBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxHookBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxHookBytes)
	}
	return data, nil
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
		if info.Size() > maxHookBytes {
			return false, fmt.Errorf("hook %s exceeds %d bytes", path, maxHookBytes)
		}
	}
	var data []byte
	if existed {
		file, openErr := os.Open(path)
		if openErr != nil {
			return false, fmt.Errorf("read %s: %w", path, openErr)
		}
		data, err = io.ReadAll(io.LimitReader(file, maxHookBytes+1))
		closeErr := file.Close()
		if err != nil {
			return false, fmt.Errorf("read %s: %w", path, err)
		}
		if closeErr != nil {
			return false, fmt.Errorf("close %s: %w", path, closeErr)
		}
		if len(data) > maxHookBytes {
			return false, fmt.Errorf("hook %s exceeds %d bytes", path, maxHookBytes)
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
			if !install {
				return false, nil
			}
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
		if install {
			if err := refreshBackup(path, mode); err != nil {
				return false, err
			}
		} else if err := createBackup(path, mode); err != nil {
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
		isPostCommitCommand(command) ||
		isPostMergeCommand(command) ||
		commandHasGitBylineExecutable(command, " annotate || exit 1") ||
		commandHasGitBylineExecutable(command, " annotate") ||
		commandHasGitBylineExecutable(command, ` rewrite --mode post-rewrite --hook-input stdin "$@" || exit 1`) ||
		commandHasGitBylineExecutable(command, ` rewrite --mode post-merge --hook-input stdin "$@" || exit 1`) ||
		commandHasGitBylineExecutable(command, ` rewrite --mode post-checkout --hook-input stdin "$@" || true`) ||
		isReferenceTransactionCommand(command)
}

func isPostCommitCommand(command string) bool {
	lines := strings.Split(command, "\n")
	if len(lines) != 2 {
		return false
	}
	return commandHasGitBylineExecutable(lines[0], ` rewrite --mode post-merge --hook-input stdin || exit 1`) &&
		commandHasGitBylineExecutable(lines[1], " annotate || exit 1")
}

// isPostMergeCommand recognizes both the current rewrite-plus-annotate
// block and accepts replacement of the older rewrite-only block.
func isPostMergeCommand(command string) bool {
	lines := strings.Split(command, "\n")
	if len(lines) != 2 {
		return false
	}
	return commandHasGitBylineExecutable(lines[0], ` rewrite --mode post-merge --hook-input stdin "$@" || exit 1`) &&
		commandHasGitBylineExecutable(lines[1], " annotate || exit 1")
}

func isReferenceTransactionCommand(command string) bool {
	prefix := "if [ -n \"${GIT_BYLINE_NESTED:-}\" ]; then\n  exit 0\nfi\n"
	if !strings.HasPrefix(command, prefix) {
		return false
	}
	return commandHasGitBylineExecutable(
		strings.TrimPrefix(command, prefix),
		` rewrite --mode ref-txn --hook-input stdin "$@" || true`,
	)
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
