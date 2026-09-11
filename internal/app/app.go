// Package app implements git-byline command dispatch and handlers.
//
// Run is the single entry point used by cmd/git-byline. Handlers receive
// their input and output streams through Env and never touch os.Stdin,
// os.Stdout, or os.Stderr directly, which keeps every behavior testable
// without spawning processes.
package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/comarch/git-byline/internal/version"
)

// Exit codes returned by Run. Usage errors are separated from operational
// failures so scripts can react precisely, following the convention of git
// and the standard flag package.
const (
	// ExitSuccess reports a completed command.
	ExitSuccess = 0
	// ExitFailure reports an operational failure.
	ExitFailure = 1
	// ExitUsage reports a usage error: an unknown command, an unknown
	// flag, or wrong arguments.
	ExitUsage = 2
)

// Env carries the streams a command reads and writes.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Dir    string
	Now    func() time.Time
}

// command is a single git-byline subcommand.
type command struct {
	name  string                                                 // invocation name, as typed by the user
	short string                                                 // one-line description shown in the command list
	usage string                                                 // full usage text, printed by help
	run   func(env *Env, c *command, args []string) (int, error) // command handler
}

// commands returns the command registry in help order.
func commands() []*command {
	return []*command{
		{
			name:  "checkpoint",
			short: "record a human or AI edit snapshot",
			usage: "Usage: git-byline checkpoint <droid|claude|agent-v1|portable-AGENT> [--type human|ai] --hook-input stdin\n\n" +
				"Read one agent hook event from stdin and record allowed file snapshots.",
			run: runCheckpoint,
		},
		{
			name:  "annotate",
			short: "write attribution for HEAD to git notes",
			usage: "Usage: git-byline annotate\n\n" +
				"Replay pending checkpoints and annotate the current commit.",
			run: runAnnotate,
		},
		{
			name:  "blame",
			short: "show line-level attribution",
			usage: "Usage: git-byline blame [--json] <file>\n\n" +
				"Show human, AI, human-override, or untracked attribution for every line at HEAD.",
			run: runBlame,
		},
		{
			name:  "status",
			short: "show repository attribution state",
			usage: "Usage: git-byline status [--json]\n\n" +
				"Show pending checkpoints, retained snapshots, and annotation state.",
			run: runStatus,
		},
		{
			name:  "dashboard",
			short: "generate a local HTML attribution report",
			usage: "Usage: git-byline dashboard [--range <rev-range>] [--repo] [--output FILE] [file]\n\n" +
				"Generate a self-contained HTML report for one file or every\n" +
				"file attributed on HEAD. Use --range or --repo for a bounded\n" +
				"repository report without source lines. Existing output files\n" +
				"are not replaced.",
			run: runDashboard,
		},
		{
			name:  "stats",
			short: "aggregate attribution statistics",
			usage: "Usage: git-byline stats [<rev-range>] [--json]\n\n" +
				"Show attribution totals and deterministic breakdowns without reading blobs.",
			run: runStats,
		},
		{
			name:  "verify",
			short: "verify attribution notes",
			usage: "Usage: git-byline verify [<rev-range>] [--deep] [--json]\n\n" +
				"Check attribution notes, blob identity, and range coverage.\n" +
				"--deep reads blobs to verify exact line counts.",
			run: runVerify,
		},
		{
			name:  "check",
			short: "check attribution policy limits",
			usage: "Usage: git-byline check [<rev-range>] [--max-ai-percent N] [--max-untracked-percent N] [--require-note] [--json]\n\n" +
				"Evaluate attribution policy flags. A policy violation exits 1.",
			run: runCheck,
		},
		{
			name:  "disclosure",
			short: "write machine-readable AI disclosure input",
			usage: "Usage: git-byline disclosure [--range <rev-range>] [--format json|spdx|cyclonedx] [--output FILE]\n\n" +
				"Write private, deterministic disclosure input from attribution notes.\n" +
				"The output is not a compliance certificate and existing files are not replaced.",
			run: runDisclosure,
		},
		{
			name:  "rewrite",
			short: "preserve attribution across Git rewrites",
			usage: "Usage: git-byline rewrite --mode <post-rewrite|post-checkout|post-merge|ref-txn|stash-apply> --hook-input stdin [hook arguments]\n\n" +
				"Reproject attribution for Git history and worktree transitions.",
			run: runRewrite,
		},
		{
			name:  "ci",
			short: "reconstruct attribution after forge merges",
			usage: "Usage: git-byline ci install --provider github|gitlab\n" +
				"       git-byline ci run --provider github|gitlab [--base REV --source REV --target REV --mode auto|squash|rebase]\n\n" +
				"Install a forge workflow or reconstruct attribution locally after\n" +
				"a squash or rebase merge. The binary never pushes notes.",
			run: runCI,
		},
		{
			name:  "export",
			short: "export attribution to an interop format",
			usage: "Usage: git-byline export --format gitai|agent-trace [--commit REV] [--output FILE]\n\n" +
				"Export the selected commit's attribution without replacing output files.",
			run: runExport,
		},
		{
			name:  "import",
			short: "import Git AI attribution notes",
			usage: "Usage: git-byline import --format gitai [--range REV-RANGE] [--dry-run]\n\n" +
				"Import Git AI notes without replacing different byline notes.",
			run: runImport,
		},
		{
			name:  "install-hooks",
			short: "install agent and Git hooks",
			usage: "Usage: git-byline install-hooks [--agent droid|claude|all|none] [--git] [--local-notes] [--user|--project]\n\n" +
				"Merge managed hooks without replacing existing configuration.\n" +
				"Git hooks share attribution notes on push unless --local-notes is set.",
			run: runInstallHooks,
		},
		{
			name:  "uninstall",
			short: "remove managed hooks",
			usage: "Usage: git-byline uninstall [--agent droid|claude|all|none] [--git] [--user|--project]\n\n" +
				"Remove only configuration managed by git-byline.",
			run: runUninstall,
		},
		{
			name:  "help",
			short: "show help for a command",
			usage: "Usage: git-byline help [command]\n\n" +
				"Show general usage, or detailed usage for a single command.",
			run: runHelp,
		},
		{
			name:  "version",
			short: "show the git-byline version",
			usage: "Usage: git-byline version\n\n" +
				"Print the git-byline version and exit. Release builds report the\n" +
				"release tag; local builds report dev.",
			run: runVersion,
		},
	}
}

