package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestBuildSmoke builds the binary the way the release pipeline does
// (CGO disabled, version injected with linker flags) and runs its version
// command. It verifies the ldflags injection path end to end: the
// injected string must reach the version command output verbatim.
func TestBuildSmoke(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	const injected = "v0.1.0-smoke"
	binName := "git-byline"
	if runtime.GOOS == "windows" {
		// go build writes the exact -o name without adding .exe, and
		// exec.Command cannot resolve an extension-less binary there.
		binName += ".exe"
	}
	bin := filepath.Join(t.TempDir(), binName)
	build := exec.Command("go", "build",
		"-ldflags", "-X github.com/mrwogu/git-byline/internal/version.Version="+injected,
		"-o", bin,
		"./cmd/git-byline",
	)
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	var buildErr bytes.Buffer
	build.Stderr = &buildErr
	if err := build.Run(); err != nil {
		t.Fatalf("go build: %v\n%s", err, buildErr.String())
	}

	run := exec.Command(bin, "version")
	var stdout, stderr bytes.Buffer
	run.Stdout = &stdout
	run.Stderr = &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("built binary version: %v (stderr: %q)", err, stderr.String())
	}
	if want := "git-byline " + injected + "\n"; stdout.String() != want {
		t.Errorf("version output = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("version stderr = %q, want it empty", stderr.String())
	}
}
