package main

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// copyFixture copies the pristine fixture module to a temporary directory
// and returns the destination path. Tests mutate the copy, never the
// committed fixture.
func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "fixture")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return dst
}

// writeFixtureFile writes a file into a fixture copy.
func writeFixtureFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestCheckGofmt(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	command := exec.Command("gofmt", "-w", ".")
	command.Dir = dir
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("format fixture: %v\n%s", err, out)
	}
	if err := checkGofmt(dir); err != nil {
		t.Fatalf("checkGofmt(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "dirty.go", "package fixture\n\nfunc  Dirty() { }\n")
	if err := checkGofmt(dir); err == nil {
		t.Error("checkGofmt(dirty fixture) = nil, want error")
	}
}

func TestCheckVet(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkVet(dir); err != nil {
		t.Fatalf("checkVet(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "vetflag.go",
		"package fixture\n\nimport \"fmt\"\n\nfunc Flag() string {\n\treturn fmt.Sprintf(\"%d\")\n}\n")
	if err := checkVet(dir); err == nil {
		t.Error("checkVet(vet-flagged fixture) = nil, want error")
	}
}

func TestCheckTest(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkTest(dir); err != nil {
		t.Fatalf("checkTest(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "fail_test.go",
		"package fixture\n\nimport \"testing\"\n\nfunc TestFail(t *testing.T) {\n\tt.Fatal(\"always fails\")\n}\n")
	if err := checkTest(dir); err == nil {
		t.Error("checkTest(failing fixture) = nil, want error")
	}
}

func TestCheckCoverage(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkCoverage(dir); err != nil {
		t.Fatalf("checkCoverage(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "uncovered.go", "package fixture\n\nfunc Uncovered() int {\n\treturn 42\n}\n")
	if err := checkCoverage(dir); err == nil {
		t.Error("checkCoverage(uncovered fixture) = nil, want error")
	}
}

// TestCheckCoverageWritesMergedProfile pins the merge contract: the
// stage must leave the merged profile at <root>/coverage.out so CI can
// upload exactly what the floor measured.
func TestCheckCoverageWritesMergedProfile(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkCoverage(dir); err != nil {
		t.Fatalf("checkCoverage(clean fixture) = %v, want nil", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "coverage.out"))
	if err != nil {
		t.Fatalf("read merged profile: %v", err)
	}
	if !strings.HasPrefix(string(data), "mode: ") {
		t.Errorf("merged profile starts with %q, want a mode header", string(data[:min(len(data), 16)]))
	}
}

// TestHasPackage pins the skip rule for foreign modules: command
// packages resolve in the real repository, never in the fixture.
func TestHasPackage(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if !hasPackage(root, "./cmd/git-byline") {
		t.Error("hasPackage(real repo, ./cmd/git-byline) = false, want true")
	}
	if hasPackage(copyFixture(t), "./cmd/git-byline") {
		t.Error("hasPackage(fixture, ./cmd/git-byline) = true, want false")
	}
}

// TestRunCoveredBinary pins the exit-code contract of spawned covered
// binaries: matching codes pass, every other outcome fails the stage.
func TestRunCoveredBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX exit-code helpers unavailable on windows")
	}
	dir := t.TempDir()
	falseBin := filepath.Join(dir, "false")
	if err := os.WriteFile(falseBin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write false helper: %v", err)
	}
	trueBin := filepath.Join(dir, "true")
	if err := os.WriteFile(trueBin, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write true helper: %v", err)
	}
	cases := []struct {
		name     string
		bin      string
		wantCode int
		wantErr  bool
	}{
		{name: "matching failure code passes", bin: falseBin, wantCode: 1},
		{name: "matching success code passes", bin: trueBin, wantCode: 0},
		{name: "unexpected failure code fails", bin: falseBin, wantCode: 2, wantErr: true},
		{name: "success where failure expected fails", bin: trueBin, wantCode: 1, wantErr: true},
		{name: "missing binary fails", bin: filepath.Join(dir, "gone"), wantCode: 0, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := retryCoveredBinary("", tc.bin, nil, nil, tc.wantCode)
			if tc.wantErr && err == nil {
				t.Fatalf("runCoveredBinary(%s) = nil error, want failure", tc.bin)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("runCoveredBinary(%s) = %v, want nil", tc.bin, err)
			}
		})
	}
}

// etxtbsy is ETXTBSY as a raw errno. syscall.ETXTBSY does not exist on
// windows, where TestRunCoveredBinary skips, and 26 matches Linux and
// macOS.
const etxtbsy = syscall.Errno(26)

// retryCoveredBinary reruns runCoveredBinary when the process never
// started because the binary was still being written back (ETXTBSY,
// observed on loaded CI runners). A failed start keeps exit codes
// meaningful, so only that errno is retried: missing binaries and
// exit-code mismatches stay single-shot.
func retryCoveredBinary(dir, bin string, args []string, extraEnv []string, wantCode int) error {
	for attempt := 0; ; attempt++ {
		err := runCoveredBinary(dir, bin, args, extraEnv, wantCode)
		if err == nil || attempt >= 2 || !isTextFileBusy(err) {
			return err
		}
		time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
	}
}

func isTextFileBusy(err error) bool {
	var execErr *exec.Error
	return errors.As(err, &execErr) && errors.Is(execErr.Err, etxtbsy)
}

// TestBuildCoveredBinary pins the build contract: a covered binary
// lands at the requested path, and a missing package fails the build.
func TestBuildCoveredBinary(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "git-byline")
	if err := buildCoveredBinary(root, bin, "./cmd/git-byline"); err != nil {
		t.Fatalf("buildCoveredBinary(cmd/git-byline) = %v, want nil", err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatalf("covered binary missing: %v", err)
	}
	if err := buildCoveredBinary(root, filepath.Join(t.TempDir(), "x"), "./no-such-package"); err == nil {
		t.Error("buildCoveredBinary(missing package) = nil error, want failure")
	}
}

// TestCollectBinaryCoverage runs the full spawned-binary collection
// against the real repository and verifies the converted child profile
// records package main statements that unit tests cannot reach.
func TestCollectBinaryCoverage(t *testing.T) {
	if testing.Short() {
		t.Skip("covered binary builds are slow")
	}
	if runtime.GOOS == "windows" {
		t.Skip("covered binary collection uses extensionless executable paths")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	tmp := t.TempDir()
	unitProfile := filepath.Join(tmp, "unit.out")
	if err := os.WriteFile(unitProfile, []byte("mode: count\nmain.go:1.2,2.10 1 0\n"), 0o600); err != nil {
		t.Fatalf("write unit profile: %v", err)
	}
	childProfile, err := collectBinaryCoverage(root, tmp, unitProfile)
	if err != nil {
		t.Fatalf("collectBinaryCoverage: %v", err)
	}
	data, err := os.ReadFile(childProfile)
	if err != nil {
		t.Fatalf("read child profile: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "cmd/git-byline/main.go") {
		t.Error("child profile has no cmd/git-byline/main.go blocks")
	}
	if !strings.Contains(text, "tools/validate/main.go") {
		t.Error("child profile has no tools/validate/main.go blocks")
	}
	if !strings.Contains(text, "tools/covermerge/main.go") {
		t.Error("child profile has no tools/covermerge/main.go blocks")
	}
	total, err := parseCoverageTotal(coverFuncOf(t, root, childProfile))
	if err != nil {
		t.Fatalf("parse child coverage total: %v", err)
	}
	if total <= 0 {
		t.Error("child profile has no covered statements")
	}
}

// coverFuncOf runs go tool cover -func over a profile for assertions.
func coverFuncOf(t *testing.T, root, profile string) string {
	t.Helper()
	out, err := goCmd{dir: root}.run("tool", "cover", "-func", profile)
	if err != nil {
		t.Fatalf("go tool cover -func: %v", err)
	}
	return out
}

func TestCheckBuilds(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkBuilds(dir); err != nil {
		t.Fatalf("checkBuilds(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "broken.go", "package fixture\n\nfunc Broken() {\n\tundefinedFunc()\n}\n")
	if err := checkBuilds(dir); err == nil {
		t.Error("checkBuilds(broken fixture) = nil, want error")
	}
}

// TestCheckDependenciesClean pins the clean case end to end: with -test,
// go list reports test pseudo-packages for the fixture's own test
// binary, and the stage must not mistake them for external dependencies.
func TestCheckDependenciesClean(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkDependencies(dir); err != nil {
		t.Fatalf("checkDependencies(clean fixture) = %v, want nil", err)
	}
}

// TestCheckDependenciesFlagsExternalImport proves the reporting path: a
// fixture that imports a module outside the standard library and itself
// fails the stage with the external import path named in the error. The
// external module is wired in with a replace directive to a local
// directory so the import resolves offline.
func TestCheckDependenciesFlagsExternalImport(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)

	extDir := filepath.Join(dir, "external")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		t.Fatalf("create external module dir: %v", err)
	}
	writeFixtureFile(t, extDir, "go.mod", "module example.com/external\n\ngo 1.24\n")
	writeFixtureFile(t, extDir, "lib.go", "package external\n\nfunc Ping() {}\n")
	writeFixtureFile(t, dir, "go.mod",
		"module example.com/fixture\n\ngo 1.24\n\n"+
			"require example.com/external v0.0.0\n\n"+
			"replace example.com/external => ./external\n")
	writeFixtureFile(t, dir, "external.go",
		"package fixture\n\nimport \"example.com/external\"\n\nvar _ = external.Ping\n")

	err := checkDependencies(dir)
	if err == nil {
		t.Fatal("checkDependencies(external import) = nil, want error")
	}
	if !strings.Contains(err.Error(), "example.com/external") {
		t.Errorf("error = %v, want it to name the external dependency", err)
	}
}

func TestFilterExternal(t *testing.T) {
	t.Parallel()
	const module = "example.com/fixture"
	input := strings.Join([]string{
		module,
		module + "/inner",
		module + " [" + module + ".test]",
		module + "_test [" + module + ".test]",
		module + ".test",
		module + "/inner.test",
		"golang.org/x/crypto/sha3",
		"evil.com/x.test",
		"",
	}, "\n")
	want := []string{"golang.org/x/crypto/sha3", "evil.com/x.test"}
	got := filterExternal(input, module)
	if len(got) != len(want) {
		t.Fatalf("filterExternal = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("filterExternal[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestOwnedByModule(t *testing.T) {
	t.Parallel()
	const module = "example.com/fixture"
	tests := []struct {
		path string
		want bool
	}{
		{module, true},
		{module + "/inner", true},
		{"example.com/fixture-similar", false},
		{"evil.com/fixture", false},
	}
	for _, tt := range tests {
		if got := ownedByModule(tt.path, module); got != tt.want {
			t.Errorf("ownedByModule(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestCheckForbiddenImports(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkForbiddenImports(dir); err != nil {
		t.Fatalf("checkForbiddenImports(fixture without production dirs) = %v, want nil", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatalf("create cmd: %v", err)
	}
	writeFixtureFile(t, dir, filepath.Join("cmd", "main.go"),
		"package main\n\nimport \"net/http\"\n\nfunc main() {\n\t_ = http.StatusOK\n}\n")
	err := checkForbiddenImports(dir)
	if err == nil {
		t.Fatal("checkForbiddenImports(net/http in production) = nil, want error")
	}
	if !strings.Contains(err.Error(), "net/http") {
		t.Errorf("error = %v, want it to name net/http", err)
	}
}

func TestCheckCITemplates(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := checkCITemplates(root); err != nil {
		t.Fatalf("checkCITemplates() = %v, want nil", err)
	}
}

func TestCheckCITemplatesDetectsSourceDrift(t *testing.T) {
	t.Parallel()
	repositoryRoot, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	templateDir := filepath.Join(root, "internal", "ci", "templates")
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"github", "gitlab"} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot, "internal", "ci", "templates", provider+".yml"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(templateDir, provider+".yml"), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := checkCITemplates(root); err != nil {
		t.Fatalf("checkCITemplates(clean) = %v", err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "github.yml"), []byte("drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkCITemplates(root); err == nil {
		t.Fatal("checkCITemplates(drifted) = nil")
	}
}

func TestParseCoverageTotal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in       string
		want     float64
		wantFail bool
	}{
		{"mode: count\ngithub.com/x/a.go:1.1,2.2 1 0\n" +
			"total:\t(statements)\t87.5%\n", 87.5, false},
		{"total: (statements) 100.0%\n", 100.0, false},
		{"total: (statements) 0.0%\n", 0.0, false},
		{"no total line here\n", 0, true},
		{"total:\t(statements)\tgarbage%\n", 0, true},
	}
	for _, tt := range tests {
		got, err := parseCoverageTotal(tt.in)
		if tt.wantFail {
			if err == nil {
				t.Errorf("parseCoverageTotal(%q) = nil error, want failure", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseCoverageTotal(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("parseCoverageTotal(%q) = %s, want %s", tt.in,
				strconv.FormatFloat(got, 'f', -1, 64), strconv.FormatFloat(tt.want, 'f', -1, 64))
		}
	}
}
