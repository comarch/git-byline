package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func checkInstallers(root string) error {
	shellPath := filepath.Join(root, "install.sh")
	powerShellPath := filepath.Join(root, "install.ps1")
	shell, err := os.ReadFile(shellPath)
	if err != nil {
		return fmt.Errorf("read install.sh: %w", err)
	}
	powerShell, err := os.ReadFile(powerShellPath)
	if err != nil {
		return fmt.Errorf("read install.ps1: %w", err)
	}
	if info, err := os.Stat(shellPath); err != nil {
		return fmt.Errorf("stat install.sh: %w", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return errors.New("install.sh is not executable")
	}
	for name, content := range map[string][]byte{
		"install.sh":  shell,
		"install.ps1": powerShell,
	} {
		text := string(content)
		for _, required := range []string{
			"https://github.com/comarch/git-byline",
			"checksums.txt",
			"git-byline $version",
			"already installed",
			"install-hooks --agent none --git --template",
		} {
			if !strings.Contains(strings.ToLower(text), strings.ToLower(required)) {
				return fmt.Errorf("%s is missing %q", name, required)
			}
		}
		if strings.Contains(text, "sudo") {
			return fmt.Errorf("%s must not use sudo", name)
		}
	}
	for _, required := range []string{"--proto '=https'", "sha256sum", "shasum -a 256"} {
		if !strings.Contains(string(shell), required) {
			return fmt.Errorf("install.sh is missing %q", required)
		}
	}
	for _, required := range []string{"Get-FileHash", "SHA256", "Invoke-WebRequest"} {
		if !strings.Contains(string(powerShell), required) {
			return fmt.Errorf("install.ps1 is missing %q", required)
		}
	}
	// Agent detection must stay opt-out and must never guess a user-level
	// path for an agent git-byline cannot configure itself.
	for _, required := range []string{
		"--no-agent-hooks",
		"--git-template",
		"install-hooks --agent",
		"marketplace/harness",
		"detected opencode",
	} {
		if !strings.Contains(string(shell), required) {
			return fmt.Errorf("install.sh is missing %q", required)
		}
	}
	for _, required := range []string{
		"NoAgentHooks",
		"GitTemplate",
		"install-hooks",
		"marketplace/harness",
	} {
		if !strings.Contains(string(powerShell), required) {
			return fmt.Errorf("install.ps1 is missing %q", required)
		}
	}
	if strings.Contains(string(powerShell), `Command = "opencode"`) {
		return errors.New("install.ps1 must not advertise OpenCode plugin installation")
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		if _, err := runCapture(sh, root, "-n", shellPath); err != nil {
			return fmt.Errorf("install.sh syntax: %w", err)
		}
	}
	if pwsh, err := exec.LookPath("pwsh"); err == nil {
		command := exec.Command(
			pwsh,
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			`$path = [Environment]::GetEnvironmentVariable("GIT_BYLINE_INSTALLER_PATH"); $null = [scriptblock]::Create((Get-Content -Raw -LiteralPath $path))`,
		)
		command.Dir = root
		command.Env = append(os.Environ(), "GIT_BYLINE_INSTALLER_PATH="+powerShellPath)
		if out, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("install.ps1 syntax: %w\n%s", err, out)
		}
	}
	for _, rel := range []string{
		filepath.Join(".factory-plugin", "marketplace.json"),
		filepath.Join(".claude-plugin", "marketplace.json"),
		filepath.Join("marketplace", "git-byline", ".factory-plugin", "plugin.json"),
		filepath.Join("marketplace", "git-byline", ".claude-plugin", "plugin.json"),
		"gemini-extension.json",
	} {
		if err := validateJSONObject(filepath.Join(root, rel)); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
	}
	pluginLicense, err := os.ReadFile(filepath.Join(root, "marketplace", "git-byline", "LICENSE"))
	if err != nil {
		return fmt.Errorf("read plugin license: %w", err)
	}
	rootLicense, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		return fmt.Errorf("read root license: %w", err)
	}
	if !bytes.Equal(pluginLicense, rootLicense) {
		return errors.New("plugin license differs from root license")
	}
	if err := checkPluginManifests(root); err != nil {
		return err
	}
	return checkHarnessTemplates(root)
}

// checkPluginManifests keeps the per-agent plugin manifests describing the
// same plugin. One directory carries a manifest per agent, so a version or
// license bump in one must not silently leave the others behind.
func checkPluginManifests(root string) error {
	plugin := filepath.Join(root, "marketplace", "git-byline")
	factory, err := readJSONObject(filepath.Join(plugin, ".factory-plugin", "plugin.json"))
	if err != nil {
		return err
	}
	claude, err := readJSONObject(filepath.Join(plugin, ".claude-plugin", "plugin.json"))
	if err != nil {
		return err
	}
	gemini, err := readJSONObject(filepath.Join(root, "gemini-extension.json"))
	if err != nil {
		return err
	}
	for _, field := range []string{"name", "version", "description"} {
		if factory[field] != claude[field] {
			return fmt.Errorf("plugin manifests disagree on %q", field)
		}
	}
	for _, field := range []string{"name", "version"} {
		if factory[field] != gemini[field] {
			return fmt.Errorf("gemini extension manifest disagrees on %q", field)
		}
	}
	if claude["license"] != "MIT" {
		return errors.New("claude plugin manifest is not MIT licensed")
	}
	return nil
}

