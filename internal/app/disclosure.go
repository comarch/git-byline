package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/comarch/git-byline/internal/disclosure"
	"github.com/comarch/git-byline/internal/report"
	"github.com/comarch/git-byline/internal/version"
)

func runDisclosure(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	format := flags.String("format", "json", "json, spdx, or cyclonedx")
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
	outputPath := flags.String("output", "", "output file")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("disclosure takes no positional arguments"))
	}
	if *format != "json" && *format != "spdx" && *format != "cyclonedx" {
		return commandUsageError(env, command, errors.New("--format must be json, spdx, or cyclonedx"))
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
	data, err := disclosure.Render(*format, aggregate, env.now(), version.Version)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	file, path, err := createDisclosureOutput(env, *outputPath)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if err := writeDisclosureOutput(file, path, data); err != nil {
		return operationalError(env, command.name, err)
	}
	fmt.Fprintln(env.Stdout, path)
	return ExitSuccess, nil
}

func createDisclosureOutput(env *Env, requested string) (*os.File, string, error) {
	if requested == "" {
		file, err := os.CreateTemp("", "git-byline-disclosure-*.json")
		if err != nil {
			return nil, "", fmt.Errorf("create disclosure temp file: %w", err)
		}
		return file, file.Name(), nil
	}
	if requested == "-" {
		return nil, "", errors.New("disclosure output must be a file")
	}
	for _, char := range requested {
		if unicode.IsControl(char) {
			return nil, "", errors.New("disclosure output path contains a control character")
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
		return nil, "", fmt.Errorf("create disclosure %s: %w", path, err)
	}
	return file, path, nil
}

func writeDisclosureOutput(file *os.File, path string, data []byte) error {
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write disclosure %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync disclosure %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close disclosure %s: %w", path, err)
	}
	success = true
	return nil
}
