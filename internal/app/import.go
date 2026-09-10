package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/comarch/git-byline/internal/interop"
)

func runImport(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	format := flags.String("format", "", "gitai")
	revisionRange := flags.String("range", "", "commit revision range")
	dryRun := flags.Bool("dry-run", false, "do not write notes")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("import takes no positional arguments"))
	}
	if *format != "gitai" {
		return commandUsageError(env, command, errors.New("--format must be gitai"))
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	result, err := interop.ImportGitAI(repo, *revisionRange, *dryRun)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	writeWarnings(env, result.Warnings)
	if *dryRun {
		fmt.Fprintf(env.Stdout, "would import %d Git AI notes\n", result.Imported)
	} else {
		fmt.Fprintf(env.Stdout, "imported %d Git AI notes\n", result.Imported)
	}
	return ExitSuccess, nil
}