// rootUsage returns the general usage text, derived from the command
// registry so the command list cannot drift from the registry itself.
func rootUsage() string {
	return buildRootUsage()
}

func buildRootUsage() string {
	var b strings.Builder
	b.WriteString("Usage: git-byline <command> [flags]\n")
	b.WriteString("\n")
	b.WriteString("Track human and AI authorship line by line, from agent edits\n")
	b.WriteString("to commits. No cloud, no daemon, no network.\n")
	b.WriteString("\n")
	b.WriteString("Commands:\n")
	for _, c := range commands() {
		fmt.Fprintf(&b, "  %-14s %s\n", c.name, c.short)
	}
	b.WriteString("\nRun 'git-byline help <command>' for details about a command.")
	return b.String()
}

func (env *Env) workingDir() (string, error) {
	if env.Dir != "" {
		return env.Dir, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read working directory: %w", err)
	}
	return dir, nil
}

func (env *Env) now() time.Time {
	if env.Now != nil {
		return env.Now()
	}
	return time.Now()
}

// lookup returns the registered command with the given name, or nil.
func lookup(name string) *command {
	for _, c := range commands() {
		if c.name == name {
			return c
		}
	}
	return nil
}

// Run dispatches args, the command line without the program name, and
// returns the process exit code together with the error that caused a
// non-zero code, if any. The caller decides how to exit the process.
func Run(args []string, env *Env) (int, error) {
	if env == nil {
		return ExitFailure, errors.New("app: env is nil")
	}
	if env.Stdin == nil || env.Stdout == nil || env.Stderr == nil {
		return ExitFailure, errors.New("app: env streams must not be nil")
	}
	if len(args) == 0 {
		return rootUsageError(env, errors.New("no command given"))
	}
	c := lookup(args[0])
	if c == nil {
		return rootUsageError(env, fmt.Errorf("unknown command %q", args[0]))
	}
	return c.run(env, c, args[1:])
}

// rootUsageError reports a dispatch-level usage problem on the error stream
// and returns the usage exit code.
func rootUsageError(env *Env, err error) (int, error) {
	fmt.Fprintf(env.Stderr, "git-byline: %v\n\n%s\n", err, rootUsage())
	return ExitUsage, fmt.Errorf("git-byline: usage: %w", err)
}

// commandUsageError reports a command-level usage problem on the error
// stream, followed by the command usage, and returns the usage exit code.
func commandUsageError(env *Env, c *command, err error) (int, error) {
	fmt.Fprintf(env.Stderr, "git-byline %s: %v\n\n%s\n", c.name, err, c.usage)
	return ExitUsage, fmt.Errorf("git-byline %s: usage: %w", c.name, err)
}

// runHelp implements the help command.
func runHelp(env *Env, c *command, args []string) (int, error) {
	switch len(args) {
	case 0:
		fmt.Fprintln(env.Stdout, rootUsage())
		return ExitSuccess, nil
	case 1:
		detail := lookup(args[0])
		if detail == nil {
			return rootUsageError(env, fmt.Errorf("unknown command %q", args[0]))
		}
		fmt.Fprintln(env.Stdout, detail.usage)
		return ExitSuccess, nil
	default:
		return commandUsageError(env, c, errors.New("help takes at most one command"))
	}
}

// runVersion implements the version command. It defines a flag set only to
// reject unknown flags and handle the help flag consistently with the rest
// of the CLI; version itself takes no options.
func runVersion(env *Env, c *command, args []string) (int, error) {
	fs := flag.NewFlagSet("git-byline "+c.name, flag.ContinueOnError)
	var flagOutput strings.Builder
	fs.SetOutput(&flagOutput)
	fs.Usage = func() {
		fmt.Fprintln(&flagOutput, c.usage)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(env.Stdout, c.usage)
			return ExitSuccess, nil
		}
		fmt.Fprint(env.Stderr, flagOutput.String())
		return ExitUsage, fmt.Errorf("git-byline %s: %w", c.name, err)
	}
	if fs.NArg() > 0 {
		return commandUsageError(env, c, fmt.Errorf("version takes no arguments, got %q", fs.Arg(0)))
	}
	fmt.Fprintf(env.Stdout, "git-byline %s\n", version.Version)
	return ExitSuccess, nil
}
