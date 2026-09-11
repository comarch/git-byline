package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/provenance"
)

func runRewrite(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	mode := flags.String("mode", "", "post-rewrite, post-checkout, post-merge, or ref-txn")
	hookInput := flags.String("hook-input", "", "must be stdin")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if *hookInput != "stdin" {
		return commandUsageError(env, command, errors.New("--hook-input must be stdin"))
	}
	switch *mode {
	case "post-rewrite", "post-checkout", "post-merge", "ref-txn":
	default:
		return commandUsageError(env, command, fmt.Errorf("unsupported rewrite mode %q", *mode))
	}
	if os.Getenv("GIT_BYLINE_NESTED") != "" && *mode == "ref-txn" {
		return ExitSuccess, nil
	}
	repo, err := discoverForEnv(env)
	if err != nil {
		if *mode == "ref-txn" || *mode == "post-checkout" {
			return ExitSuccess, nil
		}
		return operationalError(env, command.name, err)
	}
	switch *mode {
	case "post-rewrite":
		if flags.NArg() > 1 || (flags.NArg() == 1 && flags.Arg(0) != "rebase" && flags.Arg(0) != "amend") {
			return commandUsageError(env, command, errors.New("post-rewrite accepts at most one hook argument"))
		}
		input, err := readCheckpointInput(env.Stdin)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		result, err := provenance.HandlePostRewrite(repo, strings.NewReader(string(input)))
		if err != nil {
			return operationalError(env, command.name, err)
		}
		writeWarnings(env, result.Warnings)
	case "post-checkout":
		if flags.NArg() != 3 {
			return commandUsageError(env, command, errors.New("post-checkout requires old, new, and branch arguments"))
		}
		if err := validateHookObjectID(flags.Arg(0)); err != nil {
			return commandUsageError(env, command, err)
		}
		if err := validateHookObjectID(flags.Arg(1)); err != nil {
			return commandUsageError(env, command, err)
		}
		if flags.Arg(2) != "0" && flags.Arg(2) != "1" {
			return commandUsageError(env, command, errors.New("post-checkout branch argument must be 0 or 1"))
		}
		result, err := provenance.HandlePostCheckout(repo, flags.Arg(0), flags.Arg(1))
		if err != nil {
			return ExitSuccess, nil
		}
		writeWarnings(env, result.Warnings)
	case "post-merge":
		if flags.NArg() > 1 {
			return commandUsageError(env, command, errors.New("post-merge accepts at most one hook argument"))
		}
		if flags.NArg() == 1 && flags.Arg(0) != "0" && flags.Arg(0) != "1" {
			return commandUsageError(env, command, errors.New("post-merge squash argument must be 0 or 1"))
		}
		result, err := provenance.HandlePostMerge(repo)
		if err != nil {
			return operationalError(env, command.name, err)
		}
		writeWarnings(env, result.Warnings)
	case "ref-txn":
		if flags.NArg() != 1 {
			return ExitSuccess, nil
		}
		if flags.Arg(0) != "preparing" && flags.Arg(0) != "committed" && flags.Arg(0) != "aborted" {
			return ExitSuccess, nil
		}
		input, err := readCheckpointInput(env.Stdin)
		if err != nil {
			return ExitSuccess, nil
		}
		result, err := provenance.HandleReferenceTransaction(repo, strings.NewReader(string(input)), flags.Arg(0))
		if err != nil {
			return ExitSuccess, nil
		}
		writeWarnings(env, result.Warnings)
	}
	return ExitSuccess, nil
}

func validateHookObjectID(value string) error {
	if value == "" {
		return errors.New("hook object ID is empty")
	}
	allZero := true
	for _, char := range value {
		if char != '0' {
			allZero = false
			break
		}
	}
	if !allZero && !model.ValidObjectID(value) {
		return fmt.Errorf("hook object ID %q is invalid", value)
	}
	return nil
}
