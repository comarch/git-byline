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

// generatedStampPattern removes the nondeterministic compile timestamp.
var generatedStampPattern = regexp.MustCompile(`(?m)^# promptscript-generated: [^|\r\n]+ \|`)

var promptScriptOutputs = []string{
	"AGENTS.md",
	filepath.Join(".factory", "droids", "code-reviewer.md"),
	filepath.Join(".factory", "droids", "release-keeper.md"),
	filepath.Join(".factory", "droids", "security-reviewer.md"),
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
	return checkDrift(bin, root)
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
	if _, err := runCapture(bin, tmp, "compile", "--all", "--force"); err != nil {
		return fmt.Errorf("compile in isolated copy: %w", err)
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
		generated = generatedStampPattern.ReplaceAll(generated, []byte("# promptscript-generated: <generated> |"))
		committed = generatedStampPattern.ReplaceAll(committed, []byte("# promptscript-generated: <generated> |"))
		if !bytes.Equal(generated, committed) {
			return fmt.Errorf("%s is out of sync with .promptscript sources; run: promptscript compile --all --force", rel)
		}
	}
	return nil
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
