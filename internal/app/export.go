package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/comarch/git-byline/internal/interop"
)

func runExport(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	format := flags.String("format", "", "gitai or agent-trace")
	commit := flags.String("commit", "", "commit revision")
	outputPath := flags.String("output", "", "output file")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("export takes no positional arguments"))
	}
	if *format != "gitai" && *format != "agent-trace" {
		return commandUsageError(env, command, errors.New("--format must be gitai or agent-trace"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	revision := *commit
	if revision == "" {
		revision, err = repo.Head()
		if err != nil {
			return operationalError(env, command.name, err)
		}
		if revision == "" {
			return operationalError(env, command.name, errors.New("cannot export from an unborn repository"))
		}
	}
	data, err := interop.Export(repo, *format, revision)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if *outputPath == "" {
		if _, err := env.Stdout.Write(data); err != nil {
			return operationalError(env, command.name, fmt.Errorf("write export: %w", err))
		}
		return ExitSuccess, nil
	}
	file, path, err := createExclusiveOutput(env, *outputPath, "export")
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if err := writeExclusiveOutput(file, path, data, "export"); err != nil {
		return operationalError(env, command.name, err)
	}
	fmt.Fprintln(env.Stdout, path)
	return ExitSuccess, nil
}
