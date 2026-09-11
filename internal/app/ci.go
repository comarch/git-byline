package app

import (
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/comarch/git-byline/internal/ci"
)

func runCI(env *Env, command *command, args []string) (int, error) {
	if len(args) == 0 {
		return commandUsageError(env, command, errors.New("ci action is required"))
	}
	action := args[0]
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	providerName := flags.String("provider", "", "github or gitlab")
	base := flags.String("base", "", "source base commit")
	source := flags.String("source", "", "source tip commit")
	target := flags.String("target", "", "target tip commit")
	mode := flags.String("mode", "", "auto, squash, or rebase")
	if err := flags.Parse(args[1:]); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("ci takes no positional arguments after the action"))
	}
	if action == "run" {
		if *mode != "" && *mode != "auto" && *mode != "squash" && *mode != "rebase" {
			return commandUsageError(env, command, fmt.Errorf("unsupported CI merge mode %q", *mode))
		}
		for _, revision := range []struct {
			name  string
			value string
		}{
			{"base", *base},
			{"source", *source},
			{"target", *target},
		} {
			if err := validateCIRevision(revision.name, revision.value); err != nil {
				return commandUsageError(env, command, err)
			}
		}
	}
	provider, err := ci.ParseProvider(*providerName)
	if err != nil {
		return commandUsageError(env, command, err)
	}
	switch action {
	case "install":
		if *base != "" || *source != "" || *target != "" || *mode != "" {
			return commandUsageError(env, command, errors.New("ci install accepts only --provider"))
		}
		dir, err := env.workingDir()
		if err != nil {
			return operationalError(env, command.name, err)
		}
		result, err := ci.Install(dir, provider)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		if result.Changed {
			fmt.Fprintf(env.Stdout, "created %s\n", result.Path)
		} else {
			fmt.Fprintf(env.Stdout, "workflow already installed at %s\n", result.Path)
		}
		return ExitSuccess, nil
	case "run":
		repo, err := discoverForEnv(env)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		result, err := ci.Run(repo, provider, ci.RunOptions{
			Base: *base, Source: *source, Target: *target, Mode: *mode,
		})
		if err != nil {
			return operationalError(env, command.name, err)
		}
		writeWarnings(env, result.Warnings)
		fmt.Fprintf(env.Stdout, "reconstructed %s attribution: %d mapped, %d notes written\n",
			result.Mode, result.Mapped, result.Written)
		return ExitSuccess, nil
	default:
		return commandUsageError(env, command, fmt.Errorf("unsupported ci action %q", action))
	}
}

func validateCIRevision(name, value string) error {
	if value == "" {
		return nil
	}
	if len(value) != 40 && len(value) != 64 {
		return fmt.Errorf("--%s must be a full commit object ID", name)
	}
	for _, char := range value {
		if (char < '0' || char > '9') &&
			(char < 'a' || char > 'f') &&
			(char < 'A' || char > 'F') {
			return fmt.Errorf("--%s must be a full commit object ID", name)
		}
	}
	if strings.Trim(value, "0") == "" {
		return fmt.Errorf("--%s must not be an all-zero object ID", name)
	}
	return nil
}
