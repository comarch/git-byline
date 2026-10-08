package main

import (
	"bytes"
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
			name:          "OpenCode lifecycle hooks are missing",
			mode:          "success",
			writeOutputs:  true,
			complete:      true,
			checkPortable: true,
			want:          "missing OpenCode event failure",
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

func TestCheckPortableHookOutputsAcceptsCompleteOutputs(t *testing.T) {
	root := promptScriptFixture(t)
	writePortableOutputs(t, root, true)
	plugin, err := os.ReadFile(filepath.Join(root, openCodePluginRel))
	if err != nil {
		t.Fatalf("read OpenCode fixture: %v", err)
	}
	events := `"tool.execute.before"` + "\n" + `"tool.execute.after"` + "\n"
	writePromptScriptFile(t, root, openCodePluginRel, string(plugin)+events)
	if err := checkPortableHookOutputs(root); err != nil {
		t.Fatalf("checkPortableHookOutputs() = %v, want nil", err)
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

func TestPatchOpenCodeArtifacts(t *testing.T) {
	root := t.TempDir()
	pluginPath := filepath.Join(root, openCodePluginRel)
	templatePath := filepath.Join(root, openCodeTemplateRel)
	writePromptScriptFile(t, root, openCodePluginRel, testOpenCodePlugin)
	writePromptScriptFile(t, root, openCodeTemplateRel, "stale\n")
	writeOpenCodeAgents(t, root, testOpenCodeAgent)

	if err := patchOpenCodeArtifacts(root); err != nil {
		t.Fatal(err)
	}
	requireOpenCodeAgentsReadOnly(t, root)
	patched, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	template, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(patched, template) {
		t.Fatal("OpenCode harness template differs from generated plugin")
	}
	for _, want := range []string{
		openCodePatchMarker,
		"safePathArguments(payload.args)",
		"MAX_PATH_ARGUMENT_BYTES = 2048",
		"PATCH_PATH_PATTERN",
		"args: {}",
		"running.push(runRule(",
		"await Promise.all(running);",
		"export const PromptScriptHooks = (context: OpenCodePluginContext) => {",
		"return Promise.resolve({",
	} {
		if !bytes.Contains(patched, []byte(want)) {
			t.Errorf("patched OpenCode plugin is missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"void runRule(",
		"await runRule(",
		"PromptScriptHooks = async",
		"args: '[truncated]' }",
	} {
		if bytes.Contains(patched, []byte(unwanted)) {
			t.Errorf("patched OpenCode plugin still contains %q", unwanted)
		}
	}
	if err := patchOpenCodeArtifacts(root); err != nil {
		t.Fatal(err)
	}
	patchedAgain, err := os.ReadFile(pluginPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(patched, patchedAgain) {
		t.Fatal("patching OpenCode plugin twice changed its output")
	}
	requireOpenCodeAgentsReadOnly(t, root)
}

func TestPatchOpenCodeAgent(t *testing.T) {
	t.Parallel()
	patched, err := patchOpenCodeAgent([]byte(testOpenCodeAgent))
	if err != nil {
		t.Fatal(err)
	}
	if string(patched) != testPatchedOpenCodeAgent {
		t.Fatalf("patched agent = %q, want %q", patched, testPatchedOpenCodeAgent)
	}
	patchedAgain, err := patchOpenCodeAgent(patched)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(patched, patchedAgain) {
		t.Fatal("patching an OpenCode agent twice changed its output")
	}

	rejected := map[string]string{
		"no frontmatter end":  "changed PromptScript output\n",
		"permission by hand":  "---\ndescription: x\nmode: subagent\npermission:\n  edit: allow\n---\n",
		"two frontmatter end": testOpenCodeAgent + testOpenCodeAgent,
	}
	for name, agent := range rejected {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := patchOpenCodeAgent([]byte(agent)); err == nil {
				t.Fatal("patchOpenCodeAgent() accepted an unexpected agent")
			}
		})
	}
}

// TestOpenCodeAgentPermissionOrder pins the order OpenCode needs. It applies
// the last matching rule, so the allowlist must start with the catch-all deny
// and the .env prompts must follow the read allow. Verified against OpenCode
// 1.18.33: without the .env rules a reviewer reads .env files with no prompt.
func TestOpenCodeAgentPermissionOrder(t *testing.T) {
	t.Parallel()
	block := openCodeAgentReadOnly
	positions := make(map[string]int)
	for _, rule := range []string{
		`"*": deny`,
		"read:",
		`"*": allow`,
		`"*.env": ask`,
		`"*.env.*": ask`,
		`"*.env.example": allow`,
		"grep: allow",
		"glob: allow",
	} {
		at := strings.Index(block, rule)
		if at < 0 {
			t.Fatalf("permission block lacks %q", rule)
		}
		positions[rule] = at
	}
	ordered := []string{`"*": deny`, "read:", `"*": allow`, `"*.env": ask`, `"*.env.*": ask`, `"*.env.example": allow`}
	for i := 1; i < len(ordered); i++ {
		if positions[ordered[i-1]] >= positions[ordered[i]] {
			t.Errorf("rule %s must come before %s", ordered[i-1], ordered[i])
		}
	}
	for _, tool := range []string{"edit", "write", "bash", "apply_patch", "task", "webfetch"} {
		if strings.Contains(block, tool) {
			t.Errorf("permission block mentions %q, but only the read tools are allowed", tool)
		}
	}
}

func TestPatchOpenCodePluginRejectsUnexpectedOutput(t *testing.T) {
	if _, err := patchOpenCodePlugin([]byte("changed PromptScript output")); err == nil {
		t.Fatal("patchOpenCodePlugin() accepted unexpected output")
	}
	if _, err := patchOpenCodePlugin([]byte(openCodePatchMarker + "\nMAX_PATH_ARGUMENT_BYTES = 2048")); err == nil {
		t.Fatal("patchOpenCodePlugin() accepted an incomplete patch")
	}
}

// TestPatchOpenCodePluginNamesTheBrokenEdit pins that a generated plugin the
// patch no longer fits fails with the name of the edit, so a PromptScript
// upgrade points at the code to update.
func TestPatchOpenCodePluginNamesTheBrokenEdit(t *testing.T) {
	oneHook := strings.Replace(testOpenCodePlugin, "void runRule(", "runRule(", 1)
	_, err := patchOpenCodePlugin([]byte(oneHook))
	requireErrorContaining(t, err, "hook starts")

	patched, err := patchOpenCodePlugin([]byte(testOpenCodePlugin))
	if err != nil {
		t.Fatal(err)
	}
	oneStart := strings.Replace(string(patched), "running.push(runRule(", "runRule(", 1)
	_, err = patchOpenCodePlugin([]byte(oneStart))
	requireErrorContaining(t, err, "hook starts")

	oneWait := strings.Replace(string(patched), "await Promise.all(running);\n", "", 1)
	_, err = patchOpenCodePlugin([]byte(oneWait))
	requireErrorContaining(t, err, "before hook wait")
}

func TestPatchOpenCodeArtifactsFailures(t *testing.T) {
	t.Run("plugin is missing", func(t *testing.T) {
		err := patchOpenCodeArtifacts(t.TempDir())
		requireErrorContaining(t, err, "read generated OpenCode plugin")
	})

	t.Run("plugin is not PromptScript output", func(t *testing.T) {
		root := t.TempDir()
		writePromptScriptFile(t, root, openCodePluginRel, "changed PromptScript output\n")
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "patch generated OpenCode plugin")
	})

	t.Run("harness template directory is missing", func(t *testing.T) {
		root := t.TempDir()
		writePromptScriptFile(t, root, openCodePluginRel, testOpenCodePlugin)
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "write OpenCode harness template")
	})

	t.Run("plugin cannot be rewritten", func(t *testing.T) {
		if runtime.GOOS != "windows" && os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		root := t.TempDir()
		writePromptScriptFile(t, root, openCodePluginRel, testOpenCodePlugin)
		writePromptScriptFile(t, root, openCodeTemplateRel, "stale\n")
		path := filepath.Join(root, openCodePluginRel)
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatalf("make plugin read-only: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "write generated OpenCode plugin")
	})

	t.Run("agent is missing", func(t *testing.T) {
		root := openCodePluginFixture(t)
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "read generated OpenCode agent code-reviewer")
	})

	t.Run("agent is not PromptScript output", func(t *testing.T) {
		root := openCodePluginFixture(t)
		writeOpenCodeAgents(t, root, "changed PromptScript output\n")
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "patch generated OpenCode agent code-reviewer")
	})

	t.Run("agent cannot be rewritten", func(t *testing.T) {
		if runtime.GOOS != "windows" && os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		root := openCodePluginFixture(t)
		writeOpenCodeAgents(t, root, testOpenCodeAgent)
		path := filepath.Join(root, openCodeAgentRel("code-reviewer"))
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatalf("make agent read-only: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
		err := patchOpenCodeArtifacts(root)
		requireErrorContaining(t, err, "write generated OpenCode agent code-reviewer")
	})
}

// openCodePluginFixture holds a generated plugin and its harness template,
// so a patch run reaches the subagents.
func openCodePluginFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writePromptScriptFile(t, root, openCodePluginRel, testOpenCodePlugin)
	writePromptScriptFile(t, root, openCodeTemplateRel, "stale\n")
	return root
}

func writeOpenCodeAgents(t *testing.T, root, content string) {
	t.Helper()
	for _, name := range openCodeAgentNames {
		writePromptScriptFile(t, root, openCodeAgentRel(name), content)
	}
}

// requireOpenCodeAgentsReadOnly pins the exact patched subagent, so a second
// patch run that stacked another permission block would fail too.
func requireOpenCodeAgentsReadOnly(t *testing.T, root string) {
	t.Helper()
	for _, name := range openCodeAgentNames {
		agent, err := os.ReadFile(filepath.Join(root, openCodeAgentRel(name)))
		if err != nil {
			t.Fatal(err)
		}
		if string(agent) != testPatchedOpenCodeAgent {
			t.Errorf("OpenCode agent %s = %q, want %q", name, agent, testPatchedOpenCodeAgent)
		}
	}
}

func requireErrorContaining(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err, want)
	}
}

