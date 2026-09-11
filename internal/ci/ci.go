// Package ci reconstructs attribution after forge merge operations.
package ci

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/provenance"
	"github.com/comarch/git-byline/internal/rewrite"
)

// Provider identifies a supported forge workflow provider.
type Provider string

const (
	// ProviderGitHub selects GitHub Actions.
	ProviderGitHub Provider = "github"
	// ProviderGitLab selects GitLab CI.
	ProviderGitLab Provider = "gitlab"
)

const (
	maxMergeCommits = 10_000
	ciBaseEnv       = "GIT_BYLINE_CI_BASE"
	ciSourceEnv     = "GIT_BYLINE_CI_SOURCE"
	ciTargetEnv     = "GIT_BYLINE_CI_TARGET"
	ciModeEnv       = "GIT_BYLINE_CI_MODE"
)

// InstallResult reports the workflow path and whether it was created.
type InstallResult struct {
	Path    string
	Changed bool
}

// RunOptions identifies the local forge merge graph.
//
// Empty fields are read from the GIT_BYLINE_CI_* environment variables. The
// target defaults to HEAD so local tests can omit that value.
type RunOptions struct {
	Base   string
	Source string
	Target string
	Mode   string
}

// RunResult reports local reconstruction and its non-fatal skips.
type RunResult struct {
	Provider      Provider `json:"provider"`
	Mode          string   `json:"mode"`
	Base          string   `json:"base"`
	Source        string   `json:"source"`
	Target        string   `json:"target"`
	SourceCommits int      `json:"source_commits"`
	TargetCommits int      `json:"target_commits"`
	Mapped        int      `json:"mapped"`
	Written       int      `json:"written"`
	Warnings      []string `json:"warnings,omitempty"`
}

// ParseProvider validates a forge provider name.
func ParseProvider(value string) (Provider, error) {
	switch Provider(value) {
	case ProviderGitHub, ProviderGitLab:
		return Provider(value), nil
	default:
		return "", fmt.Errorf("unsupported CI provider %q", value)
	}
}

// TemplatePath returns the repository-relative install path for a provider.
func TemplatePath(provider Provider) (string, error) {
	switch provider {
	case ProviderGitHub:
		return ".github/workflows/git-byline.yml", nil
	case ProviderGitLab:
		return ".gitlab/ci/git-byline.yml", nil
	default:
		return "", fmt.Errorf("unsupported CI provider %q", provider)
	}
}

// Template returns a copy of the canonical workflow template.
func Template(provider Provider) ([]byte, error) {
	return embeddedTemplate(provider)
}

// Install writes a provider workflow without replacing an existing file.
func Install(root string, provider Provider) (InstallResult, error) {
	template, err := Template(provider)
	if err != nil {
		return InstallResult{}, err
	}
	relative, err := TemplatePath(provider)
	if err != nil {
		return InstallResult{}, err
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve CI install root: %w", err)
	}
	rootInfo, err := os.Stat(absoluteRoot)
	if err != nil {
		return InstallResult{}, fmt.Errorf("stat CI install root: %w", err)
	}
	if !rootInfo.IsDir() {
		return InstallResult{}, errors.New("CI install root is not a directory")
	}
	path := filepath.Join(absoluteRoot, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create CI workflow directory: %w", err)
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return InstallResult{}, fmt.Errorf("refusing symlinked CI workflow %q", relative)
		}
		if !info.Mode().IsRegular() {
			return InstallResult{}, fmt.Errorf("CI workflow path %q is not a regular file", relative)
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return InstallResult{}, fmt.Errorf("read existing CI workflow: %w", readErr)
		}
		if !bytes.Equal(existing, template) {
			return InstallResult{}, fmt.Errorf("refusing to replace existing CI workflow %q", relative)
		}
		return InstallResult{Path: relative}, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return InstallResult{}, fmt.Errorf("inspect CI workflow path: %w", statErr)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return InstallResult{}, fmt.Errorf("create CI workflow: %w", err)
	}
	success := false
	defer func() {
		_ = file.Close()
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(template); err != nil {
		return InstallResult{}, fmt.Errorf("write CI workflow: %w", err)
	}
	if err := file.Sync(); err != nil {
		return InstallResult{}, fmt.Errorf("sync CI workflow: %w", err)
	}
	if err := file.Close(); err != nil {
		return InstallResult{}, fmt.Errorf("close CI workflow: %w", err)
	}
	success = true
	return InstallResult{Path: relative, Changed: true}, nil
}

