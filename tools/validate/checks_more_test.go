package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const validateFixtureModule = "example.com/fixture"

func TestValidateGoBoundaryFailures(t *testing.T) {
	t.Run("go is missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := (goCmd{}).run("version"); err == nil {
			t.Fatal("goCmd.run() = nil error, want missing go failure")
		}
		if _, err := repoRoot(); err == nil {
			t.Fatal("repoRoot() = nil error, want missing go failure")
		}
	})

	t.Run("gofmt is missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := checkGofmt(t.TempDir()); err == nil {
			t.Fatal("checkGofmt() = nil error, want missing gofmt failure")
		}
	})

	t.Run("module path is empty", func(t *testing.T) {
		useFakeGo(t, "empty-module")
		if _, err := modulePathOf(copyFixture(t)); err == nil {
			t.Fatal("modulePathOf() = nil error, want empty module failure")
		}
	})

	t.Run("package listing fails", func(t *testing.T) {
		root := t.TempDir()
		if _, err := goPackageDirs(root); err == nil {
			t.Fatal("goPackageDirs() = nil error, want list failure")
		}
		if err := checkGofmt(root); err == nil {
			t.Fatal("checkGofmt() = nil error, want list failure")
		}
	})

	t.Run("no packages", func(t *testing.T) {
		root := copyFixture(t)
		useFakeGo(t, "empty-packages")
		if err := checkGofmt(root); err == nil {
			t.Fatal("checkGofmt(empty module) = nil error, want no-package failure")
		}
	})

	t.Run("gofmt command fails", func(t *testing.T) {
		root := copyFixture(t)
		useFakeTool(t, "gofmt", "#!/bin/sh\nexit 1\n")
		if err := checkGofmt(root); err == nil {
			t.Fatal("checkGofmt() = nil error, want command failure")
		}
	})
}

func TestValidateCoverageFailures(t *testing.T) {
	t.Run("temporary directory creation", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		if err := checkCoverage(copyFixture(t)); err == nil {
			t.Fatal("checkCoverage() = nil error, want temp directory failure")
		}
	})

	t.Run("unit tests fail", func(t *testing.T) {
		root := copyFixture(t)
		writeFixtureFile(t, root, "broken.go", "package fixture\n\nfunc Broken( {\n")
		if err := checkCoverage(root); err == nil {
			t.Fatal("checkCoverage() = nil error, want test failure")
		}
	})

	t.Run("covered binary build fails", func(t *testing.T) {
		root := copyFixture(t)
		dir := filepath.Join(root, "cmd", "git-byline")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create command directory: %v", err)
		}
		writeFixtureFile(t, root, filepath.Join("cmd", "git-byline", "main.go"),
			"package main\n\nfunc Value() int { return 1 }\n")
		if err := checkCoverage(root); err == nil {
			t.Fatal("checkCoverage() = nil error, want covered binary build failure")
		}
	})

	t.Run("merged profile cannot be written", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.Mkdir(filepath.Join(root, "coverage.out"), 0o755); err != nil {
			t.Fatalf("create output directory: %v", err)
		}
		if err := checkCoverage(root); err == nil {
			t.Fatal("checkCoverage() = nil error, want merge failure")
		}
	})

	t.Run("cover command fails", func(t *testing.T) {
		useFakeGo(t, "cover-error")
		if err := checkCoverage(copyFixture(t)); err == nil {
			t.Fatal("checkCoverage() = nil error, want cover command failure")
		}
	})

	t.Run("coverage output is unparsable", func(t *testing.T) {
		useFakeGo(t, "cover-parse-error")
		if err := checkCoverage(copyFixture(t)); err == nil {
			t.Fatal("checkCoverage() = nil error, want parse failure")
		}
	})

	t.Run("coverage is below floor", func(t *testing.T) {
		useFakeGo(t, "cover-low")
		if err := checkCoverage(copyFixture(t)); err == nil {
			t.Fatal("checkCoverage() = nil error, want floor failure")
		}
	})

	t.Run("binary collection succeeds", func(t *testing.T) {
		root := copyFixture(t)
		useCoverageFakeGo(t, "success")
		if err := checkCoverage(root); err != nil {
			t.Fatalf("checkCoverage() = %v, want nil", err)
		}
	})
}

