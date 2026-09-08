// checks.go implements the Go toolchain stages of the validation
// pipeline. Every stage runs the go tool through goCmd, which forces CGO
// off and the module proxy off, so the pipeline itself is pure Go and
// network-free.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// coverageFloor is the minimum total statement coverage the test suite
// must reach. It is a floor for honest behavior coverage, not a target
// to game.
const coverageFloor = 80.0

// modulePathOf returns the module path of the module rooted at root.
func modulePathOf(root string) (string, error) {
	out, err := goCmd{dir: root}.run("list", "-m")
	if err != nil {
		return "", fmt.Errorf("determine module path: %w", err)
	}
	mod := strings.TrimSpace(out)
	if mod == "" {
		return "", errors.New("go list -m produced no module path")
	}
	return mod, nil
}

// releaseTargets lists the GOOS/GOARCH pairs the release pipeline ships.
var releaseTargets = []struct{ GOOS, GOARCH string }{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// goCmd runs the go tool with a controlled environment. Every invocation
// runs with CGO disabled and the module proxy off: the pipeline must
// never depend on a C toolchain or reach out to the network.
type goCmd struct {
	dir string   // working directory, empty means the current one
	env []string // extra environment variables
}

// run executes go with args and returns combined output. Failures are
// wrapped with the command line and the captured output.
func (g goCmd) run(args ...string) (string, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return "", fmt.Errorf("go tool not found in PATH: %w", err)
	}
	cmd := exec.Command(goBin, args...)
	if g.dir != "" {
		cmd.Dir = g.dir
	}
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOPROXY=off")
	cmd.Env = append(cmd.Env, g.env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

// goPackageDirs returns the directories of all Go packages in the module
// rooted at root. Nested modules, such as test fixtures, are not part
// of ./... and therefore stay out.
func goPackageDirs(root string) ([]string, error) {
	out, err := goCmd{dir: root}.run("list", "-f", "{{.Dir}}", "./...")
	if err != nil {
		return nil, fmt.Errorf("list packages: %w", err)
	}
	var dirs []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			dirs = append(dirs, line)
		}
	}
	return dirs, nil
}

// checkGofmt verifies that every Go package in the module is gofmt-clean.
func checkGofmt(root string) error {
	gofmt, err := exec.LookPath("gofmt")
	if err != nil {
		return fmt.Errorf("gofmt not found in PATH: %w", err)
	}
	dirs, err := goPackageDirs(root)
	if err != nil {
		return err
	}
	if len(dirs) == 0 {
		return errors.New("no Go packages found")
	}
	args := append([]string{"-l"}, dirs...)
	cmd := exec.Command(gofmt, args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gofmt -l: %w: %s", err, out)
	}
	if files := strings.TrimSpace(string(out)); files != "" {
		return fmt.Errorf("files not gofmt-clean, run gofmt -w:\n%s", files)
	}
	return nil
}

// checkVet runs go vet over the whole module.
func checkVet(root string) error {
	_, err := goCmd{dir: root}.run("vet", "./...")
	return err
}

// checkTest runs the full test suite.
func checkTest(root string) error {
	_, err := goCmd{dir: root}.run("test", "./...")
	return err
}

// checkCoverage runs the test suite with coverage and enforces the
// coverage floor on the module total.
func checkCoverage(root string) error {
	tmp, err := os.MkdirTemp("", "byline-coverage-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	profile := filepath.Join(tmp, "cover.out")
	if _, err := (goCmd{dir: root}).run("test", "./...", "-coverprofile="+profile, "-covermode=count"); err != nil {
		return err
	}
	out, err := goCmd{dir: root}.run("tool", "cover", "-func", profile)
	if err != nil {
		return err
	}
	total, err := parseCoverageTotal(out)
	if err != nil {
		return fmt.Errorf("parse coverage output: %w", err)
	}
	if total < coverageFloor {
		return fmt.Errorf("total coverage %.1f%% is below the %.1f%% floor", total, coverageFloor)
	}
	return nil
}

// parseCoverageTotal extracts the total percentage from `go tool cover
// -func` output, whose last line reads:
//
//	total: (statements) 87.5%
func parseCoverageTotal(out string) (float64, error) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "total:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		percent := strings.TrimSuffix(fields[len(fields)-1], "%")
		v, err := strconv.ParseFloat(percent, 64)
		if err != nil {
			return 0, fmt.Errorf("parse total percentage: %w", err)
		}
		return v, nil
	}
	return 0, errors.New("no total line found")
}

// checkBuilds builds all release targets to prove the module compiles
// everywhere it ships.
func checkBuilds(root string) error {
	for _, t := range releaseTargets {
		env := []string{"GOOS=" + t.GOOS, "GOARCH=" + t.GOARCH}
		if _, err := (goCmd{dir: root, env: env}).run("build", "./..."); err != nil {
			return fmt.Errorf("release target %s/%s: %w", t.GOOS, t.GOARCH, err)
		}
	}
	return nil
}

// checkDependencies enforces the stdlib-only policy: no package in the
// repository may depend on anything outside the standard library and the
// module itself. When this rule changes, the pull request needs a
// written justification.
func checkDependencies(root string) error {
	module, err := modulePathOf(root)
	if err != nil {
		return err
	}
	out, err := goCmd{dir: root}.run("list", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...")
	if err != nil {
		return err
	}
	var external []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == module || strings.HasPrefix(line, module+"/") {
			continue
		}
		external = append(external, line)
	}
	if len(external) > 0 {
		return fmt.Errorf("non-standard dependencies present, stdlib-only policy:\n%s", strings.Join(external, "\n"))
	}
	return nil
}

// productionPatterns returns the package patterns of the production
// binary: everything under cmd and internal, when those directories
// exist.
func productionPatterns(root string) []string {
	var pats []string
	for _, dir := range []string{"cmd", "internal"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err == nil {
			pats = append(pats, "./"+dir+"/...")
		}
	}
	return pats
}

// checkForbiddenImports rejects direct imports that production packages
// (cmd and internal) must never use. The binary must never dial out, so
// the net package tree is forbidden there; os/exec is only allowed later
// inside internal/gitcmd, which wraps all git access.
func checkForbiddenImports(root string) error {
	module, err := modulePathOf(root)
	if err != nil {
		return err
	}
	pats := productionPatterns(root)
	if len(pats) == 0 {
		return nil
	}
	args := append([]string{"list", "-f", `{{.ImportPath}}: {{join .Imports " "}}`}, pats...)
	out, err := goCmd{dir: root}.run(args...)
	if err != nil {
		return err
	}
	var problems []string
	for _, raw := range strings.Split(out, "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		// The template renders "path: " even when a package has no
		// imports, so cut on the raw line, not on the trimmed one.
		pkg, imports, ok := strings.Cut(raw, ": ")
		if !ok {
			return fmt.Errorf("unparsable go list line %q", raw)
		}
		for _, imp := range strings.Fields(imports) {
			switch {
			case imp == "C":
				problems = append(problems, fmt.Sprintf("%s imports C: CGo is forbidden", pkg))
			case imp == "net" || strings.HasPrefix(imp, "net/"):
				problems = append(problems, fmt.Sprintf("%s imports %s: production code must not dial out", pkg, imp))
			case imp == "os/exec" && !strings.HasPrefix(pkg, module+"/internal/gitcmd"):
				problems = append(problems, fmt.Sprintf("%s imports os/exec: only internal/gitcmd may run subprocesses", pkg))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("forbidden imports:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}
