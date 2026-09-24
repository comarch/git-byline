// checks.go implements the Go toolchain stages of the validation
// pipeline. Every stage runs the go tool through goCmd, which forces CGO
// off and the module proxy off, so the pipeline itself is pure Go and
// network-free.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/comarch/git-byline/internal/ci"
	"github.com/comarch/git-byline/internal/covermerge"
)

// coverageFloor is the minimum total statement coverage the merged
// profile must reach, compared at full precision. It is a floor for
// honest behavior coverage, not a target to game: the merge includes
// package main statements that only spawned binaries can execute.
// The floor sits below the measured total because more than 200
// statements are defensive-unreachable: file sync and close failures
// on healthy files, TOCTOU rechecks, and invariant guards subsumed by
// earlier validation. Covering them would require injection seams
// across many packages or gaming the gate. The value is calibrated
// against the Go 1.24 toolchain CI uses, which splits more coverage
// blocks than newer toolchains, and leaves room for the few
// statements that differ between platforms.
const coverageFloor = 97.4

// Environment pins and arguments shared by the go, gofmt, and covered
// binary invocations: CGO disabled, the module proxy off, and the
// validate flag that selects stage subsets.
const (
	cgoDisabledEnv = "CGO_ENABLED=0"
	proxyOffEnv    = "GOPROXY=off"
	stagesFlag     = "-stages"
)

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
	cmd.Env = append(os.Environ(), cgoDisabledEnv, proxyOffEnv)
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
	cmd.Env = append(os.Environ(), proxyOffEnv)
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

