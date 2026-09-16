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
	t.Run("factory manifest missing", func(t *testing.T) {
		root := installerFixture(t)
		if err := os.Remove(filepath.Join(root, pluginFactoryManifest)); err != nil {
			t.Fatalf("remove factory manifest: %v", err)
		}
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want factory read failure")
		}
	})

	t.Run("checkInstallers returns manifest failure", func(t *testing.T) {
		root := installerFixture(t)
		if err := os.Remove(filepath.Join(root, pluginFactoryManifest)); err != nil {
			t.Fatalf("remove factory manifest: %v", err)
		}
		if err := checkInstallers(root); err == nil {
			t.Fatal("checkInstallers() = nil error, want manifest failure")
		}
	})

	t.Run("checkInstallers returns manifest mismatch", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, pluginClaudeManifest, `"name": "git-byline"`, `"name": "other"`)
		if err := checkInstallers(root); err == nil {
			t.Fatal("checkInstallers() = nil error, want manifest mismatch")
		}
	})

	t.Run("Claude manifest missing", func(t *testing.T) {
		root := installerFixture(t)
		if err := os.Remove(filepath.Join(root, pluginClaudeManifest)); err != nil {
			t.Fatalf("remove Claude manifest: %v", err)
		}
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want Claude read failure")
		}
	})

	t.Run("Gemini manifest missing", func(t *testing.T) {
		root := installerFixture(t)
		if err := os.Remove(filepath.Join(root, geminiManifest)); err != nil {
			t.Fatalf("remove Gemini manifest: %v", err)
		}
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want Gemini read failure")
		}
	})

	t.Run("factory and Claude fields differ", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, pluginClaudeManifest, `"name": "git-byline"`, `"name": "other"`)
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want plugin field mismatch")
		}
	})

	t.Run("Gemini fields differ", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, geminiManifest, `"name": "git-byline"`, `"name": "other"`)
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want Gemini field mismatch")
		}
	})

	t.Run("Claude license is not MIT", func(t *testing.T) {
		root := installerFixture(t)
		replaceInstallerText(t, root, pluginClaudeManifest, `"license": "MIT"`, `"license": "Apache-2.0"`)
		if err := checkPluginManifests(root); err == nil {
			t.Fatal("checkPluginManifests() = nil error, want license failure")
		}
	})
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

func removeInstallerFile(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
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

func replaceInstallerText(t *testing.T, root, rel, old, new string) {
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
	text = strings.ReplaceAll(text, old, new)
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
