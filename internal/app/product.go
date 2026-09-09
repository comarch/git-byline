package app

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/mrwogu/git-byline/internal/gitcmd"
	"github.com/mrwogu/git-byline/internal/hooks"
	"github.com/mrwogu/git-byline/internal/model"
	"github.com/mrwogu/git-byline/internal/preset"
	"github.com/mrwogu/git-byline/internal/provenance"
)

func runCheckpoint(env *Env, command *command, args []string) (int, error) {
	if len(args) == 0 {
		return commandUsageError(env, command, errors.New("preset is required"))
	}
	presetName := args[0]
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	typeName := flags.String("type", "", "human or ai")
	hookInput := flags.String("hook-input", "", "must be stdin")
	if err := flags.Parse(args[1:]); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("checkpoint takes no positional arguments after the preset"))
	}
	if *hookInput != "stdin" {
		return commandUsageError(env, command, errors.New("--hook-input must be stdin"))
	}
	explicit := model.Author(*typeName)
	if presetName == "agent-v1" {
		if explicit != "" {
			return commandUsageError(env, command, errors.New("agent-v1 does not accept --type"))
		}
	} else if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return commandUsageError(env, command, errors.New("--type must be human or ai"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		if gitcmd.IsNotRepository(err) {
			return ExitSuccess, nil
		}
		return operationalError(env, command.name, err)
	}
	event, handled, err := preset.Parse(presetName, explicit, env.Stdin)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if !handled {
		return ExitSuccess, nil
	}
	result, err := provenance.Capture(repo, event, env.now())
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	return ExitSuccess, nil
}

func runAnnotate(env *Env, command *command, args []string) (int, error) {
	if len(args) != 0 {
		return commandUsageError(env, command, errors.New("annotate takes no arguments"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := provenance.Annotate(repo)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	if result.Noop {
		fmt.Fprintf(env.Stdout, "already annotated %s\n", result.Commit)
	} else {
		fmt.Fprintf(env.Stdout, "annotated %s (%d files)\n", result.Commit, result.Files)
	}
	return ExitSuccess, nil
}

func runBlame(env *Env, command *command, args []string) (int, error) {
	jsonOutput, rest, err := parseJSONFlag(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, command.usage)
			return ExitSuccess, nil
		}
		return commandUsageError(env, command, err)
	}
	if len(rest) != 1 {
		return commandUsageError(env, command, errors.New("blame requires exactly one file"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := provenance.Blame(repo, rest[0])
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
		return ExitSuccess, nil
	}
	writeWarnings(env, result.Warnings)
	for _, line := range result.Lines {
		source := string(line.Attribution.Author)
		if line.Attribution.Author == model.AuthorAI {
			source += ":" + line.Attribution.Agent
			if line.Attribution.Model != "" {
				source += "/" + line.Attribution.Model
			}
		}
		fmt.Fprintf(env.Stdout, "%-24s %6d | %s\n", source, line.Number, line.Content)
	}
	return ExitSuccess, nil
}

func runStatus(env *Env, command *command, args []string) (int, error) {
	jsonOutput, rest, err := parseJSONFlag(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, command.usage)
			return ExitSuccess, nil
		}
		return commandUsageError(env, command, err)
	}
	if len(rest) != 0 {
		return commandUsageError(env, command, errors.New("status takes no positional arguments"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := provenance.Status(repo)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if jsonOutput {
		if err := writeJSON(env, result); err != nil {
			return operationalError(env, command.name, err)
		}
		return ExitSuccess, nil
	}
	fmt.Fprintf(env.Stdout, "HEAD: %s\n", valueOrNone(result.Head))
	fmt.Fprintf(env.Stdout, "Last annotated commit: %s\n", valueOrNone(result.LastAnnotatedCommit))
	fmt.Fprintf(env.Stdout, "Pending checkpoints: %d\n", result.PendingCheckpoints)
	fmt.Fprintf(env.Stdout, "Pending files: %d\n", result.PendingFiles)
	fmt.Fprintf(env.Stdout, "Retained snapshots: %d\n", result.RetainedSnapshots)
	writeWarnings(env, result.Warnings)
	return ExitSuccess, nil
}

func runInstallHooks(env *Env, command *command, args []string) (int, error) {
	options, err := parseHookOptions(command, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, command.usage)
			return ExitSuccess, nil
		}
		return commandUsageError(env, command, err)
	}
	dir, err := env.workingDir()
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := hooks.Install(dir, options)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	for _, path := range result.Changed {
		fmt.Fprintf(env.Stdout, "updated %s\n", path)
	}
	if len(result.Changed) == 0 {
		fmt.Fprintln(env.Stdout, "hooks already installed")
	}
	return ExitSuccess, nil
}

func runUninstall(env *Env, command *command, args []string) (int, error) {
	options, err := parseHookOptions(command, args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, command.usage)
			return ExitSuccess, nil
		}
		return commandUsageError(env, command, err)
	}
	dir, err := env.workingDir()
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := hooks.Uninstall(dir, options)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	for _, path := range result.Changed {
		fmt.Fprintf(env.Stdout, "updated %s\n", path)
	}
	if len(result.Changed) == 0 {
		fmt.Fprintln(env.Stdout, "no managed hooks found")
	}
	return ExitSuccess, nil
}

