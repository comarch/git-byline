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

// tarEntry is one entry in a test archive. A mode of 0 marks a symlink.
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
func buildZip(t *testing.T, path string, entries []struct {
	name    string
	content string
}) {
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

// updateFixture builds one staged release with an old target binary and
// returns everything applyUpdate needs.
func updateFixture(t *testing.T, binaryContent string) (updateOptions, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "git-byline_1.0.0_linux_amd64.tar.gz")
	buildTarGZ(t, archivePath, releaseTarEntries(binaryContent))
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "git-byline")
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

// testEnv returns a fully wired Env around two buffers.
func testEnv(stdout *bytes.Buffer) *Env {
	return &Env{Stdin: &bytes.Buffer{}, Stdout: stdout, Stderr: &bytes.Buffer{}}
}

func TestApplyUpdateTarGZReplacesTarget(t *testing.T) {
	options, stdout := updateFixture(t, "new binary")
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	got, err := os.ReadFile(options.targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Errorf("target = %q, want the extracted binary", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(options.targetPath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("target permissions = %o, want 755", info.Mode().Perm())
		}
	}
	if !strings.Contains(stdout.String(), "Updated git-byline at ") {
		t.Errorf("stdout = %q, want an update report", stdout.String())
	}
	if leftover := stagingLeftovers(t, filepath.Dir(options.targetPath)); leftover != "" {
		t.Errorf("staging file %s was not cleaned up", leftover)
	}
}

func TestApplyUpdateDryRunLeavesTarget(t *testing.T) {
	options, stdout := updateFixture(t, "new binary")
	options.dryRun = true
	if err := applyUpdate(testEnv(stdout), options); err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	got, err := os.ReadFile(options.targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old binary" {
		t.Errorf("target = %q, want the original binary", got)
	}
	if !strings.Contains(stdout.String(), "Dry run: verified ") {
		t.Errorf("stdout = %q, want a dry-run report", stdout.String())
	}
	if leftover := stagingLeftovers(t, filepath.Dir(options.targetPath)); leftover != "" {
		t.Errorf("staging file %s was not cleaned up", leftover)
	}
}

func TestApplyUpdateZipReplacesTarget(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "git-byline_1.0.0_windows_amd64.zip")
	buildZip(t, archivePath, []struct {
		name    string
		content string
	}{
		{"LICENSE", "MIT"},
		{"README.md", "git-byline"},
		{"SECURITY.md", "policy"},
		{"git-byline.exe", "new windows binary"},
	})
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "git-byline.exe")
	if err := os.WriteFile(target, []byte("old windows binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = applyUpdate(testEnv(&stdout), updateOptions{
		archivePath:   archivePath,
		checksumsPath: writeChecksums(t, dir, filepath.Base(archivePath), archive),
		targetPath:    target,
	})
	if err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new windows binary" {
		t.Errorf("target = %q, want the extracted binary", got)
	}
}

