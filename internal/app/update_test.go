package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeChecksums writes a sha256sum-style checksums file for one archive
// name and returns its path.
func writeChecksums(t *testing.T, dir, archiveName string, archive []byte) string {
	t.Helper()
	sum := sha256.Sum256(archive)
	path := filepath.Join(dir, "checksums.txt")
	content := hex.EncodeToString(sum[:]) + "  " + archiveName + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// tarEntry is one entry in a test tar.gz. A mode of 0 marks a symlink.
type tarEntry struct {
	name    string
	content string
	mode    int64
}

// releaseTarEntries returns the release allowlist plus the binary.
func releaseTarEntries(binaryContent string) []tarEntry {
	return []tarEntry{
		{"LICENSE", "MIT", 0o644},
		{"README.md", "git-byline", 0o644},
		{"SECURITY.md", "policy", 0o644},
		{"git-byline", binaryContent, 0o755},
	}
}

// zipEntry is one entry in a test zip.
type zipEntry struct {
	name    string
	content string
}

// releaseZipEntries returns the release allowlist plus the Windows binary.
func releaseZipEntries(binaryContent string) []zipEntry {
	return []zipEntry{
		{"LICENSE", "MIT"},
		{"README.md", "git-byline"},
		{"SECURITY.md", "policy"},
		{"git-byline.exe", binaryContent},
	}
}

// buildTarGZ writes a tar.gz from explicit entries.
func buildTarGZ(t *testing.T, path string, entries []tarEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipWriter := gzip.NewWriter(file)
	defer gzipWriter.Close()
	writer := tar.NewWriter(gzipWriter)
	defer writer.Close()
	for _, entry := range entries {
		header := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     entry.name,
			Size:     int64(len(entry.content)),
			Mode:     entry.mode,
		}
		if entry.mode == 0 {
			header.Typeflag = tar.TypeSymlink
			header.Linkname = entry.content
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write([]byte(entry.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// buildZip writes a zip archive from explicit entries.
func buildZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	defer writer.Close()
	for _, entry := range entries {
		output, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := output.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}
}

// stageUpdate turns a built archive into a full update request against a
// fresh target binary, with valid checksums.
func stageUpdate(t *testing.T, dir, archivePath, binaryName string) (updateOptions, *bytes.Buffer) {
	t.Helper()
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, binaryName)
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	return updateOptions{
		archivePath:   archivePath,
		checksumsPath: writeChecksums(t, dir, filepath.Base(archivePath), archive),
		targetPath:    target,
	}, &stdout
}

// stageTarUpdate builds a tar.gz release and stages it for an update.
func stageTarUpdate(t *testing.T, archiveName string, entries []tarEntry) (updateOptions, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, archiveName)
	buildTarGZ(t, archivePath, entries)
	return stageUpdate(t, dir, archivePath, "git-byline")
}

// stageZipUpdate builds a zip release and stages it for an update.
func stageZipUpdate(t *testing.T, archiveName string, entries []zipEntry) (updateOptions, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, archiveName)
	buildZip(t, archivePath, entries)
	return stageUpdate(t, dir, archivePath, "git-byline.exe")
}

// testEnv returns a fully wired Env around a stdout buffer.
func testEnv(stdout *bytes.Buffer) *Env {
	return &Env{Stdin: &bytes.Buffer{}, Stdout: stdout, Stderr: &bytes.Buffer{}}
}

// assertTarget checks the target binary content and, on POSIX, its mode.
func assertTarget(t *testing.T, target, want string) {
	t.Helper()
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("target = %q, want %q", got, want)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("target permissions = %o, want 755", info.Mode().Perm())
	}
}

// assertNoStagingLeftovers fails when a staging file was not cleaned up.
func assertNoStagingLeftovers(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".git-byline-update-") {
			t.Errorf("staging file %s was not cleaned up", entry.Name())
		}
	}
}

func TestApplyUpdateTarGZReplacesTarget(t *testing.T) {
	options, stdout := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz",
		releaseTarEntries("new binary"))
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	assertTarget(t, options.targetPath, "new binary")
	if !strings.Contains(stdout.String(), "Updated git-byline at ") {
		t.Errorf("stdout = %q, want an update report", stdout.String())
	}
	assertNoStagingLeftovers(t, filepath.Dir(options.targetPath))
}

func TestApplyUpdateDryRunLeavesTarget(t *testing.T) {
	options, stdout := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz",
		releaseTarEntries("new binary"))
	options.dryRun = true
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	assertTarget(t, options.targetPath, "old binary")
	if !strings.Contains(stdout.String(), "Dry run: verified ") {
		t.Errorf("stdout = %q, want a dry-run report", stdout.String())
	}
	assertNoStagingLeftovers(t, filepath.Dir(options.targetPath))
}

func TestApplyUpdateZipReplacesTarget(t *testing.T) {
	options, stdout := stageZipUpdate(t, "git-byline_1.0.0_windows_amd64.zip",
		releaseZipEntries("new windows binary"))
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	assertTarget(t, options.targetPath, "new windows binary")
}

// TestApplyUpdateIgnoresAppleDouble mirrors the installer rule: tar-side
// AppleDouble metadata never fails an otherwise valid release archive.
func TestApplyUpdateIgnoresAppleDouble(t *testing.T) {
	entries := append(releaseTarEntries("new binary"),
		tarEntry{name: "._git-byline", content: "resource fork", mode: 0o644})
	options, stdout := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz", entries)
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	assertTarget(t, options.targetPath, "new binary")
}