// Run reconstructs attribution locally from fetched forge commits and notes.
func Run(repo *gitcmd.Repo, provider Provider, options RunOptions) (RunResult, error) {
	if repo == nil {
		return RunResult{}, errors.New("CI repository is nil")
	}
	if _, err := ParseProvider(string(provider)); err != nil {
		return RunResult{}, err
	}
	options = resolveOptions(options)
	target, err := resolveTarget(repo, options.Target)
	if err != nil {
		return RunResult{}, err
	}
	if options.Source == "" {
		return RunResult{}, fmt.Errorf("%s is required", ciSourceEnv)
	}
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return RunResult{}, fmt.Errorf("read repository object format: %w", err)
	}
	for _, value := range []struct {
		name string
		id   string
	}{
		{"source", options.Source},
		{"target", target},
	} {
		if err := rewrite.ValidateFullObjectID(value.id, objectIDLength, false); err != nil {
			return RunResult{}, fmt.Errorf("validate CI %s commit: %w", value.name, err)
		}
	}
	if options.Base == "" {
		options.Base, err = repo.MergeBase(options.Source, target)
		if err != nil {
			return RunResult{}, fmt.Errorf("derive CI base commit: %w", err)
		}
	}
	if err := rewrite.ValidateFullObjectID(options.Base, objectIDLength, false); err != nil {
		return RunResult{}, fmt.Errorf("validate CI base commit: %w", err)
	}
	for _, value := range []struct {
		name  string
		other string
	}{
		{"source", options.Source},
		{"target", target},
	} {
		base, err := repo.MergeBase(options.Base, value.other)
		if err != nil {
			return RunResult{}, fmt.Errorf("find CI base for %s: %w", value.name, err)
		}
		if base != options.Base {
			return RunResult{}, fmt.Errorf("CI base is not an ancestor of %s", value.name)
		}
	}
	mode := options.Mode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "squash" && mode != "rebase" {
		return RunResult{}, fmt.Errorf("unsupported CI merge mode %q", mode)
	}

	sourceCommits, err := mergeCommits(repo, options.Base, options.Source)
	if err != nil {
		return RunResult{}, fmt.Errorf("list source commits: %w", err)
	}
	targetCommits, err := mergeCommits(repo, options.Base, target)
	if err != nil {
		return RunResult{}, fmt.Errorf("list target commits: %w", err)
	}
	if len(sourceCommits) == 0 {
		return RunResult{}, errors.New("CI source range contains no commits")
	}
	if len(targetCommits) == 0 {
		return RunResult{}, errors.New("CI target range contains no commits")
	}
	if mode == "auto" {
		if len(targetCommits) == 1 {
			mode = "squash"
		} else {
			mode = "rebase"
		}
	}

	result := RunResult{
		Provider:      provider,
		Mode:          mode,
		Base:          options.Base,
		Source:        options.Source,
		Target:        target,
		SourceCommits: len(sourceCommits),
		TargetCommits: len(targetCommits),
		Warnings:      nil,
	}
	pairs, warnings, err := makePairs(repo, sourceCommits, targetCommits, target, mode)
	if err != nil {
		return RunResult{}, err
	}
	result.Warnings = append(result.Warnings, warnings...)
	if len(pairs) == 0 {
		result.Warnings = append(result.Warnings, "no source commits could be mapped")
		return result, nil
	}
	mapping, err := rewrite.NewMappingForLength(pairs, objectIDLength)
	if err != nil {
		return RunResult{}, fmt.Errorf("validate CI rewrite mapping: %w", err)
	}
	var input strings.Builder
	for _, pair := range mapping.Pairs {
		fmt.Fprintf(&input, "%s %s\n", pair.Old, pair.New)
	}
	rewriteResult, err := provenance.HandlePostRewrite(repo, strings.NewReader(input.String()))
	if err != nil {
		return RunResult{}, fmt.Errorf("reconstruct CI attribution: %w", err)
	}
	result.Mapped = rewriteResult.Mapped
	result.Written = rewriteResult.Written
	result.Warnings = append(result.Warnings, rewriteResult.Warnings...)
	return result, nil
}

