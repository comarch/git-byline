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
	outputPath := ""
	outputSpecified := false
	flags.Func("output", "output file", func(value string) error {
		outputPath = value
		outputSpecified = true
		return nil
	})
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
	if !outputSpecified {
		if _, err := env.Stdout.Write(data); err != nil {
			return operationalError(env, command.name, fmt.Errorf("write disclosure: %w", err))
		}
		return ExitSuccess, nil
	}
	file, path, err := createDisclosureOutput(env, outputPath)
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
		return nil, "", errors.New("disclosure output path is required")
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
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return nil, "", fmt.Errorf("create disclosure temp file for %s: %w", path, err)
	}
	tempPath := file.Name()
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		_ = os.Remove(tempPath)
		return nil, "", fmt.Errorf("set disclosure temp permissions for %s: %w", path, err)
	}
	return file, path, nil
}

func writeDisclosureOutput(file *os.File, path string, data []byte) error {
	return writeDisclosureOutputWithLink(file, path, data, os.Link)
}

func writeDisclosureOutputWithLink(
	file *os.File,
	path string,
	data []byte,
	link func(string, string) error,
) error {
	tempPath := file.Name()
	success := false
	published := false
	defer func() {
		_ = file.Close()
		_ = os.Remove(tempPath)
		if published && !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write disclosure temp file for %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync disclosure temp file for %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close disclosure temp file for %s: %w", path, err)
	}
	if err := link(tempPath, path); err != nil {
		return fmt.Errorf("publish disclosure %s: %w", path, err)
	}
	published = true
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat published disclosure %s: %w", path, err)
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("published disclosure %s has permissions %o, want 600", path, info.Mode().Perm())
	}
	success = true
	return nil
}
