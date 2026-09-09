package app

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/comarch/git-byline/internal/version"
)

// runCase describes one CLI dispatch contract case.
type runCase struct {
	name     string
	args     []string
	code     int
	stdout   string // substring that must appear in stdout
	stderr   string // substring that must appear in stderr
	noStdout bool   // stdout must be completely empty
	noStderr bool   // stderr must be completely empty
}

// runCases covers the CLI dispatch contract: exit codes,
// stdout/stderr separation, and the exact version output.
var runCases = []runCase{
	{
		name:   "no command prints usage on stderr and exits 2",
		args:   nil,
		code:   ExitUsage,
		stderr: "Usage: git-byline <command>", noStdout: true,
	},
	{
		name:   "help prints root usage on stdout and exits 0",
		args:   []string{"help"},
		code:   ExitSuccess,
		stdout: "Usage: git-byline <command>", noStderr: true,
	},
	{
		name:   "help for a known command prints its usage on stdout",
		args:   []string{"help", "version"},
		code:   ExitSuccess,
		stdout: "Usage: git-byline version", noStderr: true,
	},
	{
		name:   "help help prints the help usage",
		args:   []string{"help", "help"},
		code:   ExitSuccess,
		stdout: "Usage: git-byline help", noStderr: true,
	},
	{
		name:   "help for an unknown command exits 2 with usage on stderr",
		args:   []string{"help", "bogus"},
		code:   ExitUsage,
		stderr: `unknown command "bogus"`, noStdout: true,
	},
	{
		name:   "help with two arguments exits 2",
		args:   []string{"help", "one", "two"},
		code:   ExitUsage,
		stderr: "help takes at most one command", noStdout: true,
	},
	{
		name:   "version prints the default version and exits 0",
		args:   []string{"version"},
		code:   ExitSuccess,
		stdout: "git-byline dev\n", noStderr: true,
	},
	{
		name:   "version with a positional argument exits 2",
		args:   []string{"version", "extra"},
		code:   ExitUsage,
		stderr: `version takes no arguments, got "extra"`, noStdout: true,
	},
	{
		name:   "version with an unknown flag exits 2 and reports the flag",
		args:   []string{"version", "--bogus"},
		code:   ExitUsage,
		stderr: "flag provided but not defined: -bogus", noStdout: true,
	},
	{
		name:   "version help flag prints usage on stdout and exits 0",
		args:   []string{"version", "-h"},
		code:   ExitSuccess,
		stdout: "Usage: git-byline version", noStderr: true,
	},
	{
		name:   "unknown command exits 2 with usage on stderr",
		args:   []string{"bogus"},
		code:   ExitUsage,
		stderr: `unknown command "bogus"`, noStdout: true,
	},
}

func TestRun(t *testing.T) {
	t.Parallel()
	for _, tt := range runCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			env := &Env{Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr}
			code, err := Run(tt.args, env)

			if code != tt.code {
				t.Errorf("Run(%q) code = %d, want %d (stderr: %q)", tt.args, code, tt.code, stderr.String())
			}
			// Error contract: a non-zero code comes with an error, a zero
			// code comes without one.
			if (code == 0) != (err == nil) {
				t.Errorf("Run(%q) code = %d, err = %v; want code zero iff err nil", tt.args, code, err)
			}
			if tt.stdout != "" && !bytes.Contains(stdout.Bytes(), []byte(tt.stdout)) {
				t.Errorf("stdout = %q, want it to contain %q", stdout.String(), tt.stdout)
			}
			if tt.stderr != "" && !bytes.Contains(stderr.Bytes(), []byte(tt.stderr)) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.stderr)
			}
			if tt.noStdout && stdout.Len() != 0 {
				t.Errorf("stdout = %q, want it empty", stdout.String())
			}
			if tt.noStderr && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", stderr.String())
			}
		})
	}
}

// TestRunVersionInjected verifies that the version command reports the
// version variable verbatim, which is what linker injection overwrites.
// It must stay sequential: it mutates the package-level version variable
// while the parallel TestRun batch is paused, and restores it before the
// batch resumes.
func TestRunVersionInjected(t *testing.T) {
	const injected = "v0.1.0-test"
	old := version.Version
	version.Version = injected
	t.Cleanup(func() { version.Version = old })

	var stdout, stderr bytes.Buffer
	env := &Env{Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr}
	code, err := Run([]string{"version"}, env)
	if code != ExitSuccess || err != nil {
		t.Fatalf("Run(version) = %d, %v; want %d, nil", code, err, ExitSuccess)
	}
	if want := fmt.Sprintf("git-byline %s\n", injected); stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", stderr.String())
	}
}

// TestRootUsageListsAllCommands guards against adding a command to the
// registry without it appearing in the help output.
func TestRootUsageListsAllCommands(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	env := &Env{Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: &stderr}
	if _, err := Run([]string{"help"}, env); err != nil {
		t.Fatalf("Run(help) error: %v", err)
	}
	for _, c := range commands() {
		if !bytes.Contains(stdout.Bytes(), []byte(c.name)) {
			t.Errorf("help output does not list command %q", c.name)
		}
	}
}

// TestRunNilEnv verifies the defensive contract used by main.
func TestRunNilEnv(t *testing.T) {
	t.Parallel()
	code, err := Run([]string{"version"}, nil)
	if code != ExitFailure {
		t.Errorf("Run with nil env code = %d, want %d", code, ExitFailure)
	}
	if err == nil {
		t.Error("Run with nil env returned nil error")
	}

	var stdout bytes.Buffer
	code, err = Run([]string{"version"}, &Env{Stdin: &bytes.Buffer{}, Stdout: &stdout, Stderr: nil})
	if code != ExitFailure || err == nil {
		t.Errorf("Run with nil stderr = %d, %v; want %d, error", code, err, ExitFailure)
	}
}
