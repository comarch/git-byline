package app

import (
	"path/filepath"
	"slices"
	"testing"
)

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