// writeDriftOutputs commits generated output for every PromptScript file. The
// fake PromptScript writes the text "generated" for all of them, except the
// OpenCode plugin and subagents, which hold the given content.
func writeDriftOutputs(t *testing.T, root, plugin, agent string) {
	t.Helper()
	agents := map[string]bool{}
	for _, name := range openCodeAgentNames {
		agents[openCodeAgentRel(name)] = true
	}
	for _, rel := range promptScriptOutputs {
		content := "generated"
		switch {
		case rel == openCodePluginRel:
			content = plugin
		case agents[rel]:
			content = agent
		}
		writePromptScriptFile(t, root, rel, content)
	}
}

func TestCheckDriftComparesThePatchedOpenCodePlugin(t *testing.T) {
	patched, err := patchOpenCodePlugin([]byte(testOpenCodePlugin))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("patched plugin is in sync", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "all-outputs")
		writeDriftOutputs(t, root, string(patched), testPatchedOpenCodeAgent)
		if err := checkDrift("promptscript", root); err != nil {
			t.Fatalf("checkDrift() = %v, want nil", err)
		}
	})
	t.Run("unpatched plugin is out of sync", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "all-outputs")
		writeDriftOutputs(t, root, testOpenCodePlugin, testPatchedOpenCodeAgent)
		err := checkDrift("promptscript", root)
		requireErrorContaining(t, err, openCodePluginRel+" is out of sync")
	})
	t.Run("generated plugin does not fit the patch", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "plugin-unexpected")
		writeDriftOutputs(t, root, string(patched), testPatchedOpenCodeAgent)
		err := checkDrift("promptscript", root)
		requireErrorContaining(t, err, "patch generated OpenCode plugin")
	})
	t.Run("committed output is missing", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "all-outputs")
		err := checkDrift("promptscript", root)
		requireErrorContaining(t, err, "read committed")
	})
}