// TestApplyUpdateArchiveRejections covers every rejection that reads the
// archive itself. Each case must leave the target binary untouched.
func TestApplyUpdateArchiveRejections(t *testing.T) {
	cases := []struct {
		name    string
		entries []tarEntry
		want    string
	}{
		{
			name: "extra archive file",
			entries: append(releaseTarEntries("new binary"),
				tarEntry{name: "surprise.txt", content: "extra", mode: 0o644}),
			want: "unexpected files",
		},
		{
			name: "symlink in place of the binary",
			entries: []tarEntry{
				{"LICENSE", "MIT", 0o644},
				{"README.md", "git-byline", 0o644},
				{"SECURITY.md", "policy", 0o644},
				{"git-byline", "/etc/passwd", 0},
			},
			want: "not a regular file",
		},
		{
			name: "missing binary entry",
			entries: []tarEntry{
				{"LICENSE", "MIT", 0o644},
				{"README.md", "git-byline", 0o644},
				{"SECURITY.md", "policy", 0o644},
			},
			want: "is missing git-byline",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			options, _ := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz", tt.entries)
			err := applyUpdate(testEnv(&bytes.Buffer{}), options)
			if err == nil {
				t.Fatalf("applyUpdate succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			assertTarget(t, options.targetPath, "old binary")
		})
	}
}

// TestApplyUpdateChecksumRejections covers the checksums-side rejections.
func TestApplyUpdateChecksumRejections(t *testing.T) {
	stage := func(t *testing.T) updateOptions {
		options, _ := stageTarUpdate(t, "git-byline_1.0.0_linux_amd64.tar.gz",
			releaseTarEntries("new binary"))
		return options
	}
	cases := []struct {
		name string
		want string
		mut  func(t *testing.T, options *updateOptions)
	}{
		{
			name: "checksum mismatch",
			want: "checksum mismatch",
			mut: func(t *testing.T, options *updateOptions) {
				zero := strings.Repeat("0", 64)
				name := filepath.Base(options.archivePath)
				if err := os.WriteFile(options.checksumsPath,
					[]byte(zero+"  "+name+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing checksum entry",
			want: "no checksum entry",
			mut: func(t *testing.T, options *updateOptions) {
				options.checksumsPath = filepath.Join(t.TempDir(), "other-checksums.txt")
				if err := os.WriteFile(options.checksumsPath, []byte("contents"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "duplicate checksum entry",
			want: "duplicate checksum entry",
			mut: func(t *testing.T, options *updateOptions) {
				archive, err := os.ReadFile(options.archivePath)
				if err != nil {
					t.Fatal(err)
				}
				sumBytes := sha256.Sum256(archive)
				sum := hex.EncodeToString(sumBytes[:])
				name := filepath.Base(options.archivePath)
				if err := os.WriteFile(options.checksumsPath,
					[]byte(sum+"  "+name+"\n"+sum+"  "+name+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing checksums file",
			want: "open checksums",
			mut: func(t *testing.T, options *updateOptions) {
				options.checksumsPath = filepath.Join(t.TempDir(), "absent.txt")
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			options := stage(t)
			tt.mut(t, &options)
			err := applyUpdate(testEnv(&bytes.Buffer{}), options)
			if err == nil {
				t.Fatalf("applyUpdate succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
			assertTarget(t, options.targetPath, "old binary")
		})
	}
}

func TestReadChecksumFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	write := func(content string) {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name    string
		content string
		want    string
		wantErr string
	}{
		{
			name:    "binary marker is accepted",
			content: "aa" + strings.Repeat("0", 62) + " *release.tar.gz\n",
			want:    "aa" + strings.Repeat("0", 62),
		},
		{
			name:    "malformed lines are ignored",
			content: "not a checksum line\n\nbb" + strings.Repeat("0", 62) + "  release.tar.gz\n",
			want:    "bb" + strings.Repeat("0", 62),
		},
		{
			name:    "wrong name is ignored",
			content: "cc" + strings.Repeat("0", 62) + "  other.tar.gz\n",
			wantErr: "no checksum entry",
		},
		{
			name:    "invalid hex is rejected",
			content: "zz" + strings.Repeat("z", 62) + "  release.tar.gz\n",
			wantErr: "invalid checksum entry",
		},
		{
			name:    "short hash is rejected",
			content: "abc123  release.tar.gz\n",
			wantErr: "invalid checksum entry",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			write(tt.content)
			got, err := readChecksumFor(path, "release.tar.gz")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("readChecksumFor error = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("readChecksumFor: %v", err)
			}
			if got != tt.want {
				t.Errorf("readChecksumFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSupportedUpdateArchive(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"git-byline_1.0.0_linux_amd64.tar.gz", true},
		{"git-byline_1.0.0_macOS_arm64.TAR.GZ", true},
		{"git-byline_1.0.0_windows_amd64.zip", true},
		{"git-byline_1.0.0_linux_amd64.tar.xz", false},
		{"checksums.txt", false},
	}
	for _, tt := range cases {
		if got := supportedUpdateArchive(tt.path); got != tt.want {
			t.Errorf("supportedUpdateArchive(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestRunUpdateOperationalFailure checks that a missing checksums file is
// an operational failure (exit 1), not a usage error.
func TestRunUpdateOperationalFailure(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	env := &Env{Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr}
	args := []string{"update",
		"--archive", filepath.Join(dir, "absent.tar.gz"),
		"--checksums", filepath.Join(dir, "absent.txt")}
	code, err := Run(args, env)
	if code != ExitFailure {
		t.Errorf("Run code = %d, want %d", code, ExitFailure)
	}
	if err == nil {
		t.Error("Run returned nil error, want an operational failure")
	}
	if !strings.Contains(stderr.String(), "open checksums") {
		t.Errorf("stderr = %q, want a checksums failure", stderr.String())
	}
}
