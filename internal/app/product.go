package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/ci"
	"github.com/comarch/git-byline/internal/dashboard"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/hooks"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/transcript"
)

var checkpointInputTimeoutNanos atomic.Int64

// checkpointArgs holds the validated checkpoint command surface.
type checkpointArgs struct {
	presetName string
	explicit   model.Author
}

func runCheckpoint(env *Env, command *command, args []string) (int, error) {
	parsed, code, done, err := parseCheckpointArgs(env, command, args)
	if done {
		return code, err
	}
	input, err := readCheckpointInput(env.Stdin)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	event, handled, parseErr := preset.Parse(parsed.presetName, parsed.explicit, bytes.NewReader(input))
	if parseErr == nil && !handled {
		return ExitSuccess, nil
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		if gitcmd.IsNotRepository(err) {
			return ExitSuccess, nil
		}
		return operationalError(env, command.name, err)
	}
	if parseErr != nil {
		return operationalError(env, command.name, parseErr)
	}
	event = resolveEventModel(event)
	result, err := provenance.CaptureEvent(repo, event, env.now())
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	return ExitSuccess, nil
}

// parseCheckpointArgs validates flags and the preset surface. done marks a
// finished exit path with its code and error.
func parseCheckpointArgs(env *Env, command *command, args []string) (checkpointArgs, int, bool, error) {
	if len(args) == 0 {
		code, err := commandUsageError(env, command, errors.New("preset is required"))
		return checkpointArgs{}, code, true, err
	}
	parsed := checkpointArgs{presetName: args[0]}
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	typeName := flags.String("type", "", "human or ai")
	hookInput := flags.String("hook-input", "", "must be stdin")
	managedBy := flags.String("managed-by", "", "managed hook owner")
	if err := flags.Parse(args[1:]); err != nil {
		code, err := flagError(env, command, output.String(), err)
		return checkpointArgs{}, code, true, err
	}
	if flags.NArg() != 0 {
		code, err := commandUsageError(env, command, errors.New("checkpoint takes no positional arguments after the preset"))
		return checkpointArgs{}, code, true, err
	}
	if *hookInput != "stdin" {
		code, err := commandUsageError(env, command, errors.New("--hook-input must be stdin"))
		return checkpointArgs{}, code, true, err
	}
	if *managedBy != "" && *managedBy != "git-byline" {
		code, err := commandUsageError(env, command, errors.New("--managed-by must be git-byline"))
		return checkpointArgs{}, code, true, err
	}
	parsed.explicit = model.Author(*typeName)
	if parsed.presetName == "agent-v1" {
		if parsed.explicit != "" {
			code, err := commandUsageError(env, command, errors.New("agent-v1 does not accept --type"))
			return checkpointArgs{}, code, true, err
		}
		return parsed, ExitSuccess, false, nil
	}
	if parsed.explicit != model.AuthorHuman && parsed.explicit != model.AuthorAI {
		code, err := commandUsageError(env, command, errors.New("--type must be human or ai"))
		return checkpointArgs{}, code, true, err
	}
	return parsed, ExitSuccess, false, nil
}

// resolveEventModel replaces the fallback model with the model named by the
// session transcript or sidecar when the event allows it.
func resolveEventModel(event preset.Event) preset.Event {
	if event.Type != model.AuthorAI || event.Model != preset.FallbackModel || event.TranscriptPath == "" {
		return event
	}
	resolved, err := transcript.ResolveModel(event.TranscriptPath)
	if err != nil || resolved == "" {
		return event
	}
	candidate := event
	candidate.Model = resolved
	if model.ValidateAttribution(model.Attribution{
		Author: candidate.Type, Agent: candidate.Agent, Model: candidate.Model, Session: candidate.Session,
	}) != nil {
		return event
	}
	return candidate
}

