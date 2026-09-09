package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunPipelineStopsAtFirstFailure(t *testing.T) {
	t.Parallel()
	var called []string
	stages := []stage{
		{"one", func(string) error { called = append(called, "one"); return nil }},
		{"two", func(string) error { called = append(called, "two"); return errors.New("boom") }},
		{"three", func(string) error { called = append(called, "three"); return nil }},
	}
	var stdout, stderr bytes.Buffer
	code := runPipeline(&stdout, &stderr, stages, "root")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
	if strings.Join(called, ",") != "one,two" {
		t.Errorf("stages called = %v, want [one two]", called)
	}
	if !strings.Contains(stderr.String(), "FAIL two") {
		t.Errorf("stderr = %q, want it to contain the failing stage", stderr.String())
	}
}

func TestRunPipelineAllStagesPass(t *testing.T) {
	t.Parallel()
	stages := []stage{
		{"one", func(string) error { return nil }},
		{"two", func(string) error { return nil }},
	}
	var stdout, stderr bytes.Buffer
	code := runPipeline(&stdout, &stderr, stages, "root")
	if code != 0 {
		t.Errorf("code = %d, want 0 (stderr: %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "all 2 stages passed") {
		t.Errorf("stdout = %q, want it to contain the summary", stdout.String())
	}
}

// TestPipelineStages pins the pipeline composition and order: stages
// stop at the first failure, so the order is the cheap checks first and
// the whole-suite runs before the release builds.
func TestPipelineStages(t *testing.T) {
	t.Parallel()
	want := []string{
		"gofmt", "vet", "test", "coverage", "build",
		"deps", "imports", "installers", "promptscript", "scans",
	}
	stages := pipelineStages()
	if len(stages) != len(want) {
		t.Fatalf("pipelineStages() has %d stages, want %d", len(stages), len(want))
	}
	for i, s := range stages {
		if s.name != want[i] {
			t.Errorf("stage %d = %q, want %q", i, s.name, want[i])
		}
	}
}

func TestRepoRoot(t *testing.T) {
	t.Parallel()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("repository root %s has no go.mod: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "validate")); err != nil {
		t.Errorf("repository root %s has no tools/validate: %v", root, err)
	}
}
