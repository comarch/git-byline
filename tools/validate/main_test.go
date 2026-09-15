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

// TestSelectStages pins the -stages filter: the pipeline order must be
// preserved, and unknown or empty names must fail so typos cannot
// silently skip stages.
func TestSelectStages(t *testing.T) {
	t.Parallel()
	pipeline := []stage{
		{"gofmt", func(string) error { return nil }},
		{"vet", func(string) error { return nil }},
		{"coverage", func(string) error { return nil }},
	}
	cases := []struct {
		name    string
		spec    string
		want    []string
		wantErr bool
	}{
		{name: "empty spec keeps all stages", spec: "", want: []string{"gofmt", "vet", "coverage"}},
		{name: "subset keeps pipeline order", spec: "coverage,gofmt", want: []string{"gofmt", "coverage"}},
		{name: "whitespace is trimmed", spec: " gofmt , vet ", want: []string{"gofmt", "vet"}},
		{name: "duplicate names collapse", spec: "vet,vet", want: []string{"vet"}},
		{name: "unknown stage", spec: "no-such-stage", wantErr: true},
		{name: "empty stage name", spec: "gofmt,,vet", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected, err := selectStages(pipeline, tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("selectStages(%q) = nil error, want failure", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("selectStages(%q) = %v", tc.spec, err)
			}
			var names []string
			for _, s := range selected {
				names = append(names, s.name)
			}
			if strings.Join(names, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("selectStages(%q) = %v, want %v", tc.spec, names, tc.want)
			}
		})
	}
}