// TestApplyUpdateIgnoresAppleDouble mirrors the installer rule: tar-side
// AppleDouble metadata never fails an otherwise valid release archive.
func TestApplyUpdateIgnoresAppleDouble(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "git-byline_1.0.0_linux_amd64.tar.gz")
	entries := append(releaseTarEntries("new binary"),
		tarEntry{name: "._git-byline", content: "resource fork", mode: 0o644})
	buildTarGZ(t, archivePath, entries)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "git-byline")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err = applyUpdate(testEnv(&stdout), updateOptions{
		archivePath:   archivePath,
		checksumsPath: writeChecksums(t, dir, filepath.Base(archivePath), archive),
		targetPath:    target,
	})
	if err != nil {
		t.Fatalf("applyUpdate: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new binary" {
		t.Errorf("target = %q, want the extracted binary", got)
	}
}

func TestApplyUpdateFailures(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T) error
		want string
	}{
		{
			name: "checksum mismatch",
			run: func(t *testing.T) error {
				options, _ := updateFixture(t, "new binary")
				zero := strings.Repeat("0", 64)
				if err := os.WriteFile(options.checksumsPath,
					[]byte(zero+"  "+filepath.Base(options.archivePath)+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return applyUpdate(testEnv(&bytes.Buffer{}), options)
			},
			want: "checksum mismatch",
		},
		{
			name: "missing checksum entry",
			run: func(t *testing.T) error {
				options, _ := updateFixture(t, "new binary")
				options.checksumsPath = filepath.Join(t.TempDir(), "other-checksums.txt")
				if err := os.WriteFile(options.checksumsPath, []byte("contents"), 0o600); err != nil {
					t.Fatal(err)
				}
				return applyUpdate(testEnv(&bytes.Buffer{}), options)
			},
			want: "no checksum entry",
		},
		{
			name: "duplicate checksum entry",
			run: func(t *testing.T) error {
				options, _ := updateFixture(t, "new binary")
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
				return applyUpdate(testEnv(&bytes.Buffer{}), options)
			},
			want: "duplicate checksum entry",
		},
		{
			name: "missing checksums file",
			run: func(t *testing.T) error {
				options, _ := updateFixture(t, "new binary")
				options.checksumsPath = filepath.Join(t.TempDir(), "absent.txt")
				return applyUpdate(testEnv(&bytes.Buffer{}), options)
			},
			want: "open checksums",
		},
		{
			name: "extra archive file",
			run: func(t *testing.T) error {
				dir := t.TempDir()
				archivePath := filepath.Join(dir, "git-byline_1.0.0_linux_amd64.tar.gz")
				entries := append(releaseTarEntries("new binary"),
					tarEntry{name: "surprise.txt", content: "extra", mode: 0o644})
				buildTarGZ(t, archivePath, entries)
				return applyUpdateOnFixture(t, dir, archivePath)
			},
			want: "unexpected files",
		},
		{
			name: "symlink in place of the binary",
			run: func(t *testing.T) error {
				dir := t.TempDir()
				archivePath := filepath.Join(dir, "git-byline_1.0.0_linux_amd64.tar.gz")
				entries := []tarEntry{
					{"LICENSE", "MIT", 0o644},
					{"README.md", "git-byline", 0o644},
					{"SECURITY.md", "policy", 0o644},
					{"git-byline", "/etc/passwd", 0},
				}
				buildTarGZ(t, archivePath, entries)
				return applyUpdateOnFixture(t, dir, archivePath)
			},
			want: "not a regular file",
		},
		{
			name: "missing binary entry",
			run: func(t *testing.T) error {
				dir := t.TempDir()
				archivePath := filepath.Join(dir, "git-byline_1.0.0_linux_amd64.tar.gz")
				entries := []tarEntry{
					{"LICENSE", "MIT", 0o644},
					{"README.md", "git-byline", 0o644},
					{"SECURITY.md", "policy", 0o644},
				}
				buildTarGZ(t, archivePath, entries)
				return applyUpdateOnFixture(t, dir, archivePath)
			},
			want: "is missing git-byline",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run(t)
			if err == nil {
				t.Fatalf("applyUpdate succeeded, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

// applyUpdateOnFixture stages an archive against a fresh target and runs
// applyUpdate, for rejection cases that build custom archives.
func applyUpdateOnFixture(t *testing.T, dir, archivePath string) error {
	t.Helper()
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "git-byline")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return applyUpdate(testEnv(&bytes.Buffer{}), updateOptions{
		archivePath:   archivePath,
		checksumsPath: writeChecksums(t, dir, filepath.Base(archivePath), archive),
		targetPath:    target,
	})
}

// TestApplyUpdateFailureLeavesTarget checks the fail-closed contract: a
// rejection leaves the running binary untouched.
func TestApplyUpdateFailureLeavesTarget(t *testing.T) {
	options, _ := updateFixture(t, "new binary")
	options.checksumsPath = filepath.Join(t.TempDir(), "absent.txt")
	err := applyUpdate(testEnv(&bytes.Buffer{}), options)
	if err == nil {
		t.Fatal("applyUpdate succeeded, want error")
	}
	got, readErr := os.ReadFile(options.targetPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old binary" {
		t.Errorf("target = %q, want the original binary after a failed update", got)
	}
}

// stagingLeftovers returns one leftover staging file name in dir, or "".
func stagingLeftovers(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".git-byline-update-") {
			return entry.Name()
		}
	}
	return ""
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
