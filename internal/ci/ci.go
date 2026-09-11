// Package ci reconstructs attribution after forge merge operations.
package ci

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// maxPatchIDCalls bounds aggregate Git work used for rebase pairing.
	maxPatchIDCalls = 10_000
	patchIDDeadline = 2 * time.Minute
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
	rootInfo, err := os.Lstat(absoluteRoot)
	if err != nil {
		return InstallResult{}, fmt.Errorf("stat CI install root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return InstallResult{}, errors.New("CI install root is a symlink")
	}
	if !rootInfo.IsDir() {
		return InstallResult{}, errors.New("CI install root is not a directory")
	}
	path := filepath.Join(absoluteRoot, filepath.FromSlash(relative))
	if err := ensureInstallDirectory(absoluteRoot, filepath.Dir(path)); err != nil {
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
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create temporary CI workflow: %w", err)
	}
	tempPath := file.Name()
	defer func() {
		_ = file.Close()
		_ = os.Remove(tempPath)
	}()
	if err := file.Chmod(0o644); err != nil {
		return InstallResult{}, fmt.Errorf("chmod temporary CI workflow: %w", err)
	}
	if _, err := file.Write(template); err != nil {
		return InstallResult{}, fmt.Errorf("write temporary CI workflow: %w", err)
	}
	if err := file.Sync(); err != nil {
		return InstallResult{}, fmt.Errorf("sync temporary CI workflow: %w", err)
	}
	if err := file.Close(); err != nil {
		return InstallResult{}, fmt.Errorf("close temporary CI workflow: %w", err)
	}
	// A hard link publishes the fully synced file atomically without replacing
	// a file another installer may have created.
	if err := os.Link(tempPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return InstallResult{}, fmt.Errorf("refusing to replace existing CI workflow %q", relative)
		}
		return InstallResult{}, fmt.Errorf("install CI workflow atomically: %w", err)
	}
	return InstallResult{Path: relative, Changed: true}, nil
}

func ensureInstallDirectory(root, directory string) error {
	root = filepath.Clean(root)
	directory = filepath.Clean(directory)
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("CI workflow directory escapes install root")
	}
	current := root
	for _, component := range splitPath(relative) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			if mkdirErr := os.Mkdir(current, 0o755); mkdirErr != nil {
				if !errors.Is(mkdirErr, os.ErrExist) {
					return mkdirErr
				}
				info, statErr = os.Lstat(current)
			} else {
				info, statErr = os.Lstat(current)
			}
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlinked CI workflow directory %q", current)
		}
		if !info.IsDir() {
			return fmt.Errorf("CI workflow parent %q is not a directory", current)
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve CI install root: %w", err)
	}
	resolvedDirectory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return fmt.Errorf("resolve CI workflow directory: %w", err)
	}
	relative, err = filepath.Rel(resolvedRoot, resolvedDirectory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("resolved CI workflow directory escapes install root")
	}
	return nil
}

func splitPath(value string) []string {
	if value == "." || value == "" {
		return nil
	}
	return strings.FieldsFunc(value, func(r rune) bool {
		return r == '/' || r == '\\'
	})
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
	mode := options.Mode
	if mode == "" {
		mode = "auto"
	}
	if mode != "auto" && mode != "squash" && mode != "rebase" {
		return RunResult{}, fmt.Errorf("unsupported CI merge mode %q", mode)
	}
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
		options.Base, err = repo.Parent(target)
		if err != nil {
			return RunResult{}, fmt.Errorf("derive CI base commit: %w", err)
		}
		if options.Base == "" {
			options.Base = target
		}
	}
	if err := rewrite.ValidateFullObjectID(options.Base, objectIDLength, false); err != nil {
		return RunResult{}, fmt.Errorf("validate CI base commit: %w", err)
	}
	sourceBase, err := repo.MergeBase(options.Base, options.Source)
	if err != nil {
		return RunResult{}, fmt.Errorf("derive CI source base commit: %w", err)
	}
	targetBase, err := repo.MergeBase(options.Base, target)
	if err != nil {
		return RunResult{}, fmt.Errorf("validate CI target base commit: %w", err)
	}
	if targetBase != options.Base {
		return RunResult{}, errors.New("CI base is not an ancestor of target")
	}

	sourceCommits, err := mergeCommits(repo, sourceBase, options.Source)
	if err != nil {
		return RunResult{}, fmt.Errorf("list source commits: %w", err)
	}
	targetCommits, err := mergeCommits(repo, options.Base, target)
	if err != nil {
		return RunResult{}, fmt.Errorf("list target commits: %w", err)
	}
	result := RunResult{
		Provider:      provider,
		Mode:          mode,
		Base:          options.Base,
		Source:        options.Source,
		Target:        target,
		SourceCommits: len(sourceCommits),
		TargetCommits: len(targetCommits),
	}
	if mode == "auto" {
		mode = classifyMode(targetCommits, target)
		result.Mode = mode
	}
	if len(sourceCommits) == 0 || len(targetCommits) == 0 {
		if len(sourceCommits) == 0 && len(targetCommits) == 0 {
			result.Warnings = append(result.Warnings,
				"source and target ranges are empty; no attribution notes written")
		} else {
			result.Warnings = append(result.Warnings,
				"inconsistent CI merge ranges; no attribution notes written")
		}
		return result, nil
	}

	budget := newPatchIDBudget()
	pairs, warnings, err := makePairs(
		repo,
		sourceCommits,
		targetCommits,
		target,
		mode,
		budget,
	)
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

func classifyMode(targetCommits []string, target string) string {
	if len(targetCommits) == 1 && targetCommits[0] == target {
		return "squash"
	}
	return "rebase"
}

func makePairs(
	repo *gitcmd.Repo,
	sourceCommits, targetCommits []string,
	target, mode string,
	budget *patchIDBudget,
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
		return pairByPatchID(repo, sourceCommits, targetCommits, budget)
	default:
		return nil, nil, fmt.Errorf("unsupported CI merge mode %q", mode)
	}
}

type patchIDBudget struct {
	calls    int
	deadline time.Time
	warned   bool
}

func newPatchIDBudget() *patchIDBudget {
	return &patchIDBudget{deadline: time.Now().Add(patchIDDeadline)}
}

func (budget *patchIDBudget) allow() bool {
	if budget == nil {
		return true
	}
	if budget.calls >= maxPatchIDCalls || time.Now().After(budget.deadline) {
		return false
	}
	budget.calls++
	return true
}

func (budget *patchIDBudget) warning() string {
	if budget != nil && budget.calls >= maxPatchIDCalls {
		return fmt.Sprintf(
			"PatchID budget of %d calls exceeded; remaining commits left untracked",
			maxPatchIDCalls,
		)
	}
	return "PatchID deadline exceeded; remaining commits left untracked"
}

func pairByPatchID(
	repo *gitcmd.Repo,
	sourceCommits, targetCommits []string,
	budget *patchIDBudget,
) ([]rewrite.Pair, []string, error) {
	sourceByPatch := make(map[string][]string, len(sourceCommits))
	var warnings []string
	for _, commit := range sourceCommits {
		if budget != nil && !budget.allow() {
			if !budget.warned {
				warnings = append(warnings, budget.warning())
				budget.warned = true
			}
			continue
		}
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
		if budget != nil && !budget.allow() {
			if !budget.warned {
				warnings = append(warnings, budget.warning())
				budget.warned = true
			}
			continue
		}
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
