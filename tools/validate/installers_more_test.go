package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	shellInstaller        = "install.sh"
	powerShellInstaller   = "install.ps1"
	pluginFactoryManifest = "marketplace/git-byline/.factory-plugin/plugin.json"
	pluginClaudeManifest  = "marketplace/git-byline/.claude-plugin/plugin.json"
	geminiManifest        = "gemini-extension.json"
)

type installerTextReplacement struct {
	rel         string
	old         string
	replacement string
}

type pluginManifestFailureCase struct {
	name          string
	check         func(string) error
	remove        string
	removeMessage string
	replace       *installerTextReplacement
	failure       string
}

func TestCheckInstallersInputFailures(t *testing.T) {
	tests := []struct {
		name      string
		skip      string
		setup     func(*testing.T, string)
		want      string
		posixOnly bool
	}{
		{
			name:  "shell installer missing",
			setup: func(t *testing.T, root string) { removeInstallerFile(t, root, shellInstaller) },
			want:  "missing shell installer",
		},
		{
			name:  "PowerShell installer missing",
			setup: func(t *testing.T, root string) { removeInstallerFile(t, root, powerShellInstaller) },
			want:  "missing PowerShell installer",
		},
		{
			name: "shell installer is not executable",
			setup: func(t *testing.T, root string) {
				if err := os.Chmod(filepath.Join(root, shellInstaller), 0o644); err != nil {
					t.Fatalf("chmod shell installer: %v", err)
				}
			},
			want:      "executable permission failure",
			posixOnly: true,
			skip:      "POSIX executable permission check",
		},
		{
			name: "common requirement is missing",
			setup: func(t *testing.T, root string) {
				replaceInstallerText(t, root, shellInstaller, "checksums.txt", "checksum-file")
			},
			want: "common requirement failure",
		},
		{
			name: "sudo is rejected",
			setup: func(t *testing.T, root string) {
				appendInstallerText(t, root, shellInstaller, "\nsudo echo forbidden\n")
			},
			want: "sudo failure",
		},
		{
			name: "shell checksum requirement is missing",
			setup: func(t *testing.T, root string) {
				replaceInstallerText(t, root, shellInstaller, "--proto '=https'", "--proto")
			},
			want: "shell checksum failure",
		},
		{
			name: "PowerShell checksum requirement is missing",
			setup: func(t *testing.T, root string) {
				replaceInstallerText(t, root, powerShellInstaller, "Get-FileHash", "Get-Hash")
			},
			want: "PowerShell checksum failure",
		},
		{
			name: "shell agent requirement is missing",
			setup: func(t *testing.T, root string) {
				replaceInstallerText(t, root, shellInstaller, "--no-agent-hooks", "--no-agent")
			},
			want: "shell agent requirement failure",
		},
		{
			name: "PowerShell agent requirement is missing",
			setup: func(t *testing.T, root string) {
				replaceInstallerText(t, root, powerShellInstaller, "NoAgentHooks", "NoAgent")
			},
			want: "PowerShell agent requirement failure",
		},
		{
			name: "PowerShell advertises OpenCode plugin installation",
			setup: func(t *testing.T, root string) {
				appendInstallerText(t, root, powerShellInstaller, `Command = "opencode"`+"\n")
			},
			want: "PowerShell OpenCode support failure",
		},
		{
			name: "shell syntax is invalid",
			setup: func(t *testing.T, root string) {
				appendInstallerText(t, root, shellInstaller, "\nif (\n")
			},
			want:      "shell syntax failure",
			posixOnly: true,
			skip:      "POSIX shell syntax check",
		},
		{
			name: "PowerShell syntax probe fails",
			setup: func(t *testing.T, root string) {
				useFakePowerShell(t)
			},
			want: "PowerShell syntax failure",
		},
		{
			name: "JSON manifest is invalid",
			setup: func(t *testing.T, root string) {
				writeInstallerFile(t, root, filepath.Join(".factory-plugin", "marketplace.json"), "{")
			},
			want: "JSON validation failure",
		},
		{
			name: "plugin license is missing",
			setup: func(t *testing.T, root string) {
				removeInstallerFile(t, root, filepath.Join("marketplace", "git-byline", "LICENSE"))
			},
			want: "plugin license failure",
		},
		{
			name:  "root license is missing",
			setup: func(t *testing.T, root string) { removeInstallerFile(t, root, "LICENSE") },
			want:  "root license failure",
		},
		{
			name: "licenses differ",
			setup: func(t *testing.T, root string) {
				appendInstallerText(t, root, "LICENSE", "different\n")
			},
			want: "license mismatch",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.posixOnly && runtime.GOOS == "windows" {
				t.Skip(tc.skip)
			}
			root := installerFixture(t)
			tc.setup(t, root)
			if err := checkInstallers(root); err == nil {
				t.Fatalf("checkInstallers() = nil error, want %s", tc.want)
			}
		})
	}
}

