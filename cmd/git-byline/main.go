// Command git-byline is the git-byline entry point.
//
// main only wires the process streams and exits; all behavior lives in
// internal/app so it can be tested without spawning processes.
package main

import (
	"os"

	"github.com/comarch/git-byline/internal/app"
)

func main() {
	env := &app.Env{
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}
	code, err := app.Run(os.Args[1:], env)
	if err != nil && code == app.ExitSuccess {
		// Run reports user-facing diagnostics through env streams. An
		// error paired with a success code is a programming bug; treat it
		// as an operational failure rather than exiting 0.
		code = app.ExitFailure
	}
	os.Exit(code)
}