func TestCheckDriftComparesThePatchedOpenCodeAgents(t *testing.T) {
	patched, err := patchOpenCodePlugin([]byte(testOpenCodePlugin))
	if err != nil {
		t.Fatal(err)
	}
	t.Run("unpatched agent is out of sync", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "all-outputs")
		writeDriftOutputs(t, root, string(patched), testOpenCodeAgent)
		err := checkDrift("promptscript", root)
		requireErrorContaining(t, err, openCodeAgentRel("code-reviewer")+" is out of sync")
	})
	t.Run("generated agent does not fit the patch", func(t *testing.T) {
		root := promptScriptFixture(t)
		usePromptScript(t, "agent-unexpected")
		writeDriftOutputs(t, root, string(patched), testPatchedOpenCodeAgent)
		err := checkDrift("promptscript", root)
		requireErrorContaining(t, err, "patch generated OpenCode agent code-reviewer")
	})
}

// testOpenCodeAgent is the PromptScript 1.19.1 OpenCode subagent layout: a
// stamp comment, a description, and the mode.
const testOpenCodeAgent = `---
# promptscript-generated: 2026-10-07T14:51:15.613Z | source: .promptscript/project.prs | target: opencode
description: Review a diff for correctness
mode: subagent
---

Review only requested changes. Do not modify files.
`

// testPatchedOpenCodeAgent is testOpenCodeAgent after the patch, written out
// so the tests do not depend on the code that produces it.
const testPatchedOpenCodeAgent = `---
# promptscript-generated: 2026-10-07T14:51:15.613Z | source: .promptscript/project.prs | target: opencode
description: Review a diff for correctness
mode: subagent
permission:
  "*": deny
  read:
    "*": allow
    "*.env": ask
    "*.env.*": ask
    "*.env.example": allow
  grep: allow
  glob: allow
---

Review only requested changes. Do not modify files.
`