func TestCheckPluginManifestsFailures(t *testing.T) {
	tests := []pluginManifestFailureCase{
		{
			name:          "factory manifest missing",
			check:         checkPluginManifests,
			remove:        pluginFactoryManifest,
			removeMessage: "remove factory manifest: %v",
			failure:       "checkPluginManifests() = nil error, want factory read failure",
		},
		{
			name:          "checkInstallers returns manifest failure",
			check:         checkInstallers,
			remove:        pluginFactoryManifest,
			removeMessage: "remove factory manifest: %v",
			failure:       "checkInstallers() = nil error, want manifest failure",
		},
		{
			name:  "checkInstallers returns manifest mismatch",
			check: checkInstallers,
			replace: &installerTextReplacement{
				rel:         pluginClaudeManifest,
				old:         `"name": "git-byline"`,
				replacement: `"name": "other"`,
			},
			failure: "checkInstallers() = nil error, want manifest mismatch",
		},
		{
			name:          "Claude manifest missing",
			check:         checkPluginManifests,
			remove:        pluginClaudeManifest,
			removeMessage: "remove Claude manifest: %v",
			failure:       "checkPluginManifests() = nil error, want Claude read failure",
		},
		{
			name:          "Gemini manifest missing",
			check:         checkPluginManifests,
			remove:        geminiManifest,
			removeMessage: "remove Gemini manifest: %v",
			failure:       "checkPluginManifests() = nil error, want Gemini read failure",
		},
		{
			name:  "factory and Claude fields differ",
			check: checkPluginManifests,
			replace: &installerTextReplacement{
				rel:         pluginClaudeManifest,
				old:         `"name": "git-byline"`,
				replacement: `"name": "other"`,
			},
			failure: "checkPluginManifests() = nil error, want plugin field mismatch",
		},
		{
			name:  "Gemini fields differ",
			check: checkPluginManifests,
			replace: &installerTextReplacement{
				rel:         geminiManifest,
				old:         `"name": "git-byline"`,
				replacement: `"name": "other"`,
			},
			failure: "checkPluginManifests() = nil error, want Gemini field mismatch",
		},
		{
			name:  "Claude license is not MIT",
			check: checkPluginManifests,
			replace: &installerTextReplacement{
				rel:         pluginClaudeManifest,
				old:         `"license": "MIT"`,
				replacement: `"license": "Apache-2.0"`,
			},
			failure: "checkPluginManifests() = nil error, want license failure",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runPluginManifestFailure(t, tc)
		})
	}
}

func runPluginManifestFailure(t *testing.T, tc pluginManifestFailureCase) {
	root := installerFixture(t)
	if tc.remove != "" {
		removeInstallerFile(t, root, tc.remove, tc.removeMessage)
	} else {
		replaceInstallerText(t, root, tc.replace.rel, tc.replace.old, tc.replace.replacement)
	}
	if err := tc.check(root); err == nil {
		t.Fatal(tc.failure)
	}
}