func readCheckpointInput(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, errors.New("hook input is nil")
	}
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(io.LimitReader(input, preset.MaxInputBytes+1))
		done <- result{data: data, err: err}
	}()
	timeout := time.Duration(checkpointInputTimeoutNanos.Load())
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value := <-done:
		if value.err != nil {
			return nil, fmt.Errorf("read hook input: %w", value.err)
		}
		if len(value.data) > preset.MaxInputBytes {
			return nil, fmt.Errorf("hook input exceeds %d bytes", preset.MaxInputBytes)
		}
		return value.data, nil
	case <-timer.C:
		return nil, fmt.Errorf("read hook input timed out after %s", timeout)
	}
}

func runDashboard(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	rangeValue := ""
	rangeSpecified := false
	flags.Func("range", "revision range", func(value string) error {
		if rangeSpecified {
			return errors.New("--range specified more than once")
		}
		rangeValue = value
		rangeSpecified = true
		return nil
	})
	repoMode := flags.Bool("repo", false, "render repository range report")
	outputPath := flags.String("output", "", "output file")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	rangeModeArg := false
	for _, arg := range args {
		if arg == "--repo" || arg == "--range" || strings.HasPrefix(arg, "--range=") {
			rangeModeArg = true
			break
		}
	}
	if flags.NArg() > 1 {
		if rangeModeArg {
			return commandUsageError(env, command, errors.New("--range or --repo cannot be combined with a file argument"))
		}
		return commandUsageError(env, command, errors.New("dashboard accepts at most one file"))
	}
	if rangeSpecified || *repoMode {
		if flags.NArg() != 0 {
			return commandUsageError(env, command, errors.New("--range or --repo cannot be combined with a file argument"))
		}
		aggregate, code, err := collectRangeAggregate(env, command, rangeSpecified, rangeValue)
		if err != nil {
			return code, err
		}
		data, err := dashboard.RenderRange(aggregate)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		file, path, err := createDashboardOutput(env, *outputPath)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		if err := writeExclusiveOutput(file, path, data, "dashboard"); err != nil {
			return operationalError(env, command.name, err)
		}
		fmt.Fprintln(env.Stdout, path)
		return ExitSuccess, nil
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	status, err := provenance.Status(repo)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	report := dashboard.Report{Status: status}
	if flags.NArg() == 1 {
		file, err := provenance.BlameHeadFile(repo, flags.Arg(0))
		if err != nil {
			return operationalError(env, command.name, err)
		}
		report.Commit = file.Commit
		report.Files = []provenance.BlameResult{file}
	} else {
		collection, err := provenance.BlameHead(repo)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		report.Commit = collection.Commit
		report.Files = collection.Files
		writeWarnings(env, collection.Warnings)
	}
	data, err := dashboard.Render(report)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	file, path, err := createDashboardOutput(env, *outputPath)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if err := writeExclusiveOutput(file, path, data, "dashboard"); err != nil {
		return operationalError(env, command.name, err)
	}
	fmt.Fprintln(env.Stdout, path)
	return ExitSuccess, nil
}

func createDashboardOutput(env *Env, requested string) (*os.File, string, error) {
	if requested == "" {
		file, err := os.CreateTemp("", "git-byline-dashboard-*.html")
		if err != nil {
			return nil, "", fmt.Errorf("create dashboard temp file: %w", err)
		}
		return file, file.Name(), nil
	}
	return createExclusiveOutput(env, requested, "dashboard")
}

func runAnnotate(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	dropStranded := flags.Bool("drop-stranded", false, "discard unreachable unrelated checkpoints")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("annotate takes no positional arguments"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	annotate := provenance.Annotate
	if *dropStranded {
		annotate = provenance.AnnotateDroppingStranded
	}
	result, err := annotate(repo)
	if err != nil {
		return annotateOperationalError(env, command.name, repo, err)
	}
	writeWarnings(env, result.Warnings)
	writeAnnotateResult(env.Stdout, result)
	return ExitSuccess, nil
}

func writeAnnotateResult(out io.Writer, result provenance.AnnotateResult) {
	if result.Skipped {
		fmt.Fprintf(out, "skipped %s\n", result.Commit)
	} else if result.Noop {
		fmt.Fprintf(out, "already annotated %s\n", result.Commit)
	} else {
		fmt.Fprintf(out, "annotated %s (%d files)\n", result.Commit, result.Files)
	}
}

func annotateOperationalError(
	env *Env,
	name string,
	repo *gitcmd.Repo,
	err error,
) (int, error) {
	seq, base, ok := provenance.UnrelatedCheckpoint(err)
	if !ok {
		return operationalError(env, name, err)
	}
	head, headErr := repo.Head()
	if headErr != nil {
		return operationalError(env, name, err)
	}
	if base == "" {
		base = "(before first commit)"
	}
	detail := fmt.Errorf(
		"attribution pending for %s\ncheckpoint %d belongs to base %s\nrun: git-byline recover\ncause: %w",
		shortCommit(head), seq, base, err,
	)
	return operationalError(env, name, detail)
}

func runBlame(env *Env, command *command, args []string) (int, error) {
	jsonOutput, mode, rest, err := parseBlameFlags(args)
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
	writeBlameText(env.Stdout, result.Lines, useColor(mode, env.Stdout))
	return ExitSuccess, nil
}

// parseBlameFlags accepts the blame flags without a flag set, so the file
// argument can follow or precede them.
func parseBlameFlags(args []string) (bool, colorMode, []string, error) {
	jsonOutput := false
	mode := colorAuto
	colorSet := false
	var rest []string
	for _, arg := range args {
		switch {
		case arg == "--json":
			if jsonOutput {
				return false, mode, nil, errors.New("--json specified more than once")
			}
			jsonOutput = true
		case arg == "-h" || arg == "--help":
			return false, mode, nil, flag.ErrHelp
		case strings.HasPrefix(arg, "--color="):
			if colorSet {
				return false, mode, nil, errors.New("--color specified more than once")
			}
			parsed, err := parseColorMode(strings.TrimPrefix(arg, "--color="))
			if err != nil {
				return false, mode, nil, err
			}
			mode = parsed
			colorSet = true
		case strings.HasPrefix(arg, "-"):
			return false, mode, nil, fmt.Errorf("unknown flag %q", arg)
		default:
			rest = append(rest, arg)
		}
	}
	return jsonOutput, mode, rest, nil
}

// writeBlameText prints one aligned row per line. The label column is sized
// from the widest label so a long human-override label cannot push the line
// numbers out of alignment.
func writeBlameText(out io.Writer, lines []provenance.BlameLine, color bool) {
	labels := make([]string, len(lines))
	width := 0
	for index, line := range lines {
		labels[index] = line.Attribution.Label()
		if count := utf8.RuneCountInString(labels[index]); count > width {
			width = count
		}
	}
	numberWidth := len(strconv.Itoa(len(lines)))
	if numberWidth < 3 {
		numberWidth = 3
	}
	for index, line := range lines {
		label := fmt.Sprintf("%-*s", width, labels[index])
		number := fmt.Sprintf("%*d |", numberWidth, line.Number)
		if color {
			label = labelColor(line.Attribution.Author) + label + ansiReset
			number = ansiDim + number + ansiReset
		}
		fmt.Fprintf(out, "%s %s %s\n", label, number, line.Content)
	}
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
	fmt.Fprintf(env.Stdout, "Annotation pending: %t\n", result.AnnotationPending)
	fmt.Fprintf(env.Stdout, "Pending checkpoints: %d\n", result.PendingCheckpoints)
	fmt.Fprintf(env.Stdout, "Pending files: %d\n", result.PendingFiles)
	fmt.Fprintf(env.Stdout, "Retained snapshots: %d\n", result.RetainedSnapshots)
	fmt.Fprintf(env.Stdout, "Unrelated checkpoints: %d\n", result.UnrelatedCheckpoints)
	fmt.Fprintf(env.Stdout, "Stranded checkpoints: %d\n", result.StrandedCheckpoints)
	fmt.Fprintf(env.Stdout, "Blocked checkpoints: %d\n", result.BlockedCheckpoints)
	if result.RecommendedAction != "" {
		fmt.Fprintf(env.Stdout, "Recommended action: %s\n", result.RecommendedAction)
	}
	writeWarnings(env, result.Warnings)
	return ExitSuccess, nil
}

// runInstallHooks merges managed agent and Git hooks and provisions the
// forge attribution workflow for a public forge origin.
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
	if len(result.Changed) == 0 && !result.ConfigChanged {
		fmt.Fprintln(env.Stdout, "hooks already installed")
	}
	if result.ConfigChanged {
		fmt.Fprintln(env.Stdout, "updated global git config init.templateDir")
	}
	if options.Git && !options.LocalNotes {
		fmt.Fprintln(env.Stdout, "attribution notes will be pushed automatically")
	}
	provisionForgeWorkflow(env, dir, options)
	return ExitSuccess, nil
}

// provisionForgeWorkflow creates the forge attribution workflow when the
// origin remote points at a supported public forge. Provisioning belongs
// to Git-hook installation with shared notes and is skipped for --template
// scope, which has no repository remote. Detection or installation
// problems warn and leave the exit code unchanged.
func provisionForgeWorkflow(env *Env, dir string, options hooks.Options) {
	if !options.Git || options.LocalNotes || options.Template {
		return
	}
	provider, found := ci.DetectProvider(dir)
	if !found {
		fmt.Fprintln(env.Stdout, "no GitHub or GitLab remote detected; run: git-byline ci install --provider github|gitlab")
		return
	}
	result, err := ci.Install(dir, provider)
	if err != nil {
		fmt.Fprintf(env.Stderr, "warning: could not install the forge workflow: %v\n", err)
		return
	}
	if result.Changed {
		fmt.Fprintf(env.Stdout, "created %s\n", result.Path)
		return
	}
	fmt.Fprintf(env.Stdout, "workflow already installed at %s\n", result.Path)
}

// runUninstall removes managed hooks and the template-matching forge
// attribution workflow.
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
	if options.Git && !options.Template {
		removed, removeErr := ci.Uninstall(dir)
		result.Changed = append(result.Changed, removed...)
		if removeErr != nil {
			fmt.Fprintf(env.Stderr, "warning: could not remove the forge workflow: %v\n", removeErr)
		}
	}
	for _, path := range result.Changed {
		fmt.Fprintf(env.Stdout, "updated %s\n", path)
	}
	if len(result.Changed) == 0 && !result.ConfigChanged {
		fmt.Fprintln(env.Stdout, "no managed hooks found")
	}
	if result.ConfigChanged {
		fmt.Fprintln(env.Stdout, "removed global git config init.templateDir")
	}
	return ExitSuccess, nil
}

func parseHookOptions(command *command, args []string) (hooks.Options, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	flags.SetOutput(new(strings.Builder))
	agent := flags.String("agent", "all", "droid, claude, all, or none")
	gitHook := flags.Bool("git", false, "manage Git attribution hooks")
	localNotes := flags.Bool("local-notes", false, "disable automatic attribution note sharing")
	user := flags.Bool("user", false, "use user agent configuration")
	project := flags.Bool("project", false, "use project agent configuration")
	template := flags.Bool("template", false, "manage Git hooks in the git-byline Git template directory")
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
	if *localNotes && !*gitHook {
		return hooks.Options{}, errors.New("--local-notes requires --git")
	}
	if *template && !*gitHook {
		return hooks.Options{}, errors.New("--template requires --git")
	}
	if *template && *agent != "none" && !*user {
		return hooks.Options{}, errors.New("--template requires --agent none for project scope")
	}
	return hooks.Options{Agent: *agent, Git: *gitHook, User: *user, LocalNotes: *localNotes, Template: *template}, nil
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