// harnessTemplates maps each copyable hook template to the generated file it
// mirrors. Users copy the template into their own project, so a template that
// drifts from the generated hook would install a stale hook body.
var harnessTemplates = map[string]string{
	filepath.Join("marketplace", "harness", "copilot", "hooks.json"):       filepath.Join(".github", "hooks", "promptscript.json"),
	filepath.Join("marketplace", "harness", "vscode", "hooks.json"):        filepath.Join(".github", "hooks", "promptscript-vscode.json"),
	filepath.Join("marketplace", "harness", "cursor", "hooks.json"):        filepath.Join(".cursor", "hooks.json"),
	filepath.Join("marketplace", "harness", "codex", "hooks.json"):         filepath.Join(".codex", "hooks.json"),
	filepath.Join("marketplace", "harness", "windsurf", "hooks.json"):      filepath.Join(".windsurf", "hooks.json"),
	filepath.Join("marketplace", "harness", "grok", "hooks.json"):          filepath.Join(".grok", "hooks", "promptscript.json"),
	filepath.Join("marketplace", "harness", "opencode", "promptscript.ts"): filepath.Join(".opencode", "plugins", "promptscript.ts"),
	filepath.Join("marketplace", "harness", "gemini", "settings.json"):     filepath.Join(".gemini", "settings.json"),
}

// setupCommandFile is the file name of the setup command of most agents.
const setupCommandFile = "git-byline-setup.md"

// openCodeSetupCommand is the one setup command whose plugin target is
// Unix-only, so it must keep the WSL path for Windows users.
var openCodeSetupCommand = filepath.Join("marketplace", "harness", "opencode", setupCommandFile)

// setupCommands lists every agent-facing setup command shipped by the
// repository. Each one must describe the same verified installation.
var setupCommands = []string{
	filepath.Join("marketplace", "git-byline", "commands", setupCommandFile),
	filepath.Join("marketplace", "harness", "copilot", "git-byline-setup.prompt.md"),
	filepath.Join("marketplace", "harness", "vscode", "git-byline-setup.prompt.md"),
	filepath.Join("marketplace", "harness", "cursor", setupCommandFile),
	filepath.Join("marketplace", "harness", "codex", setupCommandFile),
	filepath.Join("marketplace", "harness", "windsurf", setupCommandFile),
	filepath.Join("marketplace", "harness", "grok", setupCommandFile),
	openCodeSetupCommand,
	filepath.Join("commands", "git-byline-setup.toml"),
}

func checkHarnessTemplates(root string) error {
	for template, generated := range harnessTemplates {
		want, err := os.ReadFile(filepath.Join(root, generated))
		if err != nil {
			return fmt.Errorf("read generated hook %s: %w", generated, err)
		}
		got, err := os.ReadFile(filepath.Join(root, template))
		if err != nil {
			return fmt.Errorf("read hook template %s: %w", template, err)
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("hook template %s differs from %s", template, generated)
		}
	}
	for _, rel := range setupCommands {
		if err := checkSetupCommand(root, rel); err != nil {
			return err
		}
	}
	return nil
}

// checkSetupCommand holds one setup command to the verified installation.
func checkSetupCommand(root, rel string) error {
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return fmt.Errorf("read setup command %s: %w", rel, err)
	}
	text := string(data)
	for _, required := range []string{
		"install.sh",
		"install.ps1",
		"--no-git-hook",
		"install-hooks",
		"status",
	} {
		if !strings.Contains(text, required) {
			return fmt.Errorf("setup command %s is missing %q", rel, required)
		}
	}
	// The commands tell the agent never to use sudo, so match an
	// invocation rather than the word.
	if sudoInvocation.MatchString(text) {
		return fmt.Errorf("setup command %s must not use sudo", rel)
	}
	for _, block := range powerShellBlock.FindAllStringSubmatch(text, -1) {
		if powerShellCurlAlias.MatchString(block[1]) {
			return fmt.Errorf("setup command %s uses the PowerShell curl alias, use irm or curl.exe", rel)
		}
	}
	if rel == openCodeSetupCommand {
		if !strings.Contains(text, "WSL") {
			return fmt.Errorf("setup command %s must document the WSL path for Windows", rel)
		}
		if err := checkOpenCodePluginPin(root, text); err != nil {
			return fmt.Errorf("setup command %s: %w", rel, err)
		}
	}
	return nil
}