func TestCheckHarnessTemplatesFailures(t *testing.T) {
	t.Run("generated hook missing", func(t *testing.T) {
		root := installerFixture(t)
		removeHarnessFile(t, root, false)
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want generated hook failure")
		}
	})

	t.Run("hook template missing", func(t *testing.T) {
		root := installerFixture(t)
		removeHarnessFile(t, root, true)
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want hook template failure")
		}
	})

	t.Run("hook template differs", func(t *testing.T) {
		root := installerFixture(t)
		template, generated := firstHarnessPair()
		appendInstallerText(t, root, template, "drift\n")
		_ = generated
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want hook mismatch")
		}
	})

	t.Run("setup command missing", func(t *testing.T) {
		root := installerFixture(t)
		if err := os.Remove(filepath.Join(root, setupCommands[0])); err != nil {
			t.Fatalf("remove setup command: %v", err)
		}
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want setup command read failure")
		}
	})

	t.Run("setup command requirement missing", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, setupCommands[0], "status", "state")
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want setup command requirement failure")
		}
	})

	t.Run("setup command sudo invocation rejected", func(t *testing.T) {
		root := installerFixture(t)
		appendInstallerText(t, root, setupCommands[0], "\nsudo echo forbidden\n")
		if err := checkHarnessTemplates(root); err == nil {
			t.Fatal("checkHarnessTemplates() = nil error, want sudo invocation failure")
		}
	})

	t.Run("PowerShell curl alias rejected", func(t *testing.T) {
		root := installerFixture(t)
		appendInstallerText(t, root, setupCommands[0],
			"\n```powershell\ncurl -fsSL https://example.test -o file\n```\n")
		err := checkHarnessTemplates(root)
		if err == nil || !strings.Contains(err.Error(), "PowerShell curl alias") {
			t.Fatalf("checkHarnessTemplates() = %v, want PowerShell curl alias failure", err)
		}
	})

	t.Run("PowerShell curl.exe accepted", func(t *testing.T) {
		root := installerFixture(t)
		appendInstallerText(t, root, setupCommands[0],
			"\n```powershell\ncurl.exe -fsSL https://example.test -o file\n```\n")
		if err := checkHarnessTemplates(root); err != nil {
			t.Fatalf("checkHarnessTemplates() = %v, want nil", err)
		}
	})

	t.Run("sh curl outside PowerShell accepted", func(t *testing.T) {
		root := installerFixture(t)
		appendInstallerText(t, root, setupCommands[0],
			"\n```sh\ncurl -fsSL https://example.test -o file\n```\n")
		if err := checkHarnessTemplates(root); err != nil {
			t.Fatalf("checkHarnessTemplates() = %v, want nil", err)
		}
	})

	t.Run("OpenCode setup command without WSL guidance rejected", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, openCodeSetupCommand, "WSL", "a VM")
		err := checkHarnessTemplates(root)
		if err == nil || !strings.Contains(err.Error(), "WSL") {
			t.Fatalf("checkHarnessTemplates() = %v, want missing WSL guidance failure", err)
		}
	})

	t.Run("OpenCode setup command without a release pin rejected", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, openCodeSetupCommand, releasePinMarker, "pinned")
		err := checkHarnessTemplates(root)
		requireErrorContaining(t, err, "setup command "+openCodeSetupCommand+": has no release pin line")
	})

	t.Run("OpenCode plugin changed without its setup command rejected", func(t *testing.T) {
		root := installerFixture(t)
		// The template and the generated plugin stay identical, so only the
		// digest in the setup command is stale.
		generated := harnessTemplates[openCodeTemplateRel]
		appendInstallerText(t, root, openCodeTemplateRel, "// changed\n")
		appendInstallerText(t, root, generated, "// changed\n")
		err := checkHarnessTemplates(root)
		requireErrorContaining(t, err, "setup command "+openCodeSetupCommand+": lists SHA-256")
	})
}

// openCodePinFixture returns a root that holds the release manifest and the
// plugin of this repository, and the text of the OpenCode setup command.
func openCodePinFixture(t *testing.T) (string, string) {
	t.Helper()
	source, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	root := t.TempDir()
	copyInstallerFile(t, source, root, ".release-please-manifest.json")
	copyInstallerFile(t, source, root, openCodeTemplateRel)
	setup, err := os.ReadFile(filepath.Join(source, openCodeSetupCommand))
	if err != nil {
		t.Fatalf("read OpenCode setup command: %v", err)
	}
	return root, string(setup)
}

