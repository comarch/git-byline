package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// resolveOutputPath validates one explicit output path and makes it absolute.
func resolveOutputPath(env *Env, requested, kind string) (string, error) {
	if strings.ContainsRune(requested, 0) {
		return "", errors.New(kind + " output path contains NUL")
	}
	for _, char := range requested {
		if unicode.IsControl(char) {
			return "", errors.New(kind + " output path contains a control character")
		}
	}
	if filepath.IsAbs(requested) {
		return filepath.Clean(requested), nil
	}
	dir, err := env.workingDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Clean(requested)), nil
}

// createExclusiveOutput opens one output file that must not exist yet.
func createExclusiveOutput(env *Env, requested, kind string) (*os.File, string, error) {
	if requested == "-" {
		return nil, "", errors.New(kind + " output must be a file")
	}
	path, err := resolveOutputPath(env, requested, kind)
	if err != nil {
		return nil, "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("create %s %s: %w", kind, path, err)
	}
	return file, path, nil
}

// writeExclusiveOutput writes and closes one output file, removing it on failure.
func writeExclusiveOutput(file *os.File, path string, data []byte, kind string) error {
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("write %s %s: %w", kind, path, err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync %s %s: %w", kind, path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s %s: %w", kind, path, err)
	}
	success = true
	return nil
}
