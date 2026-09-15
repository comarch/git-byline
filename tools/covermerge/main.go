// Command covermerge merges Go coverage profiles into one.
//
// It combines a `go test -coverprofile` profile with profiles produced
// by `go tool covdata textfmt` from binaries built with `go build
// -cover`. The merged profile is what coverage gates measure, so
// statements covered only through spawned binaries (package main) still
// count. The merge logic lives in internal/covermerge and is shared
// with tools/validate.
//
// Usage:
//
//	covermerge -o merged.out unit.out child.out [...]
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/comarch/git-byline/internal/covermerge"
)

func main() {
	out := flag.String("o", "", "output profile path (required)")
	flag.Parse()
	code, err := run(*out, flag.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "covermerge: %v\n", err)
		os.Exit(code)
	}
}

// run merges the input profiles into out. It returns a usage exit code
// for argument problems and 1 for operational failures, matching the
// project CLI contract.
func run(out string, inputs []string) (int, error) {
	if out == "" {
		return 2, errors.New("missing -o output path")
	}
	if len(inputs) == 0 {
		return 2, errors.New("no input profiles")
	}
	covered, blocks, err := covermerge.MergeFiles(inputs, out)
	if err != nil {
		return 1, err
	}
	fmt.Fprintf(os.Stdout, "covermerge: merged %d blocks, %.1f%% of statements covered\n", blocks, covered)
	return 0, nil
}
