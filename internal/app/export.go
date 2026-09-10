package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

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
	file, path, err := createInteropOutput(env, *outputPath)
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if err := writeInteropOutput(file, path, data); err != nil {
		return operationalError(env, command.name, err)
	}
	fmt.Fprintln(env.Stdout, path)
	return ExitSuccess, nil
}

func createInteropOutput(env *Env, requested string) (*os.File, string, error) {
	if requested == "-" {
		return nil, "", errors.New("export output must be a file")
	}
	for _, char := range requested {
		if unicode.IsControl(char) {
			return nil, "", errors.New("export output path contains a control character")
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
		return nil, "", fmt.Errorf("create export %s: %w", path, err)
	}
	return file, path, nil
}

func writeInteropOutput(file *os.File, path string, data []byte) error {
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write export %s: %w", path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync export %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close export %s: %w", path, err)
	}
	success = true
	return nil
}
