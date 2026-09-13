package app

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/version"
)

// Update input caps bound untrusted local files before any expensive work.
// A release archive is a few tens of MiB, so anything larger is not a
// git-byline release.
const (
	maxUpdateArchiveBytes  = 256 << 20
	maxUpdateChecksumBytes = 1 << 20
)

// runUpdate implements the update command: verify a staged release archive
// against its checksums and replace the running binary in place. The binary
// never downloads anything; the user fetches the archive and checksums.
func runUpdate(env *Env, command *command, args []string) (int, error) {
	flags := flag.NewFlagSet(command.name, flag.ContinueOnError)
	var output strings.Builder
	flags.SetOutput(&output)
	archivePath := ""
	archiveSpecified := false
	flags.Func("archive", "path to the staged release archive", func(value string) error {
		if archiveSpecified {
			return errors.New("--archive specified more than once")
		}
		archivePath = value
		archiveSpecified = true
		return nil
	})
	checksumsPath := ""
	checksumsSpecified := false
	flags.Func("checksums", "path to the release checksums.txt", func(value string) error {
		if checksumsSpecified {
			return errors.New("--checksums specified more than once")
		}
		checksumsPath = value
		checksumsSpecified = true
		return nil
	})
	dryRun := flags.Bool("dry-run", false, "verify the archive without replacing the binary")
	if err := flags.Parse(args); err != nil {
		return flagError(env, command, output.String(), err)
	}
	if flags.NArg() != 0 {
		return commandUsageError(env, command, errors.New("update takes no positional arguments"))
	}
	if !archiveSpecified || !checksumsSpecified {
		return commandUsageError(env, command, errors.New("--archive and --checksums are required"))
	}
	if !supportedUpdateArchive(archivePath) {
		return commandUsageError(env, command, errors.New("unsupported archive: expected .tar.gz (Linux, macOS) or .zip (Windows)"))
	}
	target, err := currentExecutable()
	if err != nil {
		return operationalError(env, command.name, err)
	}
	if err := applyUpdate(env, updateOptions{
		archivePath:   archivePath,
		checksumsPath: checksumsPath,
		targetPath:    target,
		dryRun:        *dryRun,
	}); err != nil {
		return operationalError(env, command.name, err)
	}
	return ExitSuccess, nil
}

// updateOptions is one local update request.
type updateOptions struct {
	archivePath   string
	checksumsPath string
	targetPath    string
	dryRun        bool
}

// applyUpdate verifies the archive, extracts the new binary next to the
// target, and swaps it in. Every failure path leaves the target unchanged.
func applyUpdate(env *Env, o updateOptions) error {
	expected, err := readChecksumFor(o.checksumsPath, filepath.Base(o.archivePath))
	if err != nil {
		return err
	}
	actual, err := hashFile(o.archivePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", o.archivePath)
	}
	staged, err := extractUpdateBinary(o.archivePath, filepath.Dir(o.targetPath))
	if err != nil {
		return err
	}
	swapped := false
	defer func() {
		if !swapped {
			_ = os.Remove(staged)
		}
	}()
	if o.dryRun {
		fmt.Fprintf(env.Stdout, "Dry run: verified %s; would replace %s\n", o.archivePath, o.targetPath)
		return nil
	}
	leftOld, err := swapBinary(staged, o.targetPath)
	if err != nil {
		return err
	}
	swapped = true
	fmt.Fprintf(env.Stdout, "Updated git-byline at %s (was %s)\n", o.targetPath, version.Version)
	if leftOld != "" {
		fmt.Fprintf(env.Stdout, "The previous binary stays at %s until this process exits; delete it afterwards.\n", leftOld)
	}
	fmt.Fprintln(env.Stdout, "Run 'git-byline version' to confirm the new version.")
	return nil
}

// currentExecutable returns the absolute path of the running binary with
// symlinks resolved, so an update replaces the real file, not a link.
func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate running binary: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve running binary path: %w", err)
	}
	return resolved, nil
}

// supportedUpdateArchive reports whether the archive name uses a supported
// release layout: .tar.gz for Linux and macOS, .zip for Windows.
func supportedUpdateArchive(archivePath string) bool {
	lower := strings.ToLower(archivePath)
	return strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".zip")
}

