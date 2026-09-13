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
	if !supportedUpdateArchive(archivePath, runtime.GOOS) {
		return commandUsageError(env, command, errors.New("unsupported archive: expected .tar.gz on Linux and macOS, .zip on Windows"))
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
	// The archive is hashed and read through one private staging copy, so
	// a file swapped at the original path between the checksum check and
	// extraction cannot bypass verification.
	verifiedArchive, err := stageArchiveCopy(o.archivePath, filepath.Dir(o.targetPath))
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(verifiedArchive) }()
	actual, err := hashFile(verifiedArchive)
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s", o.archivePath)
	}
	staged, err := extractUpdateBinary(verifiedArchive, filepath.Dir(o.targetPath))
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

// supportedUpdateArchive reports whether the archive name matches the
// release layout for one operating system: .tar.gz on Linux and macOS,
// .zip on Windows. A checksummed archive for the wrong system would pass
// verification and install a binary that cannot run there.
func supportedUpdateArchive(archivePath, goos string) bool {
	lower := strings.ToLower(archivePath)
	if goos == "windows" {
		return strings.HasSuffix(lower, ".zip")
	}
	return strings.HasSuffix(lower, ".tar.gz")
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

// stageArchiveCopy copies the archive into a private staging file in
// targetDir, bounded by the update archive cap. The staging name keeps the
// archive suffix, because extraction dispatches on it. The caller removes it.
func stageArchiveCopy(archivePath, targetDir string) (string, error) {
	source, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("open archive: %w", err)
	}
	defer source.Close()
	suffix := ".tar.gz"
	if updateIsZip(archivePath) {
		suffix = ".zip"
	}
	staged, err := os.CreateTemp(targetDir, ".git-byline-archive-*"+suffix)
	if err != nil {
		return "", fmt.Errorf("create archive staging file: %w", err)
	}
	kept := false
	defer func() {
		if !kept {
			_ = staged.Close()
			_ = os.Remove(staged.Name())
		}
	}()
	written, err := io.Copy(staged, io.LimitReader(source, maxUpdateArchiveBytes+1))
	if err != nil {
		return "", fmt.Errorf("read archive: %w", err)
	}
	if written > maxUpdateArchiveBytes {
		return "", fmt.Errorf("archive exceeds %d bytes", maxUpdateArchiveBytes)
	}
	if err := staged.Close(); err != nil {
		return "", fmt.Errorf("close archive staging file: %w", err)
	}
	kept = true
	return staged.Name(), nil
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
			// Windows refuses to remove an open file, so close before
			// removing on every failure path.
			_ = staged.Close()
			_ = os.Remove(staged.Name())
		}
	}()
	binaryName := "git-byline"
	if updateIsZip(archivePath) {
		binaryName = "git-byline.exe"
	}
	allowlist := []string{"LICENSE", "README.md", "SECURITY.md", binaryName}
	var names []string
	if updateIsZip(archivePath) {
		names, err = readUpdateZip(archivePath, binaryName, staged)
	} else {
		names, err = readUpdateTar(archivePath, binaryName, staged)
	}
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

// updateIsZip reports whether the update reads the Windows zip release
// layout instead of the POSIX tar.gz one.
func updateIsZip(archivePath string) bool {
	return strings.HasSuffix(strings.ToLower(archivePath), ".zip")
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
	var remaining int64 = maxUpdateArchiveBytes
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		// Bound every declared entry, not only the binary: a tiny gzip
		// stream can declare a huge allowlisted entry and force the
		// reader to drain it.
		if header.Size > remaining {
			return nil, fmt.Errorf("archive entry %s exceeds the %d-byte archive budget", header.Name, maxUpdateArchiveBytes)
		}
		remaining -= header.Size
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
	var remaining int64 = maxUpdateArchiveBytes
	for _, entry := range reader.File {
		// Bound every declared entry like the tar path, so a small zip
		// cannot declare an oversized allowlisted file.
		if entry.UncompressedSize64 > uint64(remaining) {
			return nil, fmt.Errorf("archive entry %s exceeds the %d-byte archive budget", entry.Name, maxUpdateArchiveBytes)
		}
		remaining -= int64(entry.UncompressedSize64)
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

// swapBinary replaces target with staged. On Windows the running image
// cannot be overwritten in place, so the old binary moves aside first; when
// the old file cannot be deleted because this process still runs, its path
// is returned for the caller to report.
func swapBinary(staged, target string) (string, error) {
	return swapBinaryFunc(runtime.GOOS, os.Rename, os.Remove, staged, target)
}

// swapBinaryFunc is swapBinary with the platform and the file operations
// injected, so the Windows failure paths stay testable on every platform.
// When the swap fails, the previous binary is restored; if the restore also
// fails, the error says so and where the previous binary remains, rather
// than hiding the recovery path.
func swapBinaryFunc(goos string, rename func(string, string) error, remove func(string) error, staged, target string) (string, error) {
	if goos != "windows" {
		if err := rename(staged, target); err != nil {
			return "", fmt.Errorf("replace binary: %w", err)
		}
		return "", nil
	}
	old := target + ".old"
	_ = remove(old)
	if err := rename(target, old); err != nil {
		return "", fmt.Errorf("move current binary away: %w", err)
	}
	if err := rename(staged, target); err != nil {
		if restoreErr := rename(old, target); restoreErr != nil {
			return "", fmt.Errorf("place new binary: %w; restoring the previous binary also failed: %v; the previous binary remains at %s", err, restoreErr, old)
		}
		return "", fmt.Errorf("place new binary: %w", err)
	}
	if err := remove(old); err != nil {
		return old, nil
	}
	return "", nil
}
