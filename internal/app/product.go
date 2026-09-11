package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/dashboard"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/hooks"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/report"
)

var checkpointInputTimeoutNanos atomic.Int64

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
	managedBy := flags.String("managed-by", "", "managed hook owner")
	if err := flags.Parse(args[1:]); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("checkpoint takes no positional arguments after the preset"))
	}
	if *hookInput != "stdin" {
		return commandUsageError(env, command, errors.New("--hook-input must be stdin"))
	}
	if *managedBy != "" && *managedBy != "git-byline" {
		return commandUsageError(env, command, errors.New("--managed-by must be git-byline"))
	}
	explicit := model.Author(*typeName)
	if presetName == "agent-v1" {
		if explicit != "" {
			return commandUsageError(env, command, errors.New("agent-v1 does not accept --type"))
		}
	} else if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return commandUsageError(env, command, errors.New("--type must be human or ai"))
	}
	input, err := readCheckpointInput(env.Stdin)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	event, handled, parseErr := preset.Parse(presetName, explicit, bytes.NewReader(input))
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
	result, err := provenance.Capture(repo, event, env.now())
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	return ExitSuccess, nil
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
		rangeArgs := []string(nil)
		if rangeSpecified {
			rangeArgs = []string{rangeValue}
		}
		from, to, err := parseRevisionRange(rangeArgs, command.name)
		if err != nil {
			return commandUsageError(env, command, err)
		}
		repo, err := discoverForEnv(env)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		aggregate, err := report.Collect(repo, from, to, 0)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		data, err := dashboard.RenderRange(aggregate)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		file, path, err := createDashboardOutput(env, *outputPath)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		if err := writeDashboard(file, path, data); err != nil {
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
	if err := writeDashboard(file, path, data); err != nil {
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
	if requested == "-" {
		return nil, "", errors.New("dashboard output must be a file")
	}
	if strings.ContainsRune(requested, 0) {
		return nil, "", errors.New("dashboard output path contains NUL")
	}
	for _, char := range requested {
		if unicode.IsControl(char) {
			return nil, "", errors.New("dashboard output path contains a control character")
		}
	}
	var path string
	if filepath.IsAbs(requested) {
		path = filepath.Clean(requested)
	} else {
		dir, err := env.workingDir()
		if err != nil {
			return nil, "", err
		}
		path = filepath.Join(dir, filepath.Clean(requested))
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("create dashboard %s: %w", path, err)
	}
	return file, path, nil
}

func writeDashboard(file *os.File, path string, data []byte) error {
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write dashboard %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync dashboard %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close dashboard %s: %w", path, err)
	}
	success = true
	return nil
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
	if options.Git && !options.LocalNotes {
		fmt.Fprintln(env.Stdout, "attribution notes will be pushed automatically")
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
	gitHook := flags.Bool("git", false, "manage Git attribution hooks")
	localNotes := flags.Bool("local-notes", false, "disable automatic attribution note sharing")
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
	if *localNotes && !*gitHook {
		return hooks.Options{}, errors.New("--local-notes requires --git")
	}
	return hooks.Options{Agent: *agent, Git: *gitHook, User: *user, LocalNotes: *localNotes}, nil
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