// TestCheckOpenCodePluginPin pins what makes the plugin install safe to run.
// OpenCode loads the plugin as code, so the setup command downloads it from
// the release tag of the manifest and compares the download with the SHA-256
// of the plugin that this repository ships.
func TestCheckOpenCodePluginPin(t *testing.T) {
	t.Parallel()
	root, setup := openCodePinFixture(t)
	if err := checkOpenCodePluginPin(root, setup); err != nil {
		t.Fatalf("checkOpenCodePluginPin(repository) = %v, want nil", err)
	}
	version, err := releaseManifestVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	pin := "v" + version
	digest := sha256DigestPattern.FindString(setup)
	if digest == "" {
		t.Fatal("the OpenCode setup command lists no SHA-256")
	}

	t.Run("CRLF checkout of the plugin", func(t *testing.T) {
		t.Parallel()
		root, setup := openCodePinFixture(t)
		path := filepath.Join(root, openCodeTemplateRel)
		plugin, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		crlf := strings.ReplaceAll(string(plugin), "\n", "\r\n")
		if err := os.WriteFile(path, []byte(crlf), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkOpenCodePluginPin(root, setup); err != nil {
			t.Fatalf("checkOpenCodePluginPin(CRLF plugin) = %v, want nil", err)
		}
	})

	t.Run("CRLF checkout of the setup command", func(t *testing.T) {
		t.Parallel()
		root, setup := openCodePinFixture(t)
		crlf := strings.ReplaceAll(setup, "\n", "\r\n")
		if err := checkOpenCodePluginPin(root, crlf); err != nil {
			t.Fatalf("checkOpenCodePluginPin(CRLF setup command) = %v, want nil", err)
		}
	})

	rejected := []struct {
		name   string
		change func(t *testing.T, root, setup string) string
		want   string
	}{
		{"manifest is missing", func(t *testing.T, root, setup string) string {
			removeInstallerFile(t, root, ".release-please-manifest.json")
			return setup
		}, "read release manifest"},
		{"pin is behind the manifest", func(t *testing.T, root, setup string) string {
			writeInstallerFile(t, root, ".release-please-manifest.json", `{".": "99.0.0"}`+"\n")
			return setup
		}, "release pin is " + pin + ", release manifest is v99.0.0"},
		{"pin of the PowerShell block is behind", func(t *testing.T, root, setup string) string {
			return strings.Replace(setup, `$tag = "`+pin+`"`, `$tag = "v0.0.1"`, 1)
		}, "release pin is v0.0.1"},
		{"no pin line", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, releasePinMarker, "pinned")
		}, "has no release pin line"},
		{"pin line without a tag", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "tag="+pin+" #", "tag=latest #")
		}, "must hold one release tag, found 0"},
		{"pin line with two tags", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "tag="+pin+" #", "tag="+pin+" "+pin+" #")
		}, "must hold one release tag, found 2"},
		{"plugin from main", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "/git-byline/$tag/marketplace", "/git-byline/main/marketplace")
		}, "downloads the plugin from main"},
		{"plugin from main through github.com", func(t *testing.T, root, setup string) string {
			return setup + "\nhttps://github.com/comarch/git-byline/raw/main/marketplace/harness/opencode/promptscript.ts\n"
		}, "downloads the plugin from main"},
		{"plugin from a branch ref", func(t *testing.T, root, setup string) string {
			return setup + "\nhttps://raw.githubusercontent.com/comarch/git-byline/refs/heads/main/marketplace/harness/opencode/promptscript.ts\n"
		}, "downloads the plugin from refs/heads/main"},
		{"plugin from a blob page", func(t *testing.T, root, setup string) string {
			return setup + "\nhttps://github.com/comarch/git-byline/blob/main/marketplace/harness/opencode/promptscript.ts\n"
		}, "downloads the plugin from blob/main"},
		{"plugin is not downloaded", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "harness/opencode/promptscript.ts", "harness/opencode/plugin.ts")
		}, "does not download the plugin from the release pin"},
		{"no sha256sum", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "sha256sum", "sha-256-sum")
		}, "does not verify the plugin with sha256sum"},
		{"no shasum", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "shasum -a 256", "shasum -a 512")
		}, "does not verify the plugin with shasum -a 256"},
		{"no Get-FileHash", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, "Get-FileHash", "Get-Item")
		}, "does not verify the plugin with Get-FileHash"},
		{"plugin is missing", func(t *testing.T, root, setup string) string {
			removeInstallerFile(t, root, openCodeTemplateRel)
			return setup
		}, "read OpenCode plugin"},
		{"plugin changed", func(t *testing.T, root, setup string) string {
			appendInstallerText(t, root, openCodeTemplateRel, "// changed\n")
			return setup
		}, "lists SHA-256 " + digest},
		{"no digest", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, digest, "")
		}, "lists no lowercase SHA-256"},
		// The sh block compares the digest as text, and sha256sum prints
		// lowercase.
		{"uppercase digest", func(t *testing.T, root, setup string) string {
			return strings.ReplaceAll(setup, digest, strings.ToUpper(digest))
		}, "lists no lowercase SHA-256"},
		{"digest of the PowerShell block is stale", func(t *testing.T, root, setup string) string {
			at := strings.LastIndex(setup, digest)
			return setup[:at] + strings.Repeat("0", len(digest)) + setup[at+len(digest):]
		}, "lists SHA-256 " + strings.Repeat("0", 64)},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, setup := openCodePinFixture(t)
			err := checkOpenCodePluginPin(root, tt.change(t, root, setup))
			requireErrorContaining(t, err, tt.want)
		})
	}
}

