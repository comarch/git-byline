package gitcmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGlobalConfigRoundTrip exercises the user-level configuration helpers
// against an isolated global configuration file.
func TestGlobalConfigRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if err := os.MkdirAll(filepath.Join(xdg, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "git", "config"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

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
	existed, err := UnsetGlobalConfig("init.templateDir")
	if err != nil || !existed {
		t.Fatalf("unset = %v, %v", existed, err)
	}
	if existed, err := UnsetGlobalConfig("init.templateDir"); err != nil || existed {
		t.Fatalf("unset again = %v, %v", existed, err)
	}
	if _, exists, err := GlobalConfig("init.templateDir"); err != nil || exists {
		t.Fatalf("removed key = %v, %v", exists, err)
	}
}