// testOpenCodePlugin keeps the regions of the PromptScript 1.19.1 OpenCode
// plugin that the patch rewrites, verbatim, and drops the rest.
const testOpenCodePlugin = `// promptscript-generated: opencode-plugin
function payloadByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength;
}

// Keep the payload bounded: drop oversized tool arguments first, then the
// result object, so the command always receives parseable JSON.
function boundedPayload(payload: Record<string, unknown>): string {
  const full = safeStringify(payload);
  if (payloadByteLength(full) <= PAYLOAD_LIMIT_BYTES) return full;
  const withoutArgs = { ...payload, args: '[truncated]' };
  const trimmed = safeStringify(withoutArgs);
  if (payloadByteLength(trimmed) <= PAYLOAD_LIMIT_BYTES) return trimmed;
  const withoutResult = safeStringify({ ...withoutArgs, result: '[truncated]' });
  if (payloadByteLength(withoutResult) <= PAYLOAD_LIMIT_BYTES) return withoutResult;
  return '{"target":"opencode","args":"[truncated]","result":"[truncated]"}';
}

// Hooks observe tool execution asynchronously. Failures are logged, and every
// process gets a bounded lifetime so a broken hook cannot disrupt the session.
async function runRule(
  rule: OpenCodeHookRule,
  projectRoot: string,
  payload: string
): Promise<void> {
  console.log(rule, projectRoot, payload);
}

export const PromptScriptHooks = async (context: OpenCodePluginContext) => {
  const projectRoot = context.worktree || context.directory;

  return {
    'tool.execute.before': async (input: OpenCodeToolInput, output: OpenCodeToolOutput) => {
      for (const entry of compiled) {
        if (entry.rule.event !== 'tool.execute.before') continue;
        if (entry.matcher !== null && !entry.matcher.test(String(input.tool))) continue;
        void runRule(
          entry.rule,
          projectRoot,
          boundedPayload(buildPayload(entry.rule, input, output.args))
        );
      }
    },
    'tool.execute.after': async (input: OpenCodeToolInput, output: OpenCodeToolResult) => {
      for (const entry of compiled) {
        if (entry.rule.event !== 'tool.execute.after') continue;
        if (entry.matcher !== null && !entry.matcher.test(String(input.tool))) continue;
        const result = {
          title: output.title,
          output: output.output,
          metadata: output.metadata
        };
        void runRule(
          entry.rule,
          projectRoot,
          boundedPayload(buildPayload(entry.rule, input, input.args, result))
        );
      }
    }
  };
};
`

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
		filepath.Join(".opencode", "plugins", "promptscript.ts"):      "opencode",
	}
	for rel, agent := range outputs {
		if agent == "opencode" {
			content := `["git-byline","checkpoint","portable-opencode","--type","human"]` + "\n"
			if complete {
				content += `["git-byline","checkpoint","portable-opencode","--type","ai"]` + "\n"
			}
			writePromptScriptFile(t, root, rel, content)
			continue
		}
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
		"  case \"$mode\" in all-outputs|plugin-unexpected|agent-unexpected)\n" +
		"    for rel in $PROMPTSCRIPT_TEST_OUTPUTS; do\n" +
		"      mkdir -p \"$(dirname \"$rel\")\"\n" +
		"      if [ \"$rel\" = \"$PROMPTSCRIPT_TEST_PLUGIN_REL\" ] && [ \"$mode\" != \"plugin-unexpected\" ]; then\n" +
		"        cp \"$PROMPTSCRIPT_TEST_PLUGIN\" \"$rel\"\n" +
		"      elif [ \"$(dirname \"$rel\")\" = \"$PROMPTSCRIPT_TEST_AGENT_DIR\" ] && [ \"$mode\" = \"all-outputs\" ]; then\n" +
		"        cp \"$PROMPTSCRIPT_TEST_AGENT\" \"$rel\"\n" +
		"      else\n" +
		"        printf '%s' generated > \"$rel\"\n" +
		"      fi\n" +
		"    done;;\n" +
		"  esac\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	useFakeTool(t, "promptscript", script)
	pluginSource := filepath.Join(t.TempDir(), "plugin.ts")
	if err := os.WriteFile(pluginSource, []byte(testOpenCodePlugin), 0o644); err != nil {
		t.Fatalf("write generated plugin source: %v", err)
	}
	agentSource := filepath.Join(t.TempDir(), "agent.md")
	if err := os.WriteFile(agentSource, []byte(testOpenCodeAgent), 0o644); err != nil {
		t.Fatalf("write generated agent source: %v", err)
	}
	t.Setenv("PROMPTSCRIPT_TEST_MODE", mode)
	t.Setenv("PROMPTSCRIPT_TEST_OUTPUTS", strings.Join(promptScriptOutputs, " "))
	t.Setenv("PROMPTSCRIPT_TEST_PLUGIN", pluginSource)
	t.Setenv("PROMPTSCRIPT_TEST_PLUGIN_REL", openCodePluginRel)
	t.Setenv("PROMPTSCRIPT_TEST_AGENT", agentSource)
	t.Setenv("PROMPTSCRIPT_TEST_AGENT_DIR", filepath.Dir(openCodeAgentRel(openCodeAgentNames[0])))
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
