package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
