// promptscript.go guards generated project instructions: the PromptScript
// CLI is pinned, source must pass strict validation, and committed outputs
// must match what the source compiles to.
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// pinnedPromptScriptVersion is the PromptScript CLI version this
// repository is validated against. Upgrade it deliberately: regenerate
// all generated files, review the output diff, and update the pin in the
// same change.
const pinnedPromptScriptVersion = "1.18.1"

// semverPattern matches a semantic version inside tool version output.
var semverPattern = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)

// generatedStampPattern removes nondeterministic compile metadata while
// preserving the PromptScript source path.
var generatedStampPattern = regexp.MustCompile(
	`(?m)^# promptscript-generated: [^|\r\n]+ \| source: ([^|\r\n]+) \| target: [^\r\n]+$`,
)

// generatedCommentStampPattern handles the equivalent Markdown marker.
var generatedCommentStampPattern = regexp.MustCompile(
	`(?m)^<!-- PromptScript [^|\r\n]+ \| source: ([^|\r\n]+) \| target: [^>\r\n]+ -->$`,
)

var promptScriptOutputs = []string{
	"AGENTS.md",
	"CLAUDE.md",
	"GEMINI.md",
	filepath.Join(".claude", "agents", "code-reviewer.md"),
	filepath.Join(".claude", "agents", "release-keeper.md"),
	filepath.Join(".claude", "agents", "security-reviewer.md"),
	filepath.Join(".claude", "settings.json"),
	filepath.Join(".claude", "workflows", "bugfix.md"),
	filepath.Join(".claude", "workflows", "feature.md"),
	filepath.Join(".claude", "workflows", "hooks.md"),
	filepath.Join(".claude", "workflows", "promptscript.md"),
	filepath.Join(".claude", "workflows", "release.md"),
	filepath.Join(".claude", "workflows", "storage.md"),
	filepath.Join(".codex", "agents", "code-reviewer.toml"),
	filepath.Join(".codex", "agents", "release-keeper.toml"),
	filepath.Join(".codex", "agents", "security-reviewer.toml"),
	filepath.Join(".codex", "config.toml"),
	filepath.Join(".codex", "hooks.json"),
	filepath.Join(".cursor", "agents", "code-reviewer.md"),
	filepath.Join(".cursor", "agents", "release-keeper.md"),
	filepath.Join(".cursor", "agents", "security-reviewer.md"),
	filepath.Join(".cursor", "hooks.json"),
	filepath.Join(".cursor", "rules", "project.mdc"),
	filepath.Join(".factory", "droids", "code-reviewer.md"),
	filepath.Join(".factory", "droids", "release-keeper.md"),
	filepath.Join(".factory", "droids", "security-reviewer.md"),
	filepath.Join(".factory", "hooks.json"),
	filepath.Join(".gemini", "settings.json"),
	filepath.Join(".github", "agents", "code-reviewer.md"),
	filepath.Join(".github", "agents", "release-keeper.md"),
	filepath.Join(".github", "agents", "security-reviewer.md"),
	filepath.Join(".github", "copilot-instructions.md"),
	filepath.Join(".github", "hooks", "promptscript-vscode.json"),
	filepath.Join(".github", "hooks", "promptscript.json"),
	filepath.Join(".grok", "hooks", "promptscript.json"),
	filepath.Join(".promptscript", "generated", "codex.md"),
	filepath.Join(".windsurf", "hooks.json"),
	filepath.Join(".windsurf", "rules", "project.md"),
}

// parseSemver extracts the first semantic version from text.
func parseSemver(text string) string {
	return semverPattern.FindString(text)
}

// checkPromptScript validates the PromptScript setup of the repository.
func checkPromptScript(root string) error {
	if err := requirePromptScriptFiles(root); err != nil {
		return err
	}
	bin, err := exec.LookPath("promptscript")
	if err != nil {
		return fmt.Errorf("promptscript CLI not found in PATH, pinned version %s: %w", pinnedPromptScriptVersion, err)
	}
	versionOut, err := runCapture(bin, root, "--version")
	if err != nil {
		return fmt.Errorf("promptscript --version: %w", err)
	}
	got := parseSemver(versionOut)
	if got == "" {
		return fmt.Errorf("promptscript --version output contains no version: %q", strings.TrimSpace(versionOut))
	}
	if got != pinnedPromptScriptVersion {
		return fmt.Errorf("promptscript CLI version %s does not match the pinned %s; upgrade the pin deliberately and regenerate project instructions", got, pinnedPromptScriptVersion)
	}
	if _, err := runCapture(bin, root, "validate", "--strict", ".promptscript/project.prs"); err != nil {
		return fmt.Errorf("promptscript strict validation: %w", err)
	}
	if err := checkPortableHookOutputs(root); err != nil {
		return err
	}
	return checkDrift(bin, root)
}