func parseHookOptions(command *command, args []string) (hooks.Options, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	flags.SetOutput(new(strings.Builder))
	agent := flags.String("agent", "all", "droid, claude, all, or none")
	gitHook := flags.Bool("git", false, "manage Git post-commit hook")
	user := flags.Bool("user", false, "use user agent configuration")
	project := flags.Bool("project", false, "use project agent configuration")
	if err := flags.Parse(args); err != nil {
		return hooks.Options{}, err
	}
	if flags.NArg() != 0 {
		return hooks.Options{}, errors.New("unexpected positional argument")
	}
	if *user && *project {
		return hooks.Options{}, errors.New("--user and --project are mutually exclusive")
	}
	if !*user && !*project {
		*project = true
	}
	switch *agent {
	case "droid", "claude", "all", "none":
	default:
		return hooks.Options{}, fmt.Errorf("unsupported agent %q", *agent)
	}
	if *agent == "none" && !*gitHook {
		return hooks.Options{}, errors.New("select an agent or --git")
	}
	return hooks.Options{Agent: *agent, Git: *gitHook, User: *user}, nil
}

func parseJSONFlag(args []string) (bool, []string, error) {
	var jsonOutput bool
	var rest []string
	for _, arg := range args {
		switch arg {
		case "--json":
			if jsonOutput {
				return false, nil, errors.New("--json specified more than once")
			}
			jsonOutput = true
		case "-h", "--help":
			return false, nil, flag.ErrHelp
		default:
			if strings.HasPrefix(arg, "-") {
				return false, nil, fmt.Errorf("unknown flag %q", arg)
			}
			rest = append(rest, arg)
		}
	}
	return jsonOutput, rest, nil
}

func discoverForEnv(env *Env) (*gitcmd.Repo, error) {
	dir, err := env.workingDir()
	if err != nil {
		return nil, err
	}
	return gitcmd.Discover(dir)
}

func operationalError(env *Env, name string, err error) (int, error) {
	fmt.Fprintf(env.Stderr, "git-byline %s: %v\n", name, err)
	return ExitFailure, fmt.Errorf("git-byline %s: %w", name, err)
}

func flagError(env *Env, command *command, output string, err error) (int, error) {
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(env.Stdout, command.usage)
		return ExitSuccess, nil
	}
	fmt.Fprint(env.Stderr, output)
	return ExitUsage, fmt.Errorf("git-byline %s: %w", command.name, err)
}

func writeWarnings(env *Env, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintf(env.Stderr, "warning: %s\n", warning)
	}
}

func writeJSON(env *Env, value any) error {
	encoder := json.NewEncoder(env.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}

func valueOrNone(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}