func TestJSONObjectReadFailures(t *testing.T) {
	t.Run("read missing object", func(t *testing.T) {
		if _, err := readJSONObject(filepath.Join(t.TempDir(), "missing.json")); err == nil {
			t.Fatal("readJSONObject() = nil error, want read failure")
		}
	})

	t.Run("decode invalid object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.json")
		writeInstallerFile(t, filepath.Dir(path), filepath.Base(path), "{")
		if _, err := readJSONObject(path); err == nil {
			t.Fatal("readJSONObject() = nil error, want decode failure")
		}
	})

	t.Run("validate missing object", func(t *testing.T) {
		if err := validateJSONObject(filepath.Join(t.TempDir(), "missing.json")); err == nil {
			t.Fatal("validateJSONObject() = nil error, want read failure")
		}
	})

	t.Run("validate invalid object", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
			t.Fatalf("write invalid JSON: %v", err)
		}
		if err := validateJSONObject(path); err == nil {
			t.Fatal("validateJSONObject() = nil error, want decode failure")
		}
	})

	t.Run("validate second object is malformed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "trailing.json")
		if err := os.WriteFile(path, []byte("{} {"), 0o600); err != nil {
			t.Fatalf("write malformed trailing JSON: %v", err)
		}
		if err := validateJSONObject(path); err == nil {
			t.Fatal("validateJSONObject() = nil error, want trailing decode failure")
		}
	})
}

func installerFixture(t *testing.T) string {
	t.Helper()
	source, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	root := t.TempDir()
	files := []string{
		shellInstaller,
		powerShellInstaller,
		".release-please-manifest.json",
		"LICENSE",
		"marketplace/git-byline/LICENSE",
		".factory-plugin/marketplace.json",
		".claude-plugin/marketplace.json",
		"marketplace/git-byline/.factory-plugin/plugin.json",
		pluginClaudeManifest,
		geminiManifest,
	}
	for template, generated := range harnessTemplates {
		files = append(files, template, generated)
	}
	files = append(files, setupCommands...)
	for _, rel := range files {
		copyInstallerFile(t, source, root, rel)
	}
	return root
}

func copyInstallerFile(t *testing.T, source, root, rel string) {
	t.Helper()
	src := filepath.Join(source, filepath.FromSlash(rel))
	dst := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat fixture %s: %v", rel, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("create fixture parent %s: %v", rel, err)
	}
	if err := os.WriteFile(dst, data, info.Mode().Perm()); err != nil {
		t.Fatalf("write fixture %s: %v", rel, err)
	}
}

func writeInstallerFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create parent for %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write installer fixture %s: %v", rel, err)
	}
}

func removeInstallerFile(t *testing.T, root, rel string, failureMessage ...string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
		if len(failureMessage) > 0 {
			t.Fatalf(failureMessage[0], err)
		}
		t.Fatalf("remove installer fixture %s: %v", rel, err)
	}
}

func appendInstallerText(t *testing.T, root, rel, suffix string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installer fixture %s: %v", rel, err)
	}
	if err := os.WriteFile(path, append(data, suffix...), 0o755); err != nil {
		t.Fatalf("append installer fixture %s: %v", rel, err)
	}
}

func replaceInstallerText(t *testing.T, root, rel, old, replacement string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installer fixture %s: %v", rel, err)
	}
	text := string(data)
	if !strings.Contains(text, old) {
		t.Fatalf("installer fixture %s missing %q", rel, old)
	}
	text = strings.ReplaceAll(text, old, replacement)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat installer fixture %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(text), info.Mode().Perm()); err != nil {
		t.Fatalf("rewrite installer fixture %s: %v", rel, err)
	}
}

func removeHarnessFile(t *testing.T, root string, template bool) {
	t.Helper()
	templatePath, generatedPath := firstHarnessPair()
	rel := generatedPath
	if template {
		rel = templatePath
	}
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatalf("remove harness file %s: %v", rel, err)
	}
}

func firstHarnessPair() (string, string) {
	for template, generated := range harnessTemplates {
		return template, generated
	}
	panic("harnessTemplates is empty")
}

func useFakePowerShell(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	writeExecutableTestFile(t, filepath.Join(dir, "pwsh"), "#!/bin/sh\nprintf '%s\\n' 'PowerShell syntax failed' >&2\nexit 1\n")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