func resolveOptions(options RunOptions) RunOptions {
	if options.Base == "" {
		options.Base = os.Getenv(ciBaseEnv)
	}
	if options.Source == "" {
		options.Source = os.Getenv(ciSourceEnv)
	}
	if options.Target == "" {
		options.Target = os.Getenv(ciTargetEnv)
	}
	if options.Mode == "" {
		options.Mode = os.Getenv(ciModeEnv)
	}
	return options
}

func resolveTarget(repo *gitcmd.Repo, target string) (string, error) {
	if target != "" {
		return target, nil
	}
	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("read CI target: %w", err)
	}
	if head == "" {
		return "", errors.New("CI target is empty and repository has no HEAD")
	}
	return head, nil
}

func mergeCommits(repo *gitcmd.Repo, from, to string) ([]string, error) {
	commits, err := repo.RevList(from, to, maxMergeCommits+1)
	if err != nil {
		return nil, err
	}
	if len(commits) > maxMergeCommits {
		return nil, fmt.Errorf("CI merge range exceeds %d commits; narrow the merge", maxMergeCommits)
	}
	reverseStrings(commits)
	return commits, nil
}

func makePairs(
	repo *gitcmd.Repo,
	sourceCommits, targetCommits []string,
	target, mode string,
) ([]rewrite.Pair, []string, error) {
	switch mode {
	case "squash":
		if len(targetCommits) != 1 || targetCommits[0] != target {
			return nil, nil, errors.New("squash merge must produce one target commit")
		}
		pairs := make([]rewrite.Pair, 0, len(sourceCommits))
		for _, source := range sourceCommits {
			pairs = append(pairs, rewrite.Pair{Old: source, New: target})
		}
		return pairs, nil, nil
	case "rebase":
		return pairByPatchID(repo, sourceCommits, targetCommits)
	default:
		return nil, nil, fmt.Errorf("unsupported CI merge mode %q", mode)
	}
}

func pairByPatchID(repo *gitcmd.Repo, sourceCommits, targetCommits []string) ([]rewrite.Pair, []string, error) {
	sourceByPatch := make(map[string][]string, len(sourceCommits))
	var warnings []string
	for _, commit := range sourceCommits {
		patch, err := repo.PatchID(commit)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate source patch ID %s: %w", commit, err)
		}
		if patch == "" {
			warnings = append(warnings, fmt.Sprintf("skipped source commit %s with an empty patch", commit))
			continue
		}
		sourceByPatch[patch] = append(sourceByPatch[patch], commit)
	}
	usedSources := make(map[string]bool, len(sourceCommits))
	pairs := make([]rewrite.Pair, 0, len(targetCommits))
	for _, commit := range targetCommits {
		patch, err := repo.PatchID(commit)
		if err != nil {
			return nil, nil, fmt.Errorf("calculate target patch ID %s: %w", commit, err)
		}
		if patch == "" {
			warnings = append(warnings, fmt.Sprintf("skipped target commit %s with an empty patch", commit))
			continue
		}
		sources := sourceByPatch[patch]
		switch len(sources) {
		case 0:
			warnings = append(warnings, fmt.Sprintf("skipped target commit %s with no matching source patch", commit))
		case 1:
			source := sources[0]
			if usedSources[source] {
				warnings = append(warnings, fmt.Sprintf("skipped target commit %s with a duplicate source patch", commit))
				continue
			}
			usedSources[source] = true
			pairs = append(pairs, rewrite.Pair{Old: source, New: commit})
		default:
			warnings = append(warnings, fmt.Sprintf("skipped target commit %s with an ambiguous source patch", commit))
		}
	}
	return pairs, warnings, nil
}

func reverseStrings(values []string) {
	for left, right := 0, len(values)-1; left < right; left, right = left+1, right-1 {
		values[left], values[right] = values[right], values[left]
	}
}