// checkCoverage runs the test suite with coverage, merges the unit
// profile with coverage recorded from spawned command binaries, and
// enforces the coverage floor on the merged module total.
//
// Unit tests alone cannot cover package main files: main only runs in
// spawned processes. The stage therefore also builds every command
// package with `go build -cover`, exercises each binary through
// success and failure paths while GOCOVERDIR collects counters, and
// merges the converted counters into the unit-test profile. The merged
// profile is written to <root>/coverage.out so CI can upload it.
//
// Foreign modules without command packages (test fixtures) keep the
// unit-only profile.
func checkCoverage(root string) error {
	tmp, err := os.MkdirTemp("", "byline-coverage-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)
	unitProfile := filepath.Join(tmp, "unit.out")
	if _, err := (goCmd{dir: root}).run("test", "./...", "-coverprofile="+unitProfile, "-covermode=count"); err != nil {
		return err
	}
	inputs := []string{unitProfile}
	if hasPackage(root, "./cmd/git-byline") {
		childProfile, err := collectBinaryCoverage(root, tmp, unitProfile)
		if err != nil {
			return err
		}
		inputs = append(inputs, childProfile)
	}
	merged := filepath.Join(root, "coverage.out")
	covered, _, err := covermerge.MergeFiles(inputs, merged)
	if err != nil {
		return fmt.Errorf("merge coverage profiles: %w", err)
	}
	out, err := goCmd{dir: root}.run("tool", "cover", "-func", merged)
	if err != nil {
		return err
	}
	total, err := parseCoverageTotal(out)
	if err != nil {
		return fmt.Errorf("parse coverage output: %w", err)
	}
	// The tool output rounds to one decimal place, which can lift a
	// total of 97.46 to 97.5. The merged profile carries the exact
	// percentage, so the floor compares that value.
	if covered < coverageFloor {
		return fmt.Errorf("total coverage %.2f%% is below the %.1f%% floor (tool reports %.1f%%)", covered, coverageFloor, total)
	}
	return nil
}

// hasPackage reports whether a package path resolves in the module
// rooted at root. Foreign modules without command packages skip the
// spawned-binary coverage collection.
func hasPackage(root, pkg string) bool {
	_, err := goCmd{dir: root}.run("list", pkg)
	return err == nil
}

// collectBinaryCoverage builds every command package with coverage
// instrumentation, exercises each spawned binary through success and
// failure paths while GOCOVERDIR collects counters, and converts the
// counters into a `go tool covdata textfmt` profile.
func collectBinaryCoverage(root, tmp, unitProfile string) (string, error) {
	covDir := filepath.Join(tmp, "covbin")
	if err := os.Mkdir(covDir, 0o755); err != nil {
		return "", fmt.Errorf("create binary coverage dir: %w", err)
	}
	binDir := filepath.Join(tmp, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		return "", fmt.Errorf("create bin dir: %w", err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		return "", fmt.Errorf("create non-module dir: %w", err)
	}
	coverEnv := []string{"GOCOVERDIR=" + covDir}

	// git-byline: successful version and failing usage paths.
	bylineBin := filepath.Join(binDir, "git-byline")
	if err := buildCoveredBinary(root, bylineBin, "./cmd/git-byline"); err != nil {
		return "", err
	}
	if err := runCoveredBinary("", bylineBin, []string{"version"}, coverEnv, 0); err != nil {
		return "", err
	}
	if err := runCoveredBinary("", bylineBin, []string{"unknown-command"}, coverEnv, 2); err != nil {
		return "", err
	}

	// validate: stage-subset success, stage-subset usage error, and
	// the not-inside-a-module failure path.
	validateBin := filepath.Join(binDir, "validate")
	if err := buildCoveredBinary(root, validateBin, "./tools/validate"); err != nil {
		return "", err
	}
	if err := runCoveredBinary(root, validateBin, []string{stagesFlag, "gofmt"}, coverEnv, 0); err != nil {
		return "", err
	}
	if err := runCoveredBinary(root, validateBin, []string{stagesFlag, "no-such-stage"}, coverEnv, 2); err != nil {
		return "", err
	}
	if err := runCoveredBinary(outside, validateBin, []string{stagesFlag, "gofmt"}, coverEnv, 1); err != nil {
		return "", err
	}

	// covermerge: usage error and successful merge child paths.
	covermergeBin := filepath.Join(binDir, "covermerge")
	if err := buildCoveredBinary(root, covermergeBin, "./tools/covermerge"); err != nil {
		return "", err
	}
	if err := runCoveredBinary("", covermergeBin, nil, coverEnv, 2); err != nil {
		return "", err
	}
	mergeCheck := filepath.Join(tmp, "merge-check.out")
	mergeArgs := []string{"-o", mergeCheck, unitProfile, unitProfile}
	if err := runCoveredBinary("", covermergeBin, mergeArgs, coverEnv, 0); err != nil {
		return "", err
	}

	childProfile := filepath.Join(tmp, "child.out")
	if _, err := (goCmd{dir: root}).run("tool", "covdata", "textfmt", "-i="+covDir, "-o", childProfile); err != nil {
		return "", err
	}
	return childProfile, nil
}

// buildCoveredBinary builds pkg with coverage instrumentation so the
// spawned binary records counters for its package main statements.
func buildCoveredBinary(root, bin, pkg string) error {
	if _, err := (goCmd{dir: root}).run("build", "-cover", "-covermode=count", "-o", bin, pkg); err != nil {
		return fmt.Errorf("build covered %s: %w", pkg, err)
	}
	return nil
}

// runCoveredBinary runs an instrumented binary in dir (empty means the
// current directory) and requires the process to exit with wantCode.
// extraEnv carries GOCOVERDIR so spawned binaries record counters.
func runCoveredBinary(dir, bin string, args []string, extraEnv []string, wantCode int) error {
	cmd := exec.Command(bin, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), cgoDisabledEnv, proxyOffEnv)
	cmd.Env = append(cmd.Env, extraEnv...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == wantCode {
		return nil
	}
	if err == nil && wantCode == 0 {
		return nil
	}
	return fmt.Errorf("%s %s: %w\n%s", bin, strings.Join(args, " "), err, out.String())
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
// repository, production or test, may depend on anything outside the
// standard library and the module itself. When this rule changes, the
// pull request needs a written justification.
func checkDependencies(root string) error {
	module, err := modulePathOf(root)
	if err != nil {
		return err
	}
	// -test includes imports made from _test.go files, which plain
	// -deps would miss; the pseudo-packages it adds are filtered out
	// by filterExternal.
	out, err := goCmd{dir: root}.run("list", "-test", "-deps", "-f", "{{if not .Standard}}{{.ImportPath}}{{end}}", "./...")
	if err != nil {
		return err
	}
	if external := filterExternal(out, module); len(external) > 0 {
		return fmt.Errorf("non-standard dependencies present, stdlib-only policy:\n%s", strings.Join(external, "\n"))
	}
	return nil
}

// filterExternal returns every non-standard dependency in `go list -deps`
// output that is neither part of the module nor a test pseudo-package.
// With -test, go list reports three extra kinds of lines: the test
// binary "<pkg>.test" and the test variants "<pkg> [<pkg>.test]" and
// "<pkg>_test [<pkg>.test]"; they are artifacts of listing, not imports.
func filterExternal(out, module string) []string {
	var external []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || ownedByModule(line, module) || isTestArtifact(line, module) {
			continue
		}
		external = append(external, line)
	}
	return external
}

// ownedByModule reports whether path is the module itself or a package
// inside it.
func ownedByModule(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}

// isTestArtifact reports whether line is a `go list -test` pseudo-package
// rather than a real import: the test binary "<pkg>.test" or a test
// variant "<pkg> [<pkg>.test]". Real import paths cannot contain spaces or
// brackets, so a bracketed line is always a variant; only variants of
// the module itself count, an external package genuinely named x.test
// stays a finding.
func isTestArtifact(line, module string) bool {
	if pkg, ok := strings.CutSuffix(line, ".test"); ok && ownedByModule(pkg, module) {
		return true
	}
	if _, variant, ok := strings.Cut(line, " ["); ok {
		if base, ok := strings.CutSuffix(variant, "]"); ok {
			if pkg, ok := strings.CutSuffix(base, ".test"); ok && ownedByModule(pkg, module) {
				return true
			}
		}
	}
	return false
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
// (cmd and internal) must never use. The binary has no in-process network
// stack, so the net package tree is forbidden there; os/exec is only
// allowed inside internal/gitcmd, which wraps all git access, and
// internal/runner, which wraps the update command's curl and self-exec
// subprocesses.
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
			case imp == "os/exec" && pkg != module+"/internal/gitcmd" && pkg != module+"/internal/runner":
				problems = append(problems, fmt.Sprintf("%s imports os/exec: only internal/gitcmd and internal/runner may run subprocesses", pkg))
			}
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("forbidden imports:\n%s", strings.Join(problems, "\n"))
	}
	return nil
}

