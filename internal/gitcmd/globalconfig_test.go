package gitcmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGlobalConfigRoundTrip exercises the user-level configuration helpers
// against an isolated global configuration file.
func TestGlobalConfigRoundTrip(t *testing.T) {
	xdg := setupGlobalConfig(t)

	if value, exists, err := GlobalConfig("init.templateDir"); err != nil || exists || value != "" {
		t.Fatalf("missing key = %q, %v, %v", value, exists, err)
	}
	if err := SetGlobalConfig("init.templateDir", filepath.Join(xdg, "tpl")); err != nil {
		t.Fatal(err)
	}
	if value, exists, err := GlobalConfig("init.templateDir"); err != nil || !exists ||
		value != filepath.Join(xdg, "tpl") {
		t.Fatalf("set key = %q, %v, %v", value, exists, err)
	}
	if err := SetGlobalConfig("init.templateDir", filepath.Join(xdg, "other")); err != nil {
		t.Fatal(err)
	}
	if value, _, err := GlobalConfig("init.templateDir"); err != nil || value != filepath.Join(xdg, "other") {
		t.Fatalf("replaced key = %q, %v", value, err)
	}
}

func TestUnsetGlobalConfigDuplicate(t *testing.T) {
	xdg := setupGlobalConfig(t)
	if err := SetGlobalConfig("init.templateDir", filepath.Join(xdg, "other")); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(xdg, "managed")
	if _, err := runGitOutsideRepo(
		"add global git config",
		"config", globalConfig, "--add", "init.templateDir", managed,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := runGitOutsideRepo(
		"add global git config",
		"config", globalConfig, "--add", "init.templateDir", managed,
	); err != nil {
		t.Fatal(err)
	}
	existed, err := UnsetGlobalConfig("init.templateDir", managed)
	if err != nil || !existed {
		t.Fatalf("unset = %v, %v", existed, err)
	}
	if value, exists, err := GlobalConfig("init.templateDir"); err != nil || !exists ||
		value != filepath.Join(xdg, "other") {
		t.Fatalf("foreign value = %q, %v, %v", value, exists, err)
	}
	if existed, err := UnsetGlobalConfig("init.templateDir", managed); err != nil || existed {
		t.Fatalf("unset again = %v, %v", existed, err)
	}
	if existed, err := UnsetGlobalConfig("init.templateDir", filepath.Join(xdg, "other")); err != nil || !existed {
		t.Fatalf("unset foreign = %v, %v", existed, err)
	}
	if _, exists, err := GlobalConfig("init.templateDir"); err != nil || exists {
		t.Fatalf("removed key = %v, %v", exists, err)
	}
}

func setupGlobalConfig(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return xdg
}

func TestGlobalConfigReadsIncludes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	gitDir := filepath.Join(xdg, "git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	included := filepath.Join(xdg, "included")
	value := filepath.Join(xdg, "included-template")
	if err := os.WriteFile(
		included,
		[]byte("[init]\n\ttemplateDir = "+value+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(gitDir, "config"),
		[]byte("[include]\n\tpath = "+included+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if current, exists, err := GlobalConfig("init.templateDir"); err != nil || !exists || current != value {
		t.Fatalf("included value = %q, %v, %v", current, exists, err)
	}
	if removed, err := UnsetGlobalConfig("init.templateDir", value); err == nil || removed {
		t.Fatalf("removed included value = %v, %v", removed, err)
	}
	if err := AddGlobalConfig("init.templateDir", value); err != nil {
		t.Fatal(err)
	}
	if removed, err := UnsetGlobalConfig("init.templateDir", value); err == nil || !removed {
		t.Fatalf("missed included duplicate = %v, %v", removed, err)
	}
}
