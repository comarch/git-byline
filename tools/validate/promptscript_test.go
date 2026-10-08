package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// agentToolsPattern captures every agent of agents.prs with its tools list.
var agentToolsPattern = regexp.MustCompile(
	`(?m)^  ([a-z-]+): \{\r?\n    description: [^\r\n]*\r?\n    tools: (\[[^\]\r\n]*\])\r?$`,
)

func TestParseSemver(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"1.19.1", "1.19.1"},
		{"promptscript 1.19.1 darwin-arm64 node/v24.0.0", "1.19.1"},
		{"v2.0.0-rc.1", "2.0.0"},
		{"no version here", ""},
	}
	for _, tt := range tests {
		if got := parseSemver(tt.in); got != tt.want {
			t.Errorf("parseSemver(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWindsurfGeneratedHooksAvoidDuplicateShellWiring(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".windsurf", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks map[string][]struct {
			Command string `json:"command"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	for event, hooks := range config.Hooks {
		if len(hooks) != 1 {
			t.Fatalf("%s has %d generated hooks, want one", event, len(hooks))
		}
		if strings.Contains(hooks[0].Command, "windsurf-shell-") {
			t.Fatalf("%s contains a duplicate shell hook: %q", event, hooks[0].Command)
		}
	}
}

// TestCheckPromptScriptOnRepo runs the full PromptScript stage against the
// real repository: pinned CLI, strict validation, and generated-file
// drift. It skips on machines without the CLI so the test suite stays
// green for contributors who have not installed it.
func TestCheckPromptScriptOnRepo(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if err := checkPromptScript(root); err != nil {
		t.Errorf("checkPromptScript(repo) = %v, want nil", err)
	}
}

func TestCheckPromptScriptMissingFiles(t *testing.T) {
	t.Parallel()
	err := checkPromptScript(t.TempDir())
	if err == nil {
		t.Fatal("checkPromptScript(empty dir) = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v, want it to name the missing file", err)
	}
}

// TestCheckDriftDetectsMutation proves a modified source that no longer
// matches committed generated instructions fails the drift check.
func TestCheckDriftDetectsMutation(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	bin, err := exec.LookPath("promptscript")
	if err != nil {
		t.Fatalf("look up promptscript: %v", err)
	}

	dir := t.TempDir()
	src := filepath.Join(root, ".promptscript", "project.prs")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read project.prs: %v", err)
	}
	mutated := strings.Replace(string(data), "local AI code attribution tool",
		"mutated AI code attribution tool", 1)
	if mutated == string(data) {
		t.Fatal("mutation target text not found in project.prs")
	}
	srcDir := filepath.Join(dir, ".promptscript")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("create %s: %v", srcDir, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".promptscript"))
	if err != nil {
		t.Fatalf("read PromptScript sources: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "project.prs" || filepath.Ext(entry.Name()) != ".prs" {
			continue
		}
		fragment, err := os.ReadFile(filepath.Join(root, ".promptscript", entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(srcDir, entry.Name()), fragment, 0o644); err != nil {
			t.Fatalf("write %s: %v", entry.Name(), err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "project.prs"), []byte(mutated), 0o644); err != nil {
		t.Fatalf("write mutated project.prs: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(root, "promptscript.yaml"))
	if err != nil {
		t.Fatalf("read promptscript.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "promptscript.yaml"), cfg, 0o644); err != nil {
		t.Fatalf("write promptscript.yaml: %v", err)
	}
	for _, rel := range promptScriptOutputs {
		committed, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, committed, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	err = checkDrift(bin, dir)
	if err == nil {
		t.Fatal("checkDrift(mutated source) = nil, want drift error")
	}
	if !strings.Contains(err.Error(), "out of sync") {
		t.Errorf("error = %v, want it to report the drift", err)
	}
}

func TestCheckDriftMissingGeneratedFile(t *testing.T) {
	if _, err := exec.LookPath("promptscript"); err != nil {
		t.Skip("promptscript CLI not available")
	}
	bin, err := exec.LookPath("promptscript")
	if err != nil {
		t.Fatalf("look up promptscript: %v", err)
	}
	dir := t.TempDir()
	err = checkDrift(bin, dir)
	if err == nil {
		t.Fatal("checkDrift(empty dir) = nil, want error")
	}
}

// TestOpenCodeAgentsMatchTheirPromptScriptSource pins what the read-only
// permission block assumes. PromptScript drops the tools list of an agent
// when it writes an OpenCode subagent, so the patch grants Read, Grep, and
// Glob to every subagent. A source agent with other tools, or a subagent the
// patch does not know, would silently get the wrong permissions.
func TestOpenCodeAgentsMatchTheirPromptScriptSource(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	source, err := os.ReadFile(filepath.Join(root, ".promptscript", "agents.prs"))
	if err != nil {
		t.Fatal(err)
	}
	var sourceNames []string
	for _, agent := range agentToolsPattern.FindAllStringSubmatch(string(source), -1) {
		sourceNames = append(sourceNames, agent[1])
		if want := `["Read", "Grep", "Glob"]`; agent[2] != want {
			t.Errorf("agent %s lists tools %s, but the OpenCode permission block grants %s", agent[1], agent[2], want)
		}
	}
	patched := slices.Clone(openCodeAgentNames)
	slices.Sort(sourceNames)
	slices.Sort(patched)
	if !slices.Equal(sourceNames, patched) {
		t.Fatalf("agents.prs defines %v, but the OpenCode patch covers %v", sourceNames, patched)
	}

	entries, err := os.ReadDir(filepath.Join(root, openCodeDir, "agents"))
	if err != nil {
		t.Fatal(err)
	}
	var committed []string
	for _, entry := range entries {
		committed = append(committed, strings.TrimSuffix(entry.Name(), ".md"))
	}
	slices.Sort(committed)
	if !slices.Equal(committed, patched) {
		t.Fatalf("%s holds %v, want %v", filepath.Join(openCodeDir, "agents"), committed, patched)
	}
	for _, name := range patched {
		agent, err := os.ReadFile(filepath.Join(root, openCodeAgentRel(name)))
		if err != nil {
			t.Fatal(err)
		}
		// Windows checkouts may convert line endings.
		if !strings.Contains(strings.ReplaceAll(string(agent), "\r\n", "\n"), openCodeAgentReadOnly) {
			t.Errorf("%s lacks the read-only permission block", openCodeAgentRel(name))
		}
	}
}
