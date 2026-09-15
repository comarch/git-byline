package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeProfileFile writes a raw profile for test setup.
func writeProfileFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// readProfileFile reads a profile back for assertions.
func readProfileFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRunMergesProfiles(t *testing.T) {
	dir := t.TempDir()
	unit := writeProfileFile(t, dir, "unit.out", "mode: count\nmain.go:1.2,2.10 1 0\n")
	child := writeProfileFile(t, dir, "child.out", "mode: count\nmain.go:1.2,2.10 1 3\n")
	out := filepath.Join(dir, "merged.out")

	if code, err := run(out, []string{unit, child}); err != nil || code != 0 {
		t.Fatalf("run() = %d, %v; want 0, nil", code, err)
	}
	if got := readProfileFile(t, out); !strings.Contains(got, "main.go:1.2,2.10 1 3\n") {
		t.Fatalf("merged profile = %q, want summed counts", got)
	}
}

func TestRunUsageErrors(t *testing.T) {
	dir := t.TempDir()
	profile := writeProfileFile(t, dir, "unit.out", "mode: count\nmain.go:1.2,2.10 1 1\n")
	cases := []struct {
		name   string
		out    string
		inputs []string
	}{
		{name: "missing output", out: "", inputs: []string{profile}},
		{name: "no inputs", out: filepath.Join(dir, "merged.out")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if code, err := run(tc.out, tc.inputs); err == nil || code != 2 {
				t.Fatalf("run() = %d, %v; want 2, error", code, err)
			}
		})
	}
}

func TestRunOperationalFailures(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "merged.out")
	missing := writeProfileFile(t, t.TempDir(), "missing.out", "")
	missingInputs := []string{filepath.Join(dir, "gone.out")}
	if code, err := run(out, missingInputs); err == nil || code != 1 {
		t.Fatalf("run(missing input) = %d, %v; want 1, error", code, err)
	}
	empty := []string{missing}
	if code, err := run(out, empty); err == nil || code != 1 {
		t.Fatalf("run(empty profile) = %d, %v; want 1, error", code, err)
	}
	unwritable := filepath.Join(dir, "no-such-dir", "merged.out")
	profile := writeProfileFile(t, dir, "unit.out", "mode: count\nmain.go:1.2,2.10 1 1\n")
	if code, err := run(unwritable, []string{profile}); err == nil || code != 1 {
		t.Fatalf("run(unwritable output) = %d, %v; want 1, error", code, err)
	}
}
