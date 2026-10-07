package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckInstallersOnRepo(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkInstallers(root); err != nil {
		t.Fatal(err)
	}
}

func TestValidateJSONObject(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"valid":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateJSONObject(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateJSONObject(path); err == nil {
		t.Fatal("validateJSONObject accepted a trailing value")
	}
}

func TestShellInstallerVerifiesLocalRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer test")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	fixture := t.TempDir()
	osName := "linux"
	if runtime.GOOS == "darwin" {
		osName = "macOS"
	}
	arch := runtime.GOARCH
	archiveName := fmt.Sprintf("git-byline_1.2.3_%s_%s.tar.gz", osName, arch)
	archivePath := filepath.Join(fixture, archiveName)
	writeInstallerArchive(t, archivePath)
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName)
	if err := os.WriteFile(filepath.Join(fixture, "checksums.txt"), []byte(checksum), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	agentHome := t.TempDir()
	xdgConfig := filepath.Join(agentHome, "xdg")
	for _, path := range []string{
		filepath.Join(xdgConfig, "opencode"),
		filepath.Join(agentHome, ".vscode"),
		filepath.Join(agentHome, ".grok"),
	} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatalf("create agent config directory: %v", err)
		}
	}
	fakeCurl := filepath.Join(bin, "curl")
	curlScript := "#!/bin/sh\n" +
		"out=\nurl=\n" +
		"while [ \"$#\" -gt 0 ]; do\n" +
		"  case \"$1\" in -o) out=\"$2\"; shift 2;; *) url=\"$1\"; shift;; esac\n" +
		"done\n" +
		"cp \"$INSTALLER_FIXTURE/${url##*/}\" \"$out\"\n"
	if err := os.WriteFile(fakeCurl, []byte(curlScript), 0o700); err != nil {
		t.Fatal(err)
	}
	installDir := t.TempDir()
	command := exec.Command(
		"sh",
		filepath.Join(root, "install.sh"),
		"--version", "v1.2.3",
		"--bin-dir", installDir,
		"--no-git-hook",
	)
	command.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+agentHome,
		"XDG_CONFIG_HOME="+xdgConfig,
		"INSTALLER_FIXTURE="+fixture,
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh: %v\n%s", err, out)
	}
	for _, want := range []string{
		"-o .opencode/plugins/promptscript.ts",
		"marketplace/harness/opencode/promptscript.ts",
		"-o .github/hooks/promptscript.json",
		"marketplace/harness/copilot/hooks.json",
		"-o .grok/hooks/promptscript.json",
		"marketplace/harness/grok/hooks.json",
	} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("install.sh output missing %q:\n%s", want, out)
		}
	}
	installed := filepath.Join(installDir, "git-byline")
	if out, err := exec.Command(installed, "version").CombinedOutput(); err != nil ||
		string(out) != "git-byline v1.2.3\n" {
		t.Fatalf("installed version = %q, %v", out, err)
	}

	hookLog := filepath.Join(t.TempDir(), "hooks.log")
	command = exec.Command(
		"sh",
		filepath.Join(root, "install.sh"),
		"--version", "v1.2.3",
		"--bin-dir", installDir,
		"--no-git-hook",
		"--no-agent-hooks",
		"--git-template",
	)
	command.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+agentHome,
		"XDG_CONFIG_HOME="+xdgConfig,
		"INSTALLER_FIXTURE="+fixture,
		"INSTALLER_HOOK_LOG="+hookLog,
	)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("repeat install.sh: %v\n%s", err, out)
	}
	log, err := os.ReadFile(hookLog)
	if err != nil {
		t.Fatal(err)
	}
	if string(log) != "install-hooks --agent none --git --template\n" {
		t.Fatalf("template hook invocation = %q", log)
	}

	if err := os.WriteFile(
		filepath.Join(fixture, "checksums.txt"),
		[]byte(fmt.Sprintf("%064d  %s\n", 0, archiveName)),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	rejectedDir := t.TempDir()
	command = exec.Command(
		"sh",
		filepath.Join(root, "install.sh"),
		"--version", "v1.2.3",
		"--bin-dir", rejectedDir,
		"--no-git-hook",
	)
	command.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"HOME="+agentHome,
		"XDG_CONFIG_HOME="+xdgConfig,
		"INSTALLER_FIXTURE="+fixture,
	)
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("install.sh accepted a checksum mismatch: %s", out)
	}
	if _, err := os.Stat(filepath.Join(rejectedDir, "git-byline")); !os.IsNotExist(err) {
		t.Fatalf("rejected install created a binary: %v", err)
	}
}

func writeInstallerArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	content := []byte(`#!/bin/sh
if [ "${1:-}" = "version" ]; then
	printf '%s\n' 'git-byline v1.2.3'
	exit 0
fi
if [ "${1:-}" = "install-hooks" ] && [ -n "${INSTALLER_HOOK_LOG:-}" ]; then
	printf '%s\n' "$*" >>"$INSTALLER_HOOK_LOG"
fi
`)
	files := map[string][]byte{
		"LICENSE":     []byte("license\n"),
		"README.md":   []byte("readme\n"),
		"SECURITY.md": []byte("security\n"),
		"git-byline":  content,
	}
	for _, name := range []string{"LICENSE", "README.md", "SECURITY.md", "git-byline"} {
		if err := archive.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(files[name])),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
