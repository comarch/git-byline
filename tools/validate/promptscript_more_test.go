package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const promptScriptVersionOutput = "promptscript " + pinnedPromptScriptVersion + "\n"

type promptScriptFailureCase struct {
	name          string
	mode          string
	pathMissing   bool
	writeOutputs  bool
	complete      bool
	checkPortable bool
	want          string
}

type driftFailureCase struct {
	name             string
	command          string
	mode             string
	tempDirFailure   bool
	missingSource    bool
	unreadableSource bool
	want             string
}

func TestCheckPromptScriptFailures(t *testing.T) {
	tests := []promptScriptFailureCase{
		{name: "CLI is missing", pathMissing: true, want: "missing CLI failure"},
		{name: "version command fails", mode: "version-error", want: "version failure"},
		{name: "version output has no semver", mode: "version-empty", want: "missing version failure"},
		{name: "version does not match pin", mode: "version-mismatch", want: "version mismatch"},
		{name: "strict validation fails", mode: "validate-error", want: "strict validation failure"},
		{name: "portable output is missing", mode: "success", want: "portable output read failure"},
		{
			name:          "portable hook is incomplete",
			mode:          "success",
			writeOutputs:  true,
			checkPortable: true,
			want:          "missing hook failure",
		},
		{
			name:         "drift check fails",
			mode:         "generated-missing",
			writeOutputs: true,
			complete:     true,
			want:         "drift failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runPromptScriptFailure(t, tc)
		})
	}
}

func runPromptScriptFailure(t *testing.T, tc promptScriptFailureCase) {
	root := promptScriptFixture(t)
	if tc.pathMissing {
		t.Setenv("PATH", t.TempDir())
	}
	if tc.writeOutputs {
		writePortableOutputs(t, root, tc.complete)
	}
	if tc.mode != "" {
		usePromptScript(t, tc.mode)
	}
	if tc.checkPortable {
		if err := checkPortableHookOutputs(root); err == nil {
			t.Fatalf("checkPortableHookOutputs() = nil error, want %s", tc.want)
		}
		return
	}
	if err := checkPromptScript(root); err == nil {
		t.Fatalf("checkPromptScript() = nil error, want %s", tc.want)
	}
}

func TestCheckDriftFailures(t *testing.T) {
	tests := []driftFailureCase{
		{
			name:           "temporary directory creation",
			command:        "/bin/true",
			tempDirFailure: true,
			want:           "temp directory failure",
		},
		{
			name:          "PromptScript source directory is missing",
			command:       "/bin/true",
			missingSource: true,
			want:          "source directory failure",
		},
		{
			name:             "source file cannot be read",
			command:          "/bin/true",
			unreadableSource: true,
			want:             "source read failure",
		},
		{name: "build profiles fail", command: "promptscript", mode: "build-error", want: "build compile failure"},
		{name: "targets fail", command: "promptscript", mode: "target-error", want: "target compile failure"},
		{name: "compiled output walk fails", command: "promptscript", mode: "walk-error", want: "compiled output walk failure"},
		{name: "untracked output is rejected", command: "promptscript", mode: "extra-output", want: "untracked output failure"},
		{name: "generated output is missing", command: "promptscript", mode: "generated-missing", want: "generated output failure"},
		{name: "committed output is missing", command: "promptscript", mode: "all-outputs", want: "committed output failure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runDriftFailure(t, tc)
		})
	}
}

func runDriftFailure(t *testing.T, tc driftFailureCase) {
	var root string
	if tc.mode == "walk-error" && runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if tc.missingSource {
		root = t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "promptscript.yaml"), []byte("{}"), 0o644); err != nil {
			t.Fatalf("write config: %v", err)
		}
	} else {
		root = promptScriptFixture(t)
	}
	if tc.tempDirFailure {
		t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	}
	if tc.unreadableSource {
		source := filepath.Join(root, ".promptscript", "broken.prs")
		if err := os.Symlink(filepath.Join(root, "missing.prs"), source); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
	}
	if tc.mode != "" {
		usePromptScript(t, tc.mode)
	}
	if err := checkDrift(tc.command, root); err == nil {
		t.Fatalf("checkDrift() = nil error, want %s", tc.want)
	}
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
	useFakeTool(t, "promptscript", script)
	t.Setenv("PROMPTSCRIPT_TEST_MODE", mode)
	t.Setenv("PROMPTSCRIPT_TEST_OUTPUTS", strings.Join(promptScriptOutputs, " "))
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