func TestCollectBinaryCoverageSetupFailures(t *testing.T) {
	t.Run("coverage directory exists", func(t *testing.T) {
		tmp := t.TempDir()
		if err := os.Mkdir(filepath.Join(tmp, "covbin"), 0o755); err != nil {
			t.Fatalf("create coverage directory: %v", err)
		}
		if _, err := collectBinaryCoverage("", tmp, ""); err == nil {
			t.Fatal("collectBinaryCoverage() = nil error, want coverage directory failure")
		}
	})

	t.Run("binary directory exists", func(t *testing.T) {
		tmp := t.TempDir()
		if err := os.Mkdir(filepath.Join(tmp, "bin"), 0o755); err != nil {
			t.Fatalf("create binary directory: %v", err)
		}
		if _, err := collectBinaryCoverage("", tmp, ""); err == nil {
			t.Fatal("collectBinaryCoverage() = nil error, want binary directory failure")
		}
	})

	t.Run("outside directory exists", func(t *testing.T) {
		tmp := t.TempDir()
		if err := os.Mkdir(filepath.Join(tmp, "outside"), 0o755); err != nil {
			t.Fatalf("create outside directory: %v", err)
		}
		if _, err := collectBinaryCoverage("", tmp, ""); err == nil {
			t.Fatal("collectBinaryCoverage() = nil error, want outside directory failure")
		}
	})
}

func TestCollectBinaryCoverageFailures(t *testing.T) {
	modes := []string{
		"fail-byline-version",
		"fail-byline-unknown",
		"fail-validate-build",
		"fail-validate-success",
		"fail-validate-unknown",
		"fail-validate-outside",
		"fail-covermerge-build",
		"fail-covermerge-usage",
		"fail-covermerge-merge",
		"fail-covdata",
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			root := copyFixture(t)
			tmp := t.TempDir()
			useCoverageFakeGo(t, mode)
			if _, err := collectBinaryCoverage(root, tmp, "unit.out"); err == nil {
				t.Fatalf("collectBinaryCoverage(%s) = nil error, want failure", mode)
			}
		})
	}
}

func TestValidateDependencyFailures(t *testing.T) {
	t.Run("module lookup fails", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := checkDependencies(t.TempDir()); err == nil {
			t.Fatal("checkDependencies() = nil error, want module lookup failure")
		}
	})

	t.Run("dependency listing fails", func(t *testing.T) {
		root := copyFixture(t)
		writeFixtureFile(t, root, "go.mod",
			"module "+validateFixtureModule+"\n\ngo 1.24\n\n"+
				"require example.com/missing v0.0.0\n")
		writeFixtureFile(t, root, "missing.go",
			"package fixture\n\nimport _ \"example.com/missing\"\n")
		if err := checkDependencies(root); err == nil {
			t.Fatal("checkDependencies() = nil error, want list failure")
		}
	})
}

func TestValidateForbiddenImportFailures(t *testing.T) {
	t.Run("module lookup fails", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if err := checkForbiddenImports(t.TempDir()); err == nil {
			t.Fatal("checkForbiddenImports() = nil error, want module lookup failure")
		}
	})

	t.Run("package listing fails", func(t *testing.T) {
		root := fixtureWithCommand(t)
		useFakeGo(t, "imports-error")
		if err := checkForbiddenImports(root); err == nil {
			t.Fatal("checkForbiddenImports() = nil error, want list failure")
		}
	})

	t.Run("unparsable package listing", func(t *testing.T) {
		root := fixtureWithCommand(t)
		useFakeGo(t, "imports-unparsable")
		if err := checkForbiddenImports(root); err == nil {
			t.Fatal("checkForbiddenImports() = nil error, want parse failure")
		}
	})

	t.Run("C and os exec imports", func(t *testing.T) {
		root := fixtureWithCommand(t)
		useFakeGo(t, "imports-forbidden")
		if err := checkForbiddenImports(root); err == nil {
			t.Fatal("checkForbiddenImports() = nil error, want forbidden import failure")
		}
	})

	t.Run("clean production package", func(t *testing.T) {
		root := fixtureWithCommand(t)
		if err := checkForbiddenImports(root); err != nil {
			t.Fatalf("checkForbiddenImports() = %v, want nil", err)
		}
	})
}

