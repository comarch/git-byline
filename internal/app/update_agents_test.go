package app

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// installShellAgent matches one per-project agent entry of install.sh: the
// detection command and config directory, then the pending entry
// name:source directory:project file:source file.
var installShellAgent = regexp.MustCompile(
	`detected (\S+) (\S+)\s*&&\s*pending="\$pending ([^:"\s]+):([^:"\s]+):([^:"\s]+):([^:"\s]+)"`,
)

// installPowerShellAgent matches one per-project agent entry of install.ps1.
var installPowerShellAgent = regexp.MustCompile(
	`Name = "([^"]+)"; Command = "([^"]+)"; Directory = "([^"]+)"; Source = "([^"]+)"; ` +
		`Hook = "([^"]+)"; SourceFile = "([^"]+)"`,
)

// openCodeShellConfigDir is how install.sh spells the directory that
// openCodeConfigDir resolves.
const openCodeShellConfigDir = `"${XDG_CONFIG_HOME:-$HOME/.config}/opencode"`

func TestDetectManualAgentsSkipsOpenCodeOnWindows(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	always := func(string, string) bool { return true }
	tests := []struct {
		goos         string
		wantOpenCode bool
	}{
		{goos: "linux", wantOpenCode: true},
		{goos: "darwin", wantOpenCode: true},
		{goos: "windows", wantOpenCode: false},
	}
	for _, test := range tests {
		t.Run(test.goos, func(t *testing.T) {
			found := detectManualAgents(test.goos, always)
			hasOpenCode := slices.ContainsFunc(found, func(agent manualAgent) bool {
				return agent.name == openCodeAgent
			})
			if hasOpenCode != test.wantOpenCode {
				t.Fatalf("OpenCode found = %t on %s, want %t", hasOpenCode, test.goos, test.wantOpenCode)
			}
			want := len(manualAgents)
			if !test.wantOpenCode {
				want--
			}
			if len(found) != want {
				t.Fatalf("found %d agents on %s, want %d", len(found), test.goos, want)
			}
		})
	}
}

func TestDetectManualAgentsReportsOnlyDetectedAgents(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	found := detectManualAgents("linux", func(command, _ string) bool {
		return command == "gemini" || command == openCodeAgent
	})
	var names []string
	for _, agent := range found {
		names = append(names, agent.name)
	}
	if want := []string{"gemini", openCodeAgent}; !slices.Equal(names, want) {
		t.Fatalf("found agents = %v, want %v", names, want)
	}
}

func TestDetectManualAgentsPassesConfigDirs(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	seen := map[string]string{}
	detectManualAgents("linux", func(command, configDir string) bool {
		seen[command] = configDir
		return false
	})
	if got, want := seen[openCodeAgent], filepath.Join(xdg, "opencode"); got != want {
		t.Errorf("OpenCode config dir = %q, want %q", got, want)
	}
	if got, want := seen["gemini"], ".gemini"; got != want {
		t.Errorf("Gemini config dir = %q, want %q", got, want)
	}
}

func TestOpenCodeConfigDir(t *testing.T) {
	home := t.TempDir()
	xdg := t.TempDir()
	tests := []struct {
		name string
		home string
		xdg  string
		want string
	}{
		{name: "XDG config home", home: home, xdg: xdg, want: filepath.Join(xdg, "opencode")},
		{name: "config directory in home", home: home, want: filepath.Join(home, ".config", "opencode")},
		{name: "unknown home directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.
			t.Setenv("HOME", test.home)
			t.Setenv("USERPROFILE", test.home)
			t.Setenv("XDG_CONFIG_HOME", test.xdg)
			if got := openCodeConfigDir(); got != test.want {
				t.Fatalf("openCodeConfigDir() = %q, want %q", got, test.want)
			}
		})
	}
}

// TestManualAgentsMatchInstallers keeps the update copy commands in lockstep
// with the agents the installers detect, because a user gets the same advice
// from either path. install.ps1 leaves OpenCode out on purpose: the plugin is
// Unix-only.
func TestManualAgentsMatchInstallers(t *testing.T) {
	t.Run("install.sh", func(t *testing.T) {
		script := readRepositoryFile(t, "install.sh")
		if got := strings.Count(script, `pending="$pending `); got != len(manualAgents) {
			t.Fatalf("install.sh queues %d agents, want %d", got, len(manualAgents))
		}
		matches := installShellAgent.FindAllStringSubmatch(script, -1)
		if len(matches) != len(manualAgents) {
			t.Fatalf("install.sh has %d parsable agents, want %d", len(matches), len(manualAgents))
		}
		for i, agent := range manualAgents {
			command, configDir, name, source, hook, sourceFile := matches[i][1], matches[i][2], matches[i][3],
				matches[i][4], matches[i][5], matches[i][6]
			wantConfigDir := agent.configDir
			if agent.name == openCodeAgent {
				wantConfigDir = openCodeShellConfigDir
			}
			if command != agent.command || configDir != wantConfigDir || name != agent.name ||
				source != agent.name || hook != agent.hookPath || sourceFile != agent.sourceFile {
				t.Errorf("install.sh agent %d = %v, want %+v", i, matches[i][1:], agent)
			}
		}
	})

	t.Run("install.ps1", func(t *testing.T) {
		script := readRepositoryFile(t, "install.ps1")
		var want []manualAgent
		for _, agent := range manualAgents {
			if agent.name != openCodeAgent {
				want = append(want, agent)
			}
		}
		matches := installPowerShellAgent.FindAllStringSubmatch(script, -1)
		if len(matches) != len(want) {
			t.Fatalf("install.ps1 has %d parsable agents, want %d", len(matches), len(want))
		}
		for i, agent := range want {
			name, command, directory, source, hook, sourceFile := matches[i][1], matches[i][2], matches[i][3],
				matches[i][4], matches[i][5], matches[i][6]
			if name != agent.name || command != agent.command || directory != agent.configDir ||
				source != agent.name || hook != agent.hookPath || sourceFile != agent.sourceFile {
				t.Errorf("install.ps1 agent %d = %v, want %+v", i, matches[i][1:], agent)
			}
		}
	})
}

// readRepositoryFile reads a file of the repository, which is two levels
// above this package.
func readRepositoryFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
