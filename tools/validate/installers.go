package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	if sh, err := exec.LookPath("sh"); err == nil {
		if _, err := runCapture(sh, root, "-n", shellPath); err != nil {
			return fmt.Errorf("install.sh syntax: %w", err)
		}
	}
	if pwsh, err := exec.LookPath("pwsh"); err == nil {
		if _, err := runCapture(
			pwsh,
			root,
			"-NoProfile",
			"-NonInteractive",
			"-Command",
			`$null = [scriptblock]::Create((Get-Content -Raw -LiteralPath $args[0]))`,
			powerShellPath,
		); err != nil {
			return fmt.Errorf("install.ps1 syntax: %w", err)
		}
	}
	for _, rel := range []string{
		filepath.Join(".factory-plugin", "marketplace.json"),
		filepath.Join("marketplace", "git-byline", ".factory-plugin", "plugin.json"),
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
	return nil
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
