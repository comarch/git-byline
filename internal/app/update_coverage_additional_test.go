package app

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUpdateDryRunAndFlagErrors(t *testing.T) {
	options, _ := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz",
		releaseTarEntries("new binary"))
	env := testEnv(&bytes.Buffer{})
	args := []string{
		"update",
		"--archive", options.archivePath,
		"--checksums", options.checksumsPath,
		"--dry-run",
	}
	code, err := Run(args, env)
	if code != ExitSuccess || err != nil {
		t.Fatalf("update dry run = %d, %v", code, err)
	}
	if !strings.Contains(env.Stdout.(*bytes.Buffer).String(), "Dry run: verified") {
		t.Fatalf("update output = %q", env.Stdout.(*bytes.Buffer).String())
	}

	for _, duplicate := range [][]string{
		{"--archive", options.archivePath, "--archive", options.archivePath},
		{"--checksums", options.checksumsPath, "--checksums", options.checksumsPath},
	} {
		args := append([]string{"update"}, duplicate...)
		code, err := Run(args, testEnv(&bytes.Buffer{}))
		if code != ExitUsage || err == nil {
			t.Fatalf("Run(%v) = %d, %v, want usage error", args, code, err)
		}
	}
}

func TestCurrentExecutableSymlinkFailure(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	restored := false
	defer func() {
		if !restored {
			_ = os.WriteFile(path, data, info.Mode().Perm())
		}
	}()
	if _, err := currentExecutable(); err == nil {
		t.Fatal("currentExecutable succeeded after its binary was removed")
	}
	code, err := Run([]string{
		"update", "--archive", "release.tar.gz", "--checksums", "checksums.txt",
	}, testEnv(&bytes.Buffer{}))
	if code != ExitFailure || err == nil {
		t.Fatalf("update executable failure = %d, %v", code, err)
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	restored = true
}

func TestUpdateChecksumAndArchiveStagingFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	checksums := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(checksums, bytes.Repeat([]byte("x"), maxUpdateChecksumBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readChecksumFor(checksums, "archive.tar.gz"); err == nil {
		t.Fatal("readChecksumFor accepted oversized checksums")
	}
	if _, err := hashFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("hashFile accepted a missing archive")
	}
	if _, err := hashFile(dir); err == nil || !strings.Contains(err.Error(), "read archive") {
		t.Fatalf("hashFile directory = %v", err)
	}
	if _, err := readChecksumFor(dir, "archive.tar.gz"); err == nil ||
		!strings.Contains(err.Error(), "read checksums") {
		t.Fatalf("readChecksumFor directory = %v", err)
	}

	archivePath := filepath.Join(dir, "archive.tar.gz")
	buildTarGZ(t, archivePath, releaseTarEntries("binary"))
	if _, err := stageArchiveCopy(archivePath, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("stageArchiveCopy accepted a missing target directory")
	}
	if _, err := stageArchiveCopy(dir, dir); err == nil || !strings.Contains(err.Error(), "read archive") {
		t.Fatalf("stageArchiveCopy directory = %v", err)
	}
	if _, err := extractUpdateBinary(archivePath, filepath.Join(dir, "missing")); err == nil {
		t.Fatal("extractUpdateBinary accepted a missing target directory")
	}

	missingArchive := filepath.Join(dir, "missing.tar.gz")
	archiveData, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	checksumPath := writeChecksums(t, dir, filepath.Base(missingArchive), archiveData)
	if err := os.Remove(archivePath); err != nil {
		t.Fatal(err)
	}
	err = applyUpdate(testEnv(&bytes.Buffer{}), updateOptions{
		archivePath:   missingArchive,
		checksumsPath: checksumPath,
		targetPath:    filepath.Join(dir, "target"),
	})
	if err == nil || !strings.Contains(err.Error(), "open archive") {
		t.Fatalf("applyUpdate staging failure = %v", err)
	}
}

func TestUpdateArchiveSizeBounds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "large.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxUpdateArchiveBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := stageArchiveCopy(path, dir); err == nil {
		t.Fatal("stageArchiveCopy accepted an oversized archive")
	}
	if _, err := hashFile(path); err == nil {
		t.Fatal("hashFile accepted an oversized archive")
	}
}

func TestUpdateTarReadFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stagedPath := filepath.Join(dir, "staged")
	staged, err := os.OpenFile(stagedPath, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateTar(filepath.Join(dir, "missing.tar.gz"), "git-byline", staged)
	if err == nil || !strings.Contains(err.Error(), "open archive") {
		t.Fatalf("missing tar = %v", err)
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}

	invalidGzip := filepath.Join(dir, "invalid.tar.gz")
	if err := os.WriteFile(invalidGzip, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-invalid"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateTar(invalidGzip, "git-byline", staged)
	if err == nil || !strings.Contains(err.Error(), "read gzip stream") {
		t.Fatalf("invalid gzip = %v", err)
	}
	_ = staged.Close()

	malformedTar := filepath.Join(dir, "malformed.tar.gz")
	file, err := os.Create(malformedTar)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	if _, err := gzipWriter.Write([]byte("not a tar stream")); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-malformed"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateTar(malformedTar, "git-byline", staged)
	if err == nil || !strings.Contains(err.Error(), "read archive") {
		t.Fatalf("malformed tar = %v", err)
	}
	_ = staged.Close()

	validTar := filepath.Join(dir, "valid.tar.gz")
	buildTarGZ(t, validTar, releaseTarEntries("binary"))
	staged, err = os.OpenFile(filepath.Join(dir, "readonly"), os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateTar(validTar, "git-byline", staged)
	if err == nil || !strings.Contains(err.Error(), "extract git-byline") {
		t.Fatalf("tar extraction failure = %v", err)
	}
	_ = staged.Close()
}

func TestUpdateZipReadFailures(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	missingBinary := filepath.Join(dir, "missing.zip")
	buildZip(t, missingBinary, releaseZipEntries("")[0:3])
	staged, err := os.Create(filepath.Join(dir, "staged"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(missingBinary, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "missing git-byline.exe") {
		t.Fatalf("missing zip binary = %v", err)
	}
	_ = staged.Close()

	invalidZip := filepath.Join(dir, "invalid.zip")
	if err := os.WriteFile(invalidZip, []byte("not zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-invalid"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(invalidZip, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "open archive") {
		t.Fatalf("invalid zip = %v", err)
	}
	_ = staged.Close()

	symlinkZip := filepath.Join(dir, "symlink.zip")
	file, err := os.Create(symlinkZip)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "git-byline.exe"}
	header.SetMode(os.ModeSymlink | 0o777)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("target")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-symlink"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(symlinkZip, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("zip symlink = %v", err)
	}
	_ = staged.Close()

	directoryZip := filepath.Join(dir, "directory.zip")
	file, err = os.Create(directoryZip)
	if err != nil {
		t.Fatal(err)
	}
	writer = zip.NewWriter(file)
	header = &zip.FileHeader{Name: "git-byline.exe"}
	header.SetMode(os.ModeDir | 0o755)
	if _, err := writer.CreateHeader(header); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-directory"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(directoryZip, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("zip directory = %v", err)
	}
	_ = staged.Close()

	appleZip := filepath.Join(dir, "apple.zip")
	appleEntries := append([]zipEntry{{name: "._apple", content: "ignored"}}, releaseZipEntries("binary")...)
	buildZip(t, appleZip, appleEntries)
	staged, err = os.Create(filepath.Join(dir, "staged-apple"))
	if err != nil {
		t.Fatal(err)
	}
	names, err := readUpdateZip(appleZip, "git-byline.exe", staged)
	if err != nil || len(names) != 4 {
		t.Fatalf("zip AppleDouble entry = %v, %v", names, err)
	}
	_ = staged.Close()

	validZip := filepath.Join(dir, "valid.zip")
	buildZip(t, validZip, releaseZipEntries("binary"))
	staged, err = os.OpenFile(filepath.Join(dir, "readonly"), os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(validZip, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "extract git-byline.exe") {
		t.Fatalf("zip extraction failure = %v", err)
	}
	_ = staged.Close()

	unsupportedZip := filepath.Join(dir, "unsupported.zip")
	buildZip(t, unsupportedZip, []zipEntry{{name: "git-byline.exe", content: "binary"}})
	data, err := os.ReadFile(unsupportedZip)
	if err != nil {
		t.Fatal(err)
	}
	localOffset := bytes.Index(data, []byte("PK\x03\x04"))
	centralOffset := bytes.Index(data, []byte("PK\x01\x02"))
	if localOffset < 0 || centralOffset < 0 {
		t.Fatal("zip headers not found")
	}
	binary.LittleEndian.PutUint16(data[localOffset+8:localOffset+10], 99)
	binary.LittleEndian.PutUint16(data[centralOffset+10:centralOffset+12], 99)
	if err := os.WriteFile(unsupportedZip, data, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err = os.Create(filepath.Join(dir, "staged-unsupported"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(unsupportedZip, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "open archive entry") {
		t.Fatalf("unsupported zip method = %v", err)
	}
	_ = staged.Close()
}

func TestUpdateZipRejectsOversizedDeclaredEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "oversized.zip")
	buildZip(t, path, []zipEntry{{name: "git-byline.exe", content: "x"}})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("PK\x01\x02")
	offset := bytes.Index(data, marker)
	if offset < 0 {
		t.Fatal("zip central directory not found")
	}
	binary.LittleEndian.PutUint32(data[offset+24:offset+28], uint32(maxUpdateArchiveBytes+1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := os.Create(filepath.Join(dir, "staged"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = readUpdateZip(path, "git-byline.exe", staged)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized zip entry = %v", err)
	}
	_ = staged.Close()
}

func TestUpdateSmallHelpersAndSwapFailures(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", "./", "folder/", "._metadata", "./._metadata"} {
		if got := updateEntryName(name); got != "" {
			t.Errorf("updateEntryName(%q) = %q, want empty", name, got)
		}
	}
	if got := updateEntryName("./README.md"); got != "README.md" {
		t.Fatalf("updateEntryName regular = %q", got)
	}
	if sameStringSlice([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("sameStringSlice accepted different lengths")
	}
	if sameStringSlice([]string{"a"}, []string{"b"}) {
		t.Fatal("sameStringSlice accepted different values")
	}

	sentinel := errors.New("rename failed")
	_, err := swapBinaryFunc("darwin",
		func(string, string) error { return sentinel },
		func(string) error { return nil },
		"staged", "target")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("POSIX swap failure = %v", err)
	}
	_, err = swapBinaryFunc("windows",
		func(string, string) error { return sentinel },
		func(string) error { return nil },
		"staged", "target")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("Windows move failure = %v", err)
	}
	leftOld, err := swapBinaryFunc("windows",
		func(string, string) error { return nil },
		func(string) error { return nil },
		"staged", "target")
	if err != nil || leftOld != "" {
		t.Fatalf("Windows successful swap = %q, %v", leftOld, err)
	}
}

func TestApplyUpdateSwapFailure(t *testing.T) {
	t.Parallel()
	options, _ := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz",
		releaseTarEntries("new binary"))
	targetDirectory := filepath.Join(filepath.Dir(options.targetPath), "target-directory")
	if err := os.Mkdir(targetDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	options.targetPath = targetDirectory
	err := applyUpdate(testEnv(&bytes.Buffer{}), options)
	if err == nil || !strings.Contains(err.Error(), "replace binary") {
		t.Fatalf("applyUpdate swap failure = %v", err)
	}
}

func TestApplyUpdateRejectsOversizedChecksumAndArchiveEntryCleanup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	buildTarGZ(t, archivePath, releaseTarEntries("binary"))
	checksumPath := writeChecksums(t, dir, filepath.Base(archivePath), mustReadFile(t, archivePath))
	if err := os.WriteFile(checksumPath, bytes.Repeat([]byte("x"), maxUpdateChecksumBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := applyUpdate(testEnv(&bytes.Buffer{}), updateOptions{
		archivePath:   archivePath,
		checksumsPath: checksumPath,
		targetPath:    filepath.Join(dir, "target"),
	}); err == nil {
		t.Fatal("applyUpdate accepted oversized checksums")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