// releasePinMarker tags a line whose release tag Release Please rewrites on
// every release.
const releasePinMarker = "x-release-please-version"

var (
	releaseTagPattern   = regexp.MustCompile(`v[0-9]+\.[0-9]+\.[0-9]+`)
	sha256DigestPattern = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

	// openCodePluginURL captures the ref of a URL of the plugin, in both the
	// raw.githubusercontent.com and the github.com/raw forms. The ref can span
	// segments, as in refs/heads/main, so any ref but the pin fails.
	openCodePluginURL = regexp.MustCompile(
		`/comarch/git-byline/(?:raw/)?([^\s"']+?)/marketplace/harness/opencode/promptscript\.ts`,
	)
)

// checkOpenCodePluginPin keeps the plugin install of the OpenCode setup
// command safe to run. OpenCode loads every file of .opencode/plugins as
// code, so the command must download the plugin from a release tag, never
// from a branch, and must compare the download with the SHA-256 of the plugin
// this repository ships before the file reaches that directory. Release
// Please rewrites the tag on every release, so it must follow the manifest,
// like the pin of the CI templates.
func checkOpenCodePluginPin(root, setup string) error {
	if err := checkOpenCodeReleasePin(root, setup); err != nil {
		return err
	}
	if err := checkOpenCodePluginURLs(setup); err != nil {
		return err
	}
	return checkOpenCodePluginDigest(root, setup)
}

// checkOpenCodeReleasePin requires every marked pin line to hold the version
// of the release manifest.
func checkOpenCodeReleasePin(root, setup string) error {
	version, err := releaseManifestVersion(root)
	if err != nil {
		return err
	}
	pins := 0
	for _, line := range strings.Split(setup, "\n") {
		if !strings.Contains(line, releasePinMarker) {
			continue
		}
		tags := releaseTagPattern.FindAllString(line, -1)
		if len(tags) != 1 {
			return fmt.Errorf("release pin line must hold one release tag, found %d", len(tags))
		}
		if tags[0] != "v"+version {
			return fmt.Errorf("release pin is %s, release manifest is v%s", tags[0], version)
		}
		pins++
	}
	if pins == 0 {
		return fmt.Errorf("has no release pin line marked %s", releasePinMarker)
	}
	return nil
}

// checkOpenCodePluginURLs requires every download of the plugin to use the
// release pin as its ref.
func checkOpenCodePluginURLs(setup string) error {
	downloads := openCodePluginURL.FindAllStringSubmatch(setup, -1)
	if len(downloads) == 0 {
		return errors.New("does not download the plugin from the release pin")
	}
	for _, download := range downloads {
		if download[1] != "$tag" {
			return fmt.Errorf("downloads the plugin from %s, want the release pin $tag", download[1])
		}
	}
	return nil
}

// checkOpenCodePluginDigest requires the setup command to verify the download
// with the SHA-256 of the plugin this repository ships.
func checkOpenCodePluginDigest(root, setup string) error {
	for _, tool := range []string{"sha256sum", "shasum -a 256", "Get-FileHash"} {
		if !strings.Contains(setup, tool) {
			return fmt.Errorf("does not verify the plugin with %s", tool)
		}
	}
	plugin, err := os.ReadFile(filepath.Join(root, openCodeTemplateRel))
	if err != nil {
		return fmt.Errorf("read OpenCode plugin: %w", err)
	}
	// Git serves the committed bytes, which hold no CR, while a Windows
	// checkout may convert line endings.
	sum := sha256.Sum256(bytes.ReplaceAll(plugin, []byte("\r\n"), []byte("\n")))
	want := hex.EncodeToString(sum[:])
	// The sh block compares the digest as text, and sha256sum prints lowercase.
	digests := sha256DigestPattern.FindAllString(setup, -1)
	if len(digests) == 0 {
		return errors.New("lists no lowercase SHA-256 of the plugin")
	}
	for _, digest := range digests {
		if digest != want {
			return fmt.Errorf("lists SHA-256 %s, but %s hashes to %s", digest, filepath.ToSlash(openCodeTemplateRel), want)
		}
	}
	return nil
}

var sudoInvocation = regexp.MustCompile(`(?m)(^|[\s;&|(])sudo\s`)

// powerShellBlock captures the body of a fenced PowerShell example.
var powerShellBlock = regexp.MustCompile("(?s)```powershell\n(.*?)```")

// powerShellCurlAlias matches bare curl with flags. Windows PowerShell binds
// that name to Invoke-WebRequest, which rejects the flags the sh examples use.
// curl.exe is the real tool and does not match.
var powerShellCurlAlias = regexp.MustCompile(`(?m)(^|[\s;&|(])curl\s+-`)

func readJSONObject(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return value, nil
}

func validateJSONObject(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}
