// Command validate runs the full git-byline validation pipeline.
//
// It is the single local gate every change must pass before it is
// pushed: formatting, vet, tests with a coverage floor, release builds
// for all six supported targets, dependency and import policy,
// PromptScript checks, and repository scans. The first failing stage
// stops the run.
//
// The pipeline itself never reaches out to the network: every go
// invocation runs with GOPROXY=off and CGO disabled.
//
// The optional -stages flag narrows the run to a comma-separated stage
// subset, mainly so coverage collection can exercise validate itself
// as a spawned binary without re-entering the pipeline.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stage is one named validation step.
type stage struct {
	name string
	run  func(root string) error
}

func main() {
	stageSpec := flag.String("stages", "", "comma-separated subset of stages to run (default: all)")
	flag.Parse()
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(1)
	}
	stages, err := selectStages(pipelineStages(), *stageSpec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(2)
	}
	os.Exit(runPipeline(os.Stdout, os.Stderr, stages, root))
}

// selectStages filters the pipeline to the stages named in spec,
// keeping pipeline order. An empty spec keeps every stage. Unknown or
// empty names are usage errors so typos cannot silently skip stages.
func selectStages(stages []stage, spec string) ([]stage, error) {
	if spec == "" {
		return stages, nil
	}
	wanted := make(map[string]bool)
	for _, name := range strings.Split(spec, ",") {
		if name = strings.TrimSpace(name); name == "" {
			return nil, fmt.Errorf("empty stage name in %q", spec)
		}
		wanted[name] = true
	}
	var selected []stage
	for _, s := range stages {
		if wanted[s.name] {
			selected = append(selected, s)
			delete(wanted, s.name)
		}
	}
	if len(wanted) > 0 {
		names := make([]string, 0, len(wanted))
		for name := range wanted {
			names = append(names, name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("unknown stages: %s", strings.Join(names, ", "))
	}
	return selected, nil
}

// pipelineStages returns the validation stages in execution order.
func pipelineStages() []stage {
	return []stage{
		{name: "gofmt", run: checkGofmt},
		{name: "vet", run: checkVet},
		{name: "test", run: checkTest},
		{name: "coverage", run: checkCoverage},
		{name: "build", run: checkBuilds},
		{name: "deps", run: checkDependencies},
		{name: "imports", run: checkForbiddenImports},
		{name: "installers", run: checkInstallers},
		{name: "promptscript", run: checkPromptScript},
		{name: "scans", run: checkScans},
	}
}

// runPipeline executes the stages in order, stops at the first failure,
// and returns the process exit code.
func runPipeline(stdout, stderr io.Writer, stages []stage, root string) int {
	for _, s := range stages {
		start := time.Now()
		err := s.run(root)
		elapsed := time.Since(start).Round(time.Millisecond)
		if err != nil {
			fmt.Fprintf(stderr, "FAIL %s (%v): %v\n", s.name, elapsed, err)
			fmt.Fprintf(stderr, "validate: stage %s failed, stopping\n", s.name)
			return 1
		}
		fmt.Fprintf(stdout, "ok   %s (%v)\n", s.name, elapsed)
	}
	fmt.Fprintf(stdout, "validate: all %d stages passed\n", len(stages))
	return 0
}

// repoRoot returns the repository root directory, located through the
// go.mod file of the current module.
func repoRoot() (string, error) {
	out, err := goCmd{}.run("env", "GOMOD")
	if err != nil {
		return "", fmt.Errorf("locate repository root: %w", err)
	}
	modfile := strings.TrimSpace(out)
	if modfile == "" || modfile == os.DevNull {
		return "", errors.New("not inside a Go module")
	}
	return filepath.Dir(modfile), nil
}