// checkCITemplates proves that installation writes the canonical workflow
// bytes without adding a provider-specific generation path.
func checkCITemplates(root string) error {
	if root == "" {
		return errors.New("CI template root is empty")
	}
	temp, err := os.MkdirTemp("", "byline-ci-templates-")
	if err != nil {
		return fmt.Errorf("create CI template fixture: %w", err)
	}
	defer os.RemoveAll(temp)
	for _, provider := range []ci.Provider{ci.ProviderGitHub, ci.ProviderGitLab} {
		expected, err := ci.Template(provider)
		if err != nil {
			return err
		}
		sourcePath, err := ciTemplateSourcePath(root, provider)
		if err != nil {
			return err
		}
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read %s CI source template: %w", provider, err)
		}
		if !bytes.Equal(source, expected) {
			return fmt.Errorf("%s CI source template differs from embedded template", provider)
		}
		result, err := ci.Install(temp, provider)
		if err != nil {
			return fmt.Errorf("install %s CI template: %w", provider, err)
		}
		actual, err := os.ReadFile(filepath.Join(temp, filepath.FromSlash(result.Path)))
		if err != nil {
			return fmt.Errorf("read installed %s CI template: %w", provider, err)
		}
		if !bytes.Equal(actual, expected) {
			return fmt.Errorf("installed %s CI template differs from canonical template", provider)
		}
	}
	return nil
}

func ciTemplateSourcePath(root string, provider ci.Provider) (string, error) {
	name := ""
	switch provider {
	case ci.ProviderGitHub:
		name = "github.yml"
	case ci.ProviderGitLab:
		name = "gitlab.yml"
	default:
		return "", fmt.Errorf("unsupported CI provider %q", provider)
	}
	return filepath.Join(root, "internal", "ci", "templates", name), nil
}

// checkCITemplateVersions proves that every CI template installs the
// release recorded in the release-please manifest, so a release never
// ships templates that download an older binary.
func checkCITemplateVersions(root string) error {
	data, err := os.ReadFile(filepath.Join(root, ".release-please-manifest.json"))
	if err != nil {
		return fmt.Errorf("read release manifest: %w", err)
	}
	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse release manifest: %w", err)
	}
	version := manifest["."]
	if version == "" {
		return errors.New("release manifest has no root package version")
	}
	for _, name := range []string{"github.yml", "gitlab.yml"} {
		source, err := os.ReadFile(filepath.Join(root, "internal", "ci", "templates", name))
		if err != nil {
			return fmt.Errorf("read CI template %s: %w", name, err)
		}
		pinned, err := ci.PinnedVersion(source)
		if err != nil {
			return fmt.Errorf("CI template %s: %w", name, err)
		}
		if pinned != "v"+version {
			return fmt.Errorf("CI template %s pins %s, release manifest is v%s", name, pinned, version)
		}
	}
	return nil
}
