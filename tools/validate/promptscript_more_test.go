package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const promptScriptVersionOutput = "promptscript " + pinnedPromptScriptVersion + "\n"

func TestCheckPromptScriptFailures(t *testing.T) {
	t.Run("CLI is missing", func(t *testing.T) {
		root := promptScriptFixture(t)
		t.Setenv("PATH", t.TempDir())
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want missing CLI failure")
		}
	})

	t.Run("version command fails", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "version-error")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want version failure")
		}
	})

	t.Run("version output has no semver", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "version-empty")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want missing version failure")
		}
	})

	t.Run("version does not match pin", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "version-mismatch")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want version mismatch")
		}
	})

	t.Run("strict validation fails", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "validate-error")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want strict validation failure")
		}
	})

	t.Run("portable output is missing", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "success")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want portable output read failure")
		}
	})

	t.Run("portable hook is incomplete", func(t *testing.T) {
		root := promptScriptFixture(t)
		writePortableOutputs(t, root, false)
		usePromptScript(t, "success")
		if err := checkPortableHookOutputs(root); err == nil {
			t.Fatal("checkPortableHookOutputs() = nil error, want missing hook failure")
		}
	})

	t.Run("drift check fails", func(t *testing.T) {
		root := promptScriptFixture(t)
		writePortableOutputs(t, root, true)
		usePromptScript(t, "generated-missing")
		if err := checkPromptScript(root); err == nil {
			t.Fatal("checkPromptScript() = nil error, want drift failure")
		}
	})
}

func TestCheckDriftFailures(t *testing.T) {
	t.Run("temporary directory creation", func(t *testing.T) {
		root := promptScriptFixture(t)
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
		if err := checkDrift("/bin/true", root); err == nil {
			t.Fatal("checkDrift() = nil error, want temp directory failure")
		}
	})

	t.Run("PromptScript source directory is missing", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "promptscript.yaml"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
		if err := checkDrift("/bin/true", root); err == nil {
			t.Fatal("checkDrift() = nil error, want source directory failure")
		}
	})

	t.Run("source file cannot be read", func(t *testing.T) {
		root := promptScriptFixture(t)
		source := filepath.Join(root, ".promptscript", "broken.prs")
		if err := os.Symlink(filepath.Join(root, "missing.prs"), source); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := checkDrift("/bin/true", root); err == nil {
			t.Fatal("checkDrift() = nil error, want source read failure")
		}
	})

	t.Run("build profiles fail", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "build-error")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want build compile failure")
		}
	})

	t.Run("targets fail", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "target-error")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want target compile failure")
		}
	})

	t.Run("compiled output walk fails", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "walk-error")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want compiled output walk failure")
		}
	})

	t.Run("untracked output is rejected", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "extra-output")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want untracked output failure")
		}
	})

	t.Run("generated output is missing", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "generated-missing")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want generated output failure")
		}
	})

	t.Run("committed output is missing", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "all-outputs")
		if err := checkDrift("promptscript", root); err == nil {
			t.Fatal("checkDrift() = nil error, want committed output failure")
		}
	})
}

func TestPromptScriptHelpers(t *testing.T) {
	t.Run("compiled output walk fails for missing root", func(t *testing.T) {
		if _, err := compiledOutputPaths(filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("compiledOutputPaths() = nil error, want walk failure")
		}
	})

	t.Run("capture wraps command output", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("POSIX shell helper is not executable on Windows")
		}
		out, err := runCapture("/bin/sh", t.TempDir(), "-c", "printf failure; exit 1")
		if err == nil {
			t.Fatal("runCapture() = nil error, want command failure")
		}
		if out != "failure" || !strings.Contains(err.Error(), "failure") {
			t.Fatalf("runCapture() = %q, %v, want captured output", out, err)
		}
	})
}

func promptScriptFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".promptscript"), 0o755); err != nil {
		t.Fatalf("create PromptScript directory: %v", err)
	}
	writePromptScriptFile(t, root, filepath.Join(".promptscript", "project.prs"), "source\n")
	writePromptScriptFile(t, root, "promptscript.yaml", "config: true\n")
	return root
}

func writePortableOutputs(t *testing.T, root string, complete bool) {
	t.Helper()
	outputs := map[string]string{
		filepath.Join(".factory", "hooks.json"):                       "factory",
		filepath.Join(".claude", "settings.json"):                     "claude",
		filepath.Join(".github", "hooks", "promptscript.json"):        "copilot",
		filepath.Join(".github", "hooks", "promptscript-vscode.json"): "vscode",
		filepath.Join(".cursor", "hooks.json"):                        "cursor",
		filepath.Join(".codex", "hooks.json"):                         "codex",
		filepath.Join(".gemini", "settings.json"):                     "gemini",
		filepath.Join(".windsurf", "hooks.json"):                      "windsurf",
		filepath.Join(".grok", "hooks", "promptscript.json"):          "grok",
	}
	for rel, agent := range outputs {
		content := "checkpoint portable-" + agent + " --type human\n"
		if complete {
			content += "checkpoint portable-" + agent + " --type ai\n"
		}
		writePromptScriptFile(t, root, rel, content)
	}
}

func usePromptScript(t *testing.T, mode string) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\n" +
		"mode=\"$PROMPTSCRIPT_TEST_MODE\"\n" +
		"if [ \"$1\" = \"--version\" ]; then\n" +
		"  case \"$mode\" in\n" +
		"    version-error) printf '%s\\n' 'version failed' >&2; exit 1;;\n" +
		"    version-empty) exit 0;;\n" +
		"    version-mismatch) printf '%s\\n' 'promptscript 9.9.9'; exit 0;;\n" +
		"    *) printf '%s' '" + promptScriptVersionOutput[:len(promptScriptVersionOutput)-1] + "'; exit 0;;\n" +
		"  esac\n" +
		"fi\n" +
		"if [ \"$1\" = \"validate\" ]; then\n" +
		"  if [ \"$mode\" = \"validate-error\" ]; then printf '%s\\n' 'strict failed' >&2; exit 1; fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"if [ \"$1\" = \"compile\" ]; then\n" +
		"  if [ \"$2\" = \"--all-builds\" ] && [ \"$mode\" = \"build-error\" ]; then exit 1; fi\n" +
		"  if [ \"$2\" = \"--all\" ] && [ \"$mode\" = \"target-error\" ]; then exit 1; fi\n" +
		"  if [ \"$mode\" = \"walk-error\" ]; then mkdir blocked; chmod 000 blocked; exit 0; fi\n" +
		"  if [ \"$mode\" = \"extra-output\" ]; then printf '%s' extra > extra.txt; exit 0; fi\n" +
		"  if [ \"$mode\" = \"all-outputs\" ]; then\n" +
		"    for rel in $PROMPTSCRIPT_TEST_OUTPUTS; do mkdir -p \"$(dirname \"$rel\")\"; printf '%s' generated > \"$rel\"; done\n" +
		"  fi\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	writeExecutableTestFile(t, filepath.Join(dir, "promptscript"), script)
	t.Setenv("PROMPTSCRIPT_TEST_MODE", mode)
	t.Setenv("PROMPTSCRIPT_TEST_OUTPUTS", strings.Join(promptScriptOutputs, " "))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writePromptScriptFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create PromptScript parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write PromptScript fixture %s: %v", rel, err)
	}
}
