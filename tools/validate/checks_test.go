package main

import (
	"io/fs"
	"os"
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

func TestCheckDependencies(t *testing.T) {
	t.Parallel()
	dir := copyFixture(t)
	if err := checkDependencies(dir); err != nil {
		t.Fatalf("checkDependencies(clean fixture) = %v, want nil", err)
	}
	writeFixtureFile(t, dir, "external.go", "package fixture\n\nimport \"golang.org/x/crypto/sha3\"\n\nvar _ = sha3.NewLegacySHAKE128\n")
	if err := checkDependencies(dir); err == nil {
		t.Error("checkDependencies(external import) = nil, want error")
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