func TestValidateCITemplateFailures(t *testing.T) {
	t.Run("empty root", func(t *testing.T) {
		if err := checkCITemplates(""); err == nil {
			t.Fatal("checkCITemplates(empty root) = nil, want failure")
		}
	})

	t.Run("temporary directory creation", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		if err := checkCITemplates("root"); err == nil {
			t.Fatal("checkCITemplates() = nil error, want temp directory failure")
		}
	})

	t.Run("source template is missing", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "internal", "ci", "templates"), 0o755); err != nil {
			t.Fatalf("create template directory: %v", err)
		}
		if err := checkCITemplates(root); err == nil {
			t.Fatal("checkCITemplates() = nil error, want missing source failure")
		}
	})

	t.Run("unknown provider path", func(t *testing.T) {
		if _, err := ciTemplateSourcePath(t.TempDir(), "unknown"); err == nil {
			t.Fatal("ciTemplateSourcePath() = nil error, want provider failure")
		}
	})
}

func fixtureWithCommand(t *testing.T) string {
	t.Helper()
	root := copyFixture(t)
	dir := filepath.Join(root, "cmd")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create cmd: %v", err)
	}
	writeFixtureFile(t, root, filepath.Join("cmd", "main.go"), "package main\n\nfunc main() {}\n")
	return root
}

