package ci

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
)

func TestParseProviderAndPaths(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		value    string
		provider Provider
		path     string
		wantErr  bool
	}{
		{"github", "github", ProviderGitHub, ".github/workflows/git-byline.yml", false},
		{"gitlab", "gitlab", ProviderGitLab, ".gitlab/ci/git-byline.yml", false},
		{"unknown", "bitbucket", "", "", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider, err := ParseProvider(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseProvider() returned nil error")
				}
				return
			}
			if err != nil || provider != tt.provider {
				t.Fatalf("ParseProvider() = %q, %v", provider, err)
			}
			path, err := TemplatePath(provider)
			if err != nil || path != tt.path {
				t.Fatalf("TemplatePath() = %q, %v", path, err)
			}
			template, err := Template(provider)
			if err != nil || len(template) == 0 || template[len(template)-1] != '\n' {
				t.Fatalf("Template() length = %d, error = %v", len(template), err)
			}
		})
	}
	if _, err := TemplatePath(Provider("other")); err == nil {
		t.Fatal("TemplatePath accepted an unknown provider")
	}
	if _, err := Template(Provider("other")); err == nil {
		t.Fatal("Template accepted an unknown provider")
	}
}

func TestTemplatesSecurityContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider  Provider
		required  []string
		forbidden []string
	}{
		{
			provider: ProviderGitHub,
			required: []string{
				"concurrency:",
				"cancel-in-progress: false",
				`GIT_BYLINE_REMOTE_URL: ${{ github.server_url }}/${{ github.repository }}.git`,
				`GIT_CONFIG_GLOBAL: /dev/null`,
				`GIT_CONFIG_SYSTEM: /dev/null`,
				`git config --local core.hooksPath "$EMPTY_HOOKS"`,
				"git remote set-url origin",
				"GIT_BYLINE_PUSH_HOOKS",
				"git config --local user.name git-byline-ci",
				`go-version: "1.24.0"`,
				`GIT_BYLINE_CI_BASE=%s`,
			},
		},
		{
			provider: ProviderGitLab,
			required: []string{
				"image: golang:1.24.0@sha256:",
				"resource_group:",
				"timeout: 15m",
				`if: '$CI_PIPELINE_SOURCE == "push" && $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH'`,
				`if: '$CI_PIPELINE_SOURCE == "merge_request_event"'`,
				"CI_COMMIT_SHA",
				`GIT_BYLINE_REMOTE_URL: "$CI_PROJECT_URL"`,
				"GIT_CONFIG_GLOBAL: /dev/null",
				"GIT_CONFIG_SYSTEM: /dev/null",
				"gitlab_git ls-remote",
				"gitlab_git fetch",
				"git config --local user.email git-byline-ci@users.noreply.gitlab.com",
				"git-byline-push-hooks",
				`source="${3:-$base}"`,
				"unset GITLAB_TOKEN",
			},
			forbidden: []string{
				"merged_result",
				"CI_MERGE_REQUEST_EVENT_TYPE",
				"api",
			},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.provider), func(t *testing.T) {
			t.Parallel()
			data, err := Template(tt.provider)
			if err != nil {
				t.Fatal(err)
			}
			template := string(data)
			for _, value := range tt.required {
				if !strings.Contains(template, value) {
					t.Errorf("template lacks %q", value)
				}
			}
			for _, value := range tt.forbidden {
				if strings.Contains(template, value) {
					t.Errorf("template contains forbidden %q", value)
				}
			}
		})
	}
}

func TestInstallWorkflowIsIdempotentAndNonDestructive(t *testing.T) {
	t.Parallel()
	for _, provider := range []Provider{ProviderGitHub, ProviderGitLab} {
		provider := provider
		t.Run(string(provider), func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			first, err := Install(root, provider)
			if err != nil {
				t.Fatal(err)
			}
			if !first.Changed || first.Path == "" {
				t.Fatalf("first install = %+v, want changed", first)
			}
			second, err := Install(root, provider)
			if err != nil {
				t.Fatal(err)
			}
			if second.Changed {
				t.Fatalf("second install = %+v, want no change", second)
			}
			path := filepath.Join(root, filepath.FromSlash(first.Path))
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if expected, _ := Template(provider); !bytes.Equal(data, expected) {
				t.Fatal("installed workflow differs from canonical template")
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".tmp") {
					t.Fatalf("atomic install left temporary file %q", entry.Name())
				}
			}
			if err := os.WriteFile(path, []byte("user workflow\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Install(root, provider); err == nil {
				t.Fatal("Install() replaced a different workflow")
			}
		})
	}
}

func TestInstallRejectsSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "outside.yml")
	if err := os.WriteFile(target, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(path, "git-byline.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(root, ProviderGitHub); err == nil {
		t.Fatal("Install() followed a workflow symlink")
	}
}

func TestInstallRejectsSymlinkedParent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".github")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Install(root, ProviderGitHub); err == nil {
		t.Fatal("Install() followed a symlinked workflow parent")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlink target changed: %v", entries)
	}
}

func TestInstallRejectsInvalidRoots(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "root")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(file, ProviderGitHub); err == nil {
		t.Fatal("Install() accepted a file root")
	}
	if _, err := Install(filepath.Join(t.TempDir(), "missing"), ProviderGitHub); err == nil {
		t.Fatal("Install() accepted a missing root")
	}
}

func TestRunSquashProjectsSourceCommitsInOrder(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	sourceOne := commit(t, root, "source one")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	sourceTwo := commit(t, root, "source two")

	runGit(t, root, "reset", "--hard", base)
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	target := commit(t, root, "squash")

	repo := discover(t, root)
	writeNoteForFile(t, repo, sourceOne, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "one", Model: "m1", Session: "s1", TS: "2026-01-01T00:00:00Z"},
	})
	writeNoteForFile(t, repo, sourceTwo, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "two", Model: "m2", Session: "s2", TS: "2026-01-01T00:00:01Z"},
		{Author: model.AuthorAI, Agent: "two", Model: "m2", Session: "s2", TS: "2026-01-01T00:00:01Z"},
	})

	result, err := Run(repo, ProviderGitHub, RunOptions{
		Base: base, Source: sourceTwo, Target: target, Mode: "squash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != 2 || result.Written != 1 {
		t.Fatalf("Run() = %+v, want two mappings and one note", result)
	}
	note := readNote(t, repo, target)
	assertReconstructedNoteVerified(t, repo, target)
	file := note.Files["file.txt"]
	if len(file.Ranges) != 2 || file.Ranges[1].Agent != "two" {
		t.Fatalf("squash ranges = %+v, want later source attribution", file.Ranges)
	}
	if err := repo.DeleteNoteRef("refs/notes/byline", target); err != nil {
		t.Fatal(err)
	}
	writeNoteForFile(t, repo, target, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorUntracked},
		{Author: model.AuthorUntracked},
	})
	result, err = Run(repo, ProviderGitHub, RunOptions{
		Base: base, Source: sourceTwo, Target: target, Mode: "squash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Written != 0 || len(result.Warnings) == 0 ||
		!strings.Contains(result.Warnings[len(result.Warnings)-1], "different attribution note") {
		t.Fatalf("different target note result = %+v, want warning and no overwrite", result)
	}
}

func TestRunRebasePairsByPatchID(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	sourceOne := commit(t, root, "source one")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	sourceTwo := commit(t, root, "source two")

	runGit(t, root, "reset", "--hard", base)
	writeFile(t, root, "file.txt", "one\ntwo\n")
	targetOne := commit(t, root, "rebased one")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	targetTwo := commit(t, root, "rebased two")

	repo := discover(t, root)
	writeNoteForFile(t, repo, sourceOne, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "source-one", Model: "m", Session: "s1", TS: "2026-01-01T00:00:00Z"},
	})
	writeNoteForFile(t, repo, sourceTwo, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "source-one", Model: "m", Session: "s1", TS: "2026-01-01T00:00:00Z"},
		{Author: model.AuthorAI, Agent: "source-two", Model: "m", Session: "s2", TS: "2026-01-01T00:00:01Z"},
	})

	result, err := Run(repo, ProviderGitLab, RunOptions{
		Base: base, Source: sourceTwo, Target: targetTwo, Mode: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "rebase" || result.Mapped != 2 || result.Written != 2 {
		t.Fatalf("Run() = %+v, want two mapped notes", result)
	}
	first := readNote(t, repo, targetOne).Files["file.txt"]
	second := readNote(t, repo, targetTwo).Files["file.txt"]
	assertReconstructedNoteVerified(t, repo, targetOne)
	assertReconstructedNoteVerified(t, repo, targetTwo)
	if len(first.Ranges) != 2 || first.Ranges[1].Agent != "source-one" {
		t.Fatalf("first rebase ranges = %+v", first.Ranges)
	}
	if len(second.Ranges) != 3 || second.Ranges[2].Agent != "source-two" {
		t.Fatalf("second rebase ranges = %+v", second.Ranges)
	}
}

func TestRunRejectsUnsupportedContexts(t *testing.T) {
	t.Parallel()
	if _, err := Run(nil, ProviderGitHub, RunOptions{}); err == nil {
		t.Fatal("Run(nil) returned nil error")
	}
	if _, err := ParseProvider("unknown"); err == nil {
		t.Fatal("ParseProvider accepted unknown provider")
	}
	root := initRepository(t)
	repo := discover(t, root)
	if _, err := Run(repo, Provider("unknown"), RunOptions{}); err == nil {
		t.Fatal("Run accepted an unknown provider")
	}
	if _, err := Run(repo, ProviderGitHub, RunOptions{
		Base:   strings.Repeat("1", 40),
		Source: strings.Repeat("2", 40),
	}); err == nil || !strings.Contains(err.Error(), "no HEAD") {
		t.Fatalf("Run on unborn repository error = %v", err)
	}
}

func TestRunValidatesInputsAndMergeShape(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	source := commit(t, root, "source")
	repo := discover(t, root)

	tests := []struct {
		name    string
		options RunOptions
		want    string
	}{
		{"missing source", RunOptions{Base: base, Target: source}, "GIT_BYLINE_CI_SOURCE"},
		{"invalid base", RunOptions{Base: "bad", Source: source, Target: source}, "object ID"},
		{"invalid mode", RunOptions{Base: base, Source: source, Target: source, Mode: "merge"}, "merge mode"},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(repo, ProviderGitHub, tt.options)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Fatalf("Run() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRunEmptyRangesWarnAndSkip(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	source := commit(t, root, "source")
	repo := discover(t, root)

	tests := []struct {
		name    string
		options RunOptions
	}{
		{
			name:    "both empty",
			options: RunOptions{Base: base, Source: base, Target: base, Mode: "squash"},
		},
		{
			name:    "target empty",
			options: RunOptions{Base: base, Source: source, Target: base, Mode: "rebase"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result, err := Run(repo, ProviderGitHub, tt.options)
			if err != nil {
				t.Fatal(err)
			}
			if result.Written != 0 || len(result.Warnings) == 0 {
				t.Fatalf("Run() = %+v, want warning and no writes", result)
			}
		})
	}
}

func TestRunRebaseWarnsForUnmatchedPatch(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	source := commit(t, root, "source")

	runGit(t, root, "reset", "--hard", base)
	writeFile(t, root, "other.txt", "different\n")
	target := commit(t, root, "target")

	result, err := Run(discover(t, root), ProviderGitLab, RunOptions{
		Base: base, Source: source, Target: target, Mode: "rebase",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mapped != 0 || len(result.Warnings) == 0 ||
		!strings.Contains(result.Warnings[0], "no matching source patch") {
		t.Fatalf("Run() = %+v, want unmatched patch warning", result)
	}
}

func TestRunSquashUsesAdvancedTargetBase(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	common := commit(t, root, "common")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	sourceOne := commit(t, root, "source one")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	sourceTwo := commit(t, root, "source two")

	runGit(t, root, "reset", "--hard", common)
	writeFile(t, root, "target.txt", "target advance\n")
	targetBase := commit(t, root, "target advance")
	writeFile(t, root, "file.txt", "one\ntwo\nthree\n")
	target := commit(t, root, "squash")

	repo := discover(t, root)
	writeNoteForFile(t, repo, sourceOne, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "one", Model: "m", Session: "s1"},
	})
	writeNoteForFile(t, repo, sourceTwo, "file.txt", []model.Attribution{
		{Author: model.AuthorUntracked},
		{Author: model.AuthorAI, Agent: "one", Model: "m", Session: "s1"},
		{Author: model.AuthorAI, Agent: "two", Model: "m", Session: "s2"},
	})
	result, err := Run(repo, ProviderGitHub, RunOptions{
		Base: targetBase, Source: sourceTwo, Target: target, Mode: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "squash" || result.Written != 1 {
		t.Fatalf("Run() = %+v, want advanced-base squash reconstruction", result)
	}
	assertReconstructedNoteVerified(t, repo, target)
}

func TestRunUsesEnvironmentOptions(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	source := commit(t, root, "source")
	runGit(t, root, "commit", "--amend", "-m", "target")
	target := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
	t.Setenv(ciBaseEnv, base)
	t.Setenv(ciSourceEnv, source)
	t.Setenv(ciTargetEnv, target)
	t.Setenv(ciModeEnv, "squash")
	result, err := Run(discover(t, root), ProviderGitHub, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Mode != "squash" || result.Target != target {
		t.Fatalf("Run() = %+v, want environment options", result)
	}
	t.Setenv(ciBaseEnv, "")
	t.Setenv(ciTargetEnv, "")
	result, err = Run(discover(t, root), ProviderGitHub, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target != target {
		t.Fatalf("Run() default target = %q, want %q", result.Target, target)
	}
}

func assertReconstructedNoteVerified(t *testing.T, repo *gitcmd.Repo, commitID string) {
	t.Helper()
	data, found, err := repo.ReadNote(commitID)
	if err != nil || !found {
		t.Fatalf("verification note = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatalf("verification decode = %v", err)
	}
	for path, file := range note.Files {
		blob, exists, err := repo.BlobID(commitID, path)
		if err != nil || !exists || blob != file.Blob {
			t.Fatalf("verification blob %s = %q, %t, %v; note=%q", path, blob, exists, err, file.Blob)
		}
		content, err := repo.ReadBlob(blob)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := engine.SplitLines(content)
		if err != nil {
			t.Fatal(err)
		}
		if err := model.ValidateRanges(file.Ranges, len(lines)); err != nil {
			t.Fatalf("verification ranges %s: %v", path, err)
		}
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--initial-branch=main")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "user.email", "test@example.com")
	return root
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, root, message string) string {
	t.Helper()
	runGit(t, root, "add", "--", ".")
	runGit(t, root, "commit", "-m", message)
	return strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

func discover(t *testing.T, root string) *gitcmd.Repo {
	t.Helper()
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func writeNoteForFile(
	t *testing.T,
	repo *gitcmd.Repo,
	commitID, path string,
	attrs []model.Attribution,
) {
	t.Helper()
	blob, exists, err := repo.BlobID(commitID, path)
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	ranges := make([]model.Range, 0, len(attrs))
	for index, attr := range attrs {
		ranges = append(ranges, model.Range{
			Start: index + 1, End: index + 1, Attribution: attr,
		})
	}
	noteData, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			path: {Blob: blob, Ranges: ranges},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commitID, noteData); err != nil {
		t.Fatal(err)
	}
}

func readNote(t *testing.T, repo *gitcmd.Repo, commitID string) model.Note {
	t.Helper()
	data, found, err := repo.ReadNote(commitID)
	if err != nil || !found {
		t.Fatalf("ReadNote() = %t, %v", found, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func TestRunResultJSONContract(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(RunResult{Provider: ProviderGitHub, Mode: "squash"})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"provider":"github"`)) {
		t.Fatalf("JSON = %s", data)
	}
}

func TestPatchIDBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		budget   patchIDBudget
		wantCall bool
		wantWarn string
	}{
		{
			name:     "call limit",
			budget:   patchIDBudget{calls: maxPatchIDCalls, deadline: time.Now().Add(time.Minute)},
			wantWarn: "budget",
		},
		{
			name:     "deadline",
			budget:   patchIDBudget{deadline: time.Now().Add(-time.Minute)},
			wantWarn: "deadline",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.budget.allow(); got != tt.wantCall {
				t.Fatalf("allow() = %t, want %t", got, tt.wantCall)
			}
			if warning := tt.budget.warning(); !strings.Contains(warning, tt.wantWarn) {
				t.Fatalf("warning = %q, want %q", warning, tt.wantWarn)
			}
		})
	}
}