// readChecksumFor returns the SHA-256 value recorded for one archive in a
// checksums.txt file, rejecting missing and duplicate entries.
func readChecksumFor(checksumsPath, archiveName string) (string, error) {
	file, err := os.Open(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("open checksums: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxUpdateChecksumBytes+1))
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	if len(data) > maxUpdateChecksumBytes {
		return "", fmt.Errorf("checksums exceed %d bytes", maxUpdateChecksumBytes)
	}
	expected := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name != archiveName {
			continue
		}
		if expected != "" {
			return "", fmt.Errorf("duplicate checksum entry for %s", archiveName)
		}
		sum := strings.ToLower(fields[0])
		if len(sum) != sha256.Size*2 {
			return "", fmt.Errorf("invalid checksum entry for %s", archiveName)
		}
		if _, err := hex.DecodeString(sum); err != nil {
			return "", fmt.Errorf("invalid checksum entry for %s", archiveName)
		}
		expected = sum
	}
	if expected == "" {
		return "", fmt.Errorf("no checksum entry for %s in %s", archiveName, checksumsPath)
	}
	return expected, nil
}

// hashFile returns the lowercase SHA-256 of one file, bounded by the update
// archive cap.
func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	hasher := sha256.New()
	written, err := io.Copy(hasher, io.LimitReader(file, maxUpdateArchiveBytes+1))
	if err != nil {
		return "", fmt.Errorf("read archive: %w", err)
	}
	if written > maxUpdateArchiveBytes {
		return "", fmt.Errorf("archive exceeds %d bytes", maxUpdateArchiveBytes)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// extractUpdateBinary pulls exactly the release binary out of a verified
// archive into a staging file in targetDir, enforcing the same entry
// allowlist the installers enforce.
func extractUpdateBinary(archivePath, targetDir string) (string, error) {
	staged, err := os.CreateTemp(targetDir, ".git-byline-update-*")
	if err != nil {
		return "", fmt.Errorf("create staging file: %w", err)
	}
	kept := false
	defer func() {
		if !kept {
			_ = os.Remove(staged.Name())
		}
	}()
	binaryName := "git-byline"
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		binaryName = "git-byline.exe"
	}
	allowlist := []string{"LICENSE", "README.md", "SECURITY.md", binaryName}
	names, err := readUpdateArchive(archivePath, binaryName, staged)
	if err != nil {
		return "", err
	}
	sort.Strings(allowlist)
	if !sameStringSlice(names, allowlist) {
		return "", fmt.Errorf("unexpected files in %s", archivePath)
	}
	if runtime.GOOS != "windows" {
		if err := staged.Chmod(0o755); err != nil {
			return "", fmt.Errorf("set staging permissions: %w", err)
		}
	}
	if err := staged.Close(); err != nil {
		return "", fmt.Errorf("close staging file: %w", err)
	}
	kept = true
	return staged.Name(), nil
}

// readUpdateArchive walks one archive, copies the release binary into staged,
// and returns the sorted entry names it contains.
func readUpdateArchive(archivePath, binaryName string, staged *os.File) ([]string, error) {
	if strings.HasSuffix(strings.ToLower(archivePath), ".zip") {
		return readUpdateZip(archivePath, binaryName, staged)
	}
	return readUpdateTar(archivePath, binaryName, staged)
}

func readUpdateTar(archivePath, binaryName string, staged *os.File) ([]string, error) {
	file, err := os.Open(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("read gzip stream: %w", err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	names := []string{}
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		name := updateEntryName(header.Name)
		if name == "" {
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("archive entry %s is not a regular file", name)
		}
		names = append(names, name)
		if name != binaryName {
			continue
		}
		written, err := io.Copy(staged, io.LimitReader(reader, maxUpdateArchiveBytes+1))
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}
		if written > maxUpdateArchiveBytes {
			return nil, fmt.Errorf("extracted %s exceeds %d bytes", name, maxUpdateArchiveBytes)
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("archive is missing %s", binaryName)
	}
	sort.Strings(names)
	return names, nil
}

func readUpdateZip(archivePath, binaryName string, staged *os.File) ([]string, error) {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer reader.Close()
	names := []string{}
	found := false
	for _, entry := range reader.File {
		name := updateEntryName(entry.Name)
		if name == "" {
			continue
		}
		if !entry.FileInfo().Mode().IsRegular() {
			return nil, fmt.Errorf("archive entry %s is not a regular file", name)
		}
		names = append(names, name)
		if name != binaryName {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("open archive entry %s: %w", name, err)
		}
		written, copyErr := io.Copy(staged, io.LimitReader(source, maxUpdateArchiveBytes+1))
		closeErr := source.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("extract %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close archive entry %s: %w", name, closeErr)
		}
		if written > maxUpdateArchiveBytes {
			return nil, fmt.Errorf("extracted %s exceeds %d bytes", name, maxUpdateArchiveBytes)
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("archive is missing %s", binaryName)
	}
	sort.Strings(names)
	return names, nil
}

// updateEntryName normalizes one archive entry name and returns "" for
// entries the installers ignore: directories and macOS AppleDouble files.
func updateEntryName(name string) string {
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasSuffix(name, "/") {
		return ""
	}
	if strings.HasPrefix(filepath.Base(name), "._") {
		return ""
	}
	return name
}

// sameStringSlice reports whether two sorted slices are equal.
func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// swapBinary replaces target with staged. On Windows the running image cannot
// be overwritten in place, so the old binary moves aside first; when the old
// file cannot be deleted because this process still runs, its path is
// returned for the caller to report. On every error path the target is left
// unchanged.
func swapBinary(staged, target string) (string, error) {
	if runtime.GOOS != "windows" {
		if err := os.Rename(staged, target); err != nil {
			return "", fmt.Errorf("replace binary: %w", err)
		}
		return "", nil
	}
	old := target + ".old"
	_ = os.Remove(old)
	if err := os.Rename(target, old); err != nil {
		return "", fmt.Errorf("move current binary away: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		_ = os.Rename(old, target)
		return "", fmt.Errorf("place new binary: %w", err)
	}
	if err := os.Remove(old); err != nil {
		return old, nil
	}
	return "", nil
}