func useFakeGo(t *testing.T, mode string) {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("look up go: %v", err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"-m\" ] && [ \"$VALIDATE_FAKE_GO_MODE\" = \"empty-module\" ]; then exit 0; fi\n" +
		"if [ \"$1\" = \"list\" ] && [ \"$2\" = \"-f\" ]; then\n" +
		"  case \"$VALIDATE_FAKE_GO_MODE\" in\n" +
		"    empty-packages) exit 0;;\n" +
		"    imports-error) printf '%s\\n' 'list failed' >&2; exit 1;;\n" +
		"    imports-unparsable) printf '%s\\n' 'malformed listing'; exit 0;;\n" +
		"    imports-forbidden) printf '%s\\n' 'example.com/fixture/cmd: C os/exec'; exit 0;;\n" +
		"  esac\n" +
		"fi\n" +
		"if [ \"$1\" = \"tool\" ] && [ \"$2\" = \"cover\" ] && [ \"$3\" = \"-func\" ]; then\n" +
		"  case \"$VALIDATE_FAKE_GO_MODE\" in\n" +
		"    cover-error) printf '%s\\n' 'cover failed' >&2; exit 1;;\n" +
		"    cover-parse-error) printf '%s\\n' 'total: (statements) not-a-number%'; exit 0;;\n" +
		"    cover-low) printf '%s\\n' 'total: (statements) 95.0%'; exit 0;;\n" +
		"  esac\n" +
		"fi\n" +
		"exec " + shellQuote(realGo) + " \"$@\"\n"
	writeExecutableTestFile(t, filepath.Join(dir, "go"), script)
	t.Setenv("VALIDATE_FAKE_GO_MODE", mode)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func useFakeTool(t *testing.T, name, content string) {
	t.Helper()
	dir := t.TempDir()
	writeExecutableTestFile(t, filepath.Join(dir, name), content)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeExecutableTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func useCoverageFakeGo(t *testing.T, mode string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"mode=\"$VALIDATE_COVERAGE_FAKE_MODE\"\n" +
		"if [ \"$1\" = test ]; then\n" +
		"  for arg in \"$@\"; do case \"$arg\" in -coverprofile=*) profile=\"${arg#*=}\";; esac; done\n" +
		"  printf '%s\\n' 'mode: count' 'fixture.go:1.1,1.2 1 1' > \"$profile\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = list ]; then exit 0; fi\n" +
		"if [ \"$1\" = build ]; then\n" +
		"  pkg=\"$6\"\n" +
		"  case \"$mode:$pkg\" in\n" +
		"    fail-validate-build:./tools/validate|fail-covermerge-build:./tools/covermerge) exit 1;;\n" +
		"  esac\n" +
		"  kind=other\n" +
		"  case \"$pkg\" in\n" +
		"    ./cmd/git-byline) kind=byline;;\n" +
		"    ./tools/validate) kind=validate;;\n" +
		"    ./tools/covermerge) kind=covermerge;;\n" +
		"  esac\n" +
		"  out=\"$5\"\n" +
		"  printf '%s\\n' '#!/bin/sh' \"kind=$kind\" \"buildroot=$PWD\" > \"$out\"\n" +
		"  cat >> \"$out\" <<'EOF'\n" +
		"mode=\"$VALIDATE_COVERAGE_FAKE_MODE\"\n" +
		"if [ \"$kind\" = byline ] && [ \"$1\" = version ] && [ \"$mode\" = fail-byline-version ]; then exit 1; fi\n" +
		"if [ \"$kind\" = byline ] && [ \"$1\" = unknown-command ] && [ \"$mode\" = fail-byline-unknown ]; then exit 1; fi\n" +
		"if [ \"$kind\" = validate ] && [ \"$1\" = -stages ] && [ \"$2\" = gofmt ] && [ \"$mode\" = fail-validate-success ]; then exit 1; fi\n" +
		"if [ \"$kind\" = validate ] && [ \"$1\" = -stages ] && [ \"$2\" = no-such-stage ] && [ \"$mode\" = fail-validate-unknown ]; then exit 1; fi\n" +
		"if [ \"$kind\" = covermerge ] && [ \"$#\" = 0 ] && [ \"$mode\" = fail-covermerge-usage ]; then exit 1; fi\n" +
		"if [ \"$kind\" = covermerge ] && [ \"$#\" -gt 0 ] && [ \"$mode\" = fail-covermerge-merge ]; then exit 1; fi\n" +
		"if [ \"$kind\" = byline ] && [ \"$1\" = unknown-command ]; then exit 2; fi\n" +
		"if [ \"$kind\" = validate ] && [ \"$1\" = -stages ] && [ \"$2\" = no-such-stage ]; then exit 2; fi\n" +
		"current=$(pwd)\n" +
		"if [ \"$kind\" = validate ] && [ \"$1\" = -stages ] && [ \"$2\" = gofmt ] && [ \"$mode\" = fail-validate-outside ]; then exit 0; fi\n" +
		"if [ \"$kind\" = validate ] && [ \"$current\" != \"$buildroot\" ]; then exit 1; fi\n" +
		"if [ \"$kind\" = validate ] && [ \"$1\" = -stages ] && [ \"$2\" = gofmt ]; then exit 0; fi\n" +
		"if [ \"$kind\" = covermerge ] && [ \"$#\" = 0 ]; then exit 2; fi\n" +
		"exit 0\n" +
		"EOF\n" +
		"  chmod 755 \"$out\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = tool ] && [ \"$2\" = covdata ]; then\n" +
		"  if [ \"$mode\" = fail-covdata ]; then exit 1; fi\n" +
		"  for arg in \"$@\"; do case \"$arg\" in -o) next=1;; *) if [ \"$next\" = 1 ]; then profile=\"$arg\"; next=0; fi;; esac; done\n" +
		"  printf '%s\\n' 'mode: count' 'tools/validate/main.go:1.1,1.2 1 1' > \"$profile\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = tool ] && [ \"$2\" = cover ]; then printf '%s\\n' 'total: (statements) 100.0%'; exit 0; fi\n" +
		"exit 1\n"
	writeExecutableTestFile(t, filepath.Join(dir, "go"), script)
	t.Setenv("VALIDATE_COVERAGE_FAKE_MODE", mode)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