func checkPortableHookOutputs(root string) error {
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
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("read generated hook output %s: %w", rel, err)
		}
		for _, author := range []string{"human", "ai"} {
			signature := fmt.Sprintf("checkpoint portable-%s --type %s", agent, author)
			if !bytes.Contains(data, []byte(signature)) {
				return fmt.Errorf("%s is missing %s hook", rel, author)
			}
		}
	}
	return nil
}

// requirePromptScriptFiles verifies the source and config exist.
func requirePromptScriptFiles(root string) error {
	for _, rel := range []string{
		filepath.Join(".promptscript", "project.prs"),
		"promptscript.yaml",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			return fmt.Errorf("promptscript file %s missing: %w", rel, err)
		}
	}
	return nil
}

// checkDrift compiles PromptScript source in an isolated copy and compares
// all outputs with committed files. The copy keeps the repository untouched
// even when a committed file has drifted.
func checkDrift(bin, root string) error {
	tmp, err := os.MkdirTemp("", "byline-drift-")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	srcDir := filepath.Join(tmp, ".promptscript")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", srcDir, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".promptscript"))
	if err != nil {
		return fmt.Errorf("read PromptScript source directory: %w", err)
	}
	files := []struct{ src, dst string }{
		{filepath.Join(root, "promptscript.yaml"), filepath.Join(tmp, "promptscript.yaml")},
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".prs" {
			continue
		}
		files = append(files, struct{ src, dst string }{
			src: filepath.Join(root, ".promptscript", entry.Name()),
			dst: filepath.Join(srcDir, entry.Name()),
		})
	}
	for _, f := range files {
		data, err := os.ReadFile(f.src)
		if err != nil {
			return fmt.Errorf("read %s: %w", f.src, err)
		}
		if err := os.WriteFile(f.dst, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", f.dst, err)
		}
	}
	if _, err := runCapture(bin, tmp, "compile", "--all-builds", "--force", "--strict"); err != nil {
		return fmt.Errorf("compile build profiles in isolated copy: %w", err)
	}
	if _, err := runCapture(bin, tmp, "compile", "--all", "--force", "--strict"); err != nil {
		return fmt.Errorf("compile targets in isolated copy: %w", err)
	}
	compiled, err := compiledOutputPaths(tmp)
	if err != nil {
		return err
	}
	expected := make(map[string]bool, len(promptScriptOutputs))
	for _, rel := range promptScriptOutputs {
		expected[filepath.Clean(rel)] = true
	}
	for _, rel := range compiled {
		if !expected[rel] {
			return fmt.Errorf("untracked PromptScript output %s; add it to promptScriptOutputs", rel)
		}
	}
	for _, rel := range promptScriptOutputs {
		generated, err := os.ReadFile(filepath.Join(tmp, rel))
		if err != nil {
			return fmt.Errorf("read generated %s: %w", rel, err)
		}
		committed, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return fmt.Errorf("read committed %s: %w", rel, err)
		}
		generated = normalizeGeneratedStamps(generated)
		committed = normalizeGeneratedStamps(committed)
		if !bytes.Equal(generated, committed) {
			return fmt.Errorf(
				"%s is out of sync with .promptscript sources; run all build profiles, then all targets",
				rel,
			)
		}
	}
	return nil
}

func compiledOutputPaths(root string) ([]string, error) {
	var outputs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "promptscript.yaml" ||
			strings.HasPrefix(rel, ".promptscript"+string(filepath.Separator)) &&
				filepath.Ext(rel) == ".prs" {
			return nil
		}
		outputs = append(outputs, filepath.Clean(rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list compiled PromptScript outputs: %w", err)
	}
	return outputs, nil
}

func normalizeGeneratedStamps(data []byte) []byte {
	data = generatedStampPattern.ReplaceAll(
		data,
		[]byte("# promptscript-generated: <generated> | source: $1 | target: <target>"),
	)
	return generatedCommentStampPattern.ReplaceAll(
		data,
		[]byte("<!-- PromptScript <generated> | source: $1 | target: <target> -->"),
	)
}

// runCapture runs a command in dir, capturing combined output, and wraps
// failures with that output.
func runCapture(bin, dir string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w\n%s", filepath.Base(bin), strings.Join(args, " "), err, out)
	}
	return string(out), nil
}
