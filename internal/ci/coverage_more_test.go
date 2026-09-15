package ci

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const (
	coverageIDLength  = 40
	coverageOneID     = "1111111111111111111111111111111111111111"
	coverageBaseDir   = ".github"
	coverageWorkflows = "workflows"
	coverageWorkflow  = "git-byline.yml"
	coverageSquash    = "squash"
)

func TestInstallAdditionalBranches(t *testing.T) {
	t.Run("root symlink", func(t *testing.T) {
		target := t.TempDir()
		root := filepath.Join(t.TempDir(), "root")
		if err := os.Symlink(target, root); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		_, err := Install(root, ProviderGitHub)
		assertErrorContains(t, err, "CI install root is a symlink")
	})

	t.Run("unknown provider", func(t *testing.T) {
		_, err := Install(t.TempDir(), Provider("unknown"))
		assertErrorContains(t, err, "unsupported CI provider")
	})

	t.Run("workflow directory is file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, coverageBaseDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, coverageBaseDir, coverageWorkflows), []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Install(root, ProviderGitHub)
		assertErrorContains(t, err, "not a directory")
	})

	t.Run("workflow path is directory", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, coverageBaseDir, coverageWorkflows, coverageWorkflow)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Install(root, ProviderGitHub)
		assertErrorContains(t, err, "not a regular file")
	})

	t.Run("workflow path cannot be inspected", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(directory, 0o755)
		_, err := Install(root, ProviderGitHub)
		assertErrorContains(t, err, "inspect CI workflow path")
	})

	t.Run("temporary workflow cannot be created", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(directory, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(directory, 0o755)
		_, err := Install(root, ProviderGitHub)
		assertErrorContains(t, err, "create temporary CI workflow")
	})

	t.Run("existing workflow cannot be read", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, coverageBaseDir, coverageWorkflows, coverageWorkflow)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("existing"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0o600)
		_, err := Install(root, ProviderGitHub)
		if err == nil {
			t.Skip("filesystem permits reading mode-zero files")
		}
		assertErrorContains(t, err, "read existing CI workflow")
	})
}

func TestEnsureInstallDirectoryBranches(t *testing.T) {
	t.Run("same root", func(t *testing.T) {
		root := t.TempDir()
		if err := ensureInstallDirectory(root, root); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("escape", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		err := ensureInstallDirectory(root, outside)
		assertErrorContains(t, err, "escapes install root")
	})

	t.Run("invalid component", func(t *testing.T) {
		root := t.TempDir()
		err := ensureInstallDirectory(root, filepath.Join(root, "bad\x00"))
		if err == nil {
			t.Fatal("ensureInstallDirectory accepted an invalid path")
		}
	})

	t.Run("missing root", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "missing")
		err := ensureInstallDirectory(root, root)
		assertErrorContains(t, err, "resolve CI install root")
	})

	t.Run("parent is file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, coverageBaseDir), []byte("file"), 0o644); err != nil {
			t.Fatal(err)
		}
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		err := ensureInstallDirectory(root, directory)
		assertErrorContains(t, err, "not a directory")
	})

	t.Run("parent is symlink", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(root, coverageBaseDir)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		err := ensureInstallDirectory(root, directory)
		assertErrorContains(t, err, "symlinked CI workflow directory")
	})

	t.Run("parent cannot be created", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Chmod(root, 0o555); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(root, 0o755)
		directory := filepath.Join(root, coverageBaseDir)
		err := ensureInstallDirectory(root, directory)
		if err == nil {
			t.Skip("filesystem permits creating directories without write permission")
		}
	})

	if got := splitPath("."); got != nil {
		t.Fatalf("splitPath(.) = %#v, want nil", got)
	}
	if got := splitPath(""); got != nil {
		t.Fatalf("splitPath(empty) = %#v, want nil", got)
	}
}

func TestEmbeddedTemplateReadError(t *testing.T) {
	original := workflowTemplates
	workflowTemplates = embed.FS{}
	defer func() {
		workflowTemplates = original
	}()
	_, err := Template(ProviderGitHub)
	assertErrorContains(t, err, "read embedded CI template")
}

func TestRunValidationBranches(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "source\n")
	source := commit(t, root, "source")
	unknown := strings.Repeat("2", coverageIDLength)

	tests := []struct {
		name    string
		options RunOptions
		want    string
	}{
		{
			name:    "invalid source",
			options: RunOptions{Base: base, Source: "bad", Target: source},
			want:    "validate CI source commit",
		},
		{
			name:    "invalid target",
			options: RunOptions{Base: base, Source: source, Target: "bad"},
			want:    "validate CI target commit",
		},
		{
			name:    "target parent lookup",
			options: RunOptions{Source: source, Target: unknown},
			want:    "derive CI base commit",
		},
		{
			name:    "source merge base",
			options: RunOptions{Base: base, Source: unknown, Target: source},
			want:    "derive CI source base commit",
		},
		{
			name:    "target merge base",
			options: RunOptions{Base: base, Source: source, Target: unknown},
			want:    "validate CI target base commit",
		},
	}
	repo := discover(t, root)
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			_, err := Run(repo, ProviderGitHub, tt.options)
			assertErrorContains(t, err, tt.want)
		})
	}
}

func TestRunRejectsNonAncestorBase(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "common\n")
	common := commit(t, root, "common")
	runGit(t, root, "switch", "-c", "side-a")
	writeFile(t, root, "file.txt", "side-a\n")
	sideA := commit(t, root, "side-a")
	runGit(t, root, "switch", "-c", "side-b", common)
	writeFile(t, root, "file.txt", "side-b\n")
	sideB := commit(t, root, "side-b")

	_, err := Run(discover(t, root), ProviderGitHub, RunOptions{
		Base: sideA, Source: sideA, Target: sideB, Mode: coverageSquash,
	})
	assertErrorContains(t, err, "CI base is not an ancestor")
}

func TestRunDerivesRootBase(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "root\n")
	head := commit(t, root, "root")

	result, err := Run(discover(t, root), ProviderGitHub, RunOptions{
		Source: head, Target: head, Mode: coverageSquash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Base != head || len(result.Warnings) == 0 {
		t.Fatalf("Run() = %+v, want root base and empty-range warning", result)
	}
}

func TestRunRejectsInvalidPairShape(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "source\n")
	source := commit(t, root, "source")
	runGit(t, root, "reset", "--hard", base)
	writeFile(t, root, "file.txt", "target one\n")
	commit(t, root, "target one")
	writeFile(t, root, "file.txt", "target two\n")
	target := commit(t, root, "target two")

	_, err := Run(discover(t, root), ProviderGitHub, RunOptions{
		Base: base, Source: source, Target: target, Mode: coverageSquash,
	})
	assertErrorContains(t, err, "squash merge must produce one target commit")
}

func TestRunRejectsInvalidMappedCommitID(t *testing.T) {
	baseID := strings.Repeat("1", coverageIDLength)
	sourceID := strings.Repeat("2", coverageIDLength)
	targetID := strings.Repeat("3", coverageIDLength)
	invalidID := strings.Repeat("4", coverageIDLength-1)
	repo := fakeCIRepo(t, fmt.Sprintf(
		`  *"--show-object-format=storage"*) printf '%%s\n' "sha1" ;;
  *"merge-base"*) printf '%%s\n' "%s" ;;
  *"rev-list"*"%s"*) printf '%%s\n' "%s" ;;
  *"rev-list"*) printf '%%s\n' "%s" ;;
`, baseID, sourceID, invalidID, targetID))

	_, err := Run(repo, ProviderGitHub, RunOptions{
		Base: baseID, Source: sourceID, Target: targetID, Mode: coverageSquash,
	})
	assertErrorContains(t, err, "validate CI rewrite mapping")
}

func TestRunReportsReconstructionFailure(t *testing.T) {
	root := initRepository(t)
	writeFile(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	writeFile(t, root, "file.txt", "source\n")
	source := commit(t, root, "source")
	runGit(t, root, "reset", "--hard", base)
	writeFile(t, root, "file.txt", "target\n")
	target := commit(t, root, "target")
	repo := discover(t, root)
	if err := os.WriteFile(filepath.Join(repo.CommonDir, "byline"), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Run(repo, ProviderGitHub, RunOptions{
		Base: base, Source: source, Target: target, Mode: coverageSquash,
	})
	assertErrorContains(t, err, "reconstruct CI attribution")
}

func TestInstallHandlesConcurrentTargetCreation(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		root := t.TempDir()
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(directory, coverageWorkflow)
		stop := make(chan struct{})
		found := make(chan bool, 1)
		go watchInstallTemp(t, directory, target, stop, found, false)
		_, err := Install(root, ProviderGitHub)
		close(stop)
		<-found
		if err != nil && strings.Contains(err.Error(), "refusing to replace existing CI workflow") {
			return
		}
	}
	t.Fatal("concurrent target creation did not hit atomic collision")
}

func TestInstallHandlesConcurrentDirectoryRemoval(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		root := t.TempDir()
		directory := filepath.Join(root, coverageBaseDir, coverageWorkflows)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		stop := make(chan struct{})
		found := make(chan bool, 1)
		go watchInstallTemp(t, directory, "", stop, found, true)
		_, err := Install(root, ProviderGitHub)
		close(stop)
		<-found
		if err != nil && strings.Contains(err.Error(), "install CI workflow atomically") {
			return
		}
	}
	t.Fatal("concurrent directory removal did not hit atomic failure")
}

func watchInstallTemp(
	t *testing.T,
	directory, target string,
	stop <-chan struct{},
	found chan<- bool,
	removeDirectory bool,
) {
	t.Helper()
	defer func() { found <- true }()
	for {
		select {
		case <-stop:
			return
		default:
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), "."+coverageWorkflow+".") {
				continue
			}
			tempPath := filepath.Join(directory, entry.Name())
			if removeDirectory {
				if err := os.Remove(tempPath); err == nil {
					_ = os.Remove(directory)
					return
				}
				continue
			}
			if err := os.WriteFile(target, []byte("racing installer\n"), 0o644); err == nil {
				return
			}
		}
	}
}

func TestRunReportsRepositoryCommandFailures(t *testing.T) {
	t.Run("object format", func(t *testing.T) {
		repo := fakeCIRepo(t, `  *"--show-object-format=storage"*) printf '%s\n' "unknown" ;;
`)
		_, err := Run(repo, ProviderGitHub, RunOptions{
			Base: coverageOneID, Source: coverageOneID, Target: coverageOneID,
		})
		assertErrorContains(t, err, "read repository object format")
	})

	t.Run("target", func(t *testing.T) {
		repo := fakeCIRepo(t, `  *"--show-object-format=storage"*) printf '%s\n' "sha1" ;;
  *"--verify HEAD"*) printf '%s\n' "head unavailable" >&2; exit 1 ;;
`)
		_, err := Run(repo, ProviderGitHub, RunOptions{Source: coverageOneID})
		assertErrorContains(t, err, "read CI target")
	})

	t.Run("source range", func(t *testing.T) {
		repo := fakeCIRepo(t, fmt.Sprintf(
			`  *"--show-object-format=storage"*) printf '%%s\n' "sha1" ;;
  *"merge-base"*) printf '%%s\n' "%s" ;;
  *"rev-list"*) exit 1 ;;
`, coverageOneID))
		_, err := Run(repo, ProviderGitHub, RunOptions{
			Base: coverageOneID, Source: coverageOneID, Target: coverageOneID,
		})
		assertErrorContains(t, err, "list source commits")
	})

	t.Run("target range", func(t *testing.T) {
		counter := filepath.Join(t.TempDir(), "counter")
		t.Setenv("CI_FAKE_COUNTER", counter)
		repo := fakeCIRepo(t, fmt.Sprintf(
			`  *"--show-object-format=storage"*) printf '%%s\n' "sha1" ;;
  *"merge-base"*) printf '%%s\n' "%s" ;;
  *"rev-list"*)
    count=$(cat "$CI_FAKE_COUNTER" 2>/dev/null || printf '0')
    count=$((count + 1))
    printf '%%s\n' "$count" > "$CI_FAKE_COUNTER"
    if [ "$count" -eq 1 ]; then printf '%%s\n' "%s"; else exit 1; fi
    ;;
`, coverageOneID, coverageOneID))
		_, err := Run(repo, ProviderGitHub, RunOptions{
			Base: coverageOneID, Source: coverageOneID, Target: coverageOneID,
		})
		assertErrorContains(t, err, "list target commits")
	})
}

func TestMergeCommitsRejectsInvalidAndOversizedRanges(t *testing.T) {
	root := initRepository(t)
	repo := discover(t, root)
	if _, err := mergeCommits(repo, "bad", "bad"); err == nil {
		t.Fatal("mergeCommits accepted an invalid range")
	}

	largeRepo := fakeCIRepo(t, fmt.Sprintf(
		`  *"rev-list"*)
    i=0
    while [ "$i" -lt 10001 ]; do printf '%%s\n' "%s"; i=$((i + 1)); done
    ;;
`, coverageOneID))
	_, err := mergeCommits(largeRepo, coverageOneID, coverageOneID)
	assertErrorContains(t, err, "merge range exceeds")
}

func TestMakePairsValidation(t *testing.T) {
	t.Run("invalid squash shape", func(t *testing.T) {
		_, _, err := makePairs(nil, []string{coverageOneID}, []string{coverageOneID, coverageOneID}, coverageOneID, coverageSquash, nil)
		assertErrorContains(t, err, "squash merge must produce one target commit")
	})

	t.Run("unsupported mode", func(t *testing.T) {
		_, _, err := makePairs(nil, nil, nil, coverageOneID, "merge", nil)
		assertErrorContains(t, err, "unsupported CI merge mode")
	})
}

func TestPairByPatchIDBudgetAndErrors(t *testing.T) {
	t.Run("source budget", func(t *testing.T) {
		budget := &patchIDBudget{calls: maxPatchIDCalls, deadline: time.Now().Add(time.Minute)}
		_, warnings, err := pairByPatchID(nil, []string{coverageOneID}, nil, budget)
		if err != nil || len(warnings) != 1 {
			t.Fatalf("source budget = %v, %v", warnings, err)
		}
		assertWarningContains(t, warnings, "budget")
	})

	t.Run("target deadline", func(t *testing.T) {
		budget := &patchIDBudget{deadline: time.Now().Add(-time.Minute)}
		_, warnings, err := pairByPatchID(nil, nil, []string{coverageOneID}, budget)
		if err != nil || len(warnings) != 1 {
			t.Fatalf("target deadline = %v, %v", warnings, err)
		}
		assertWarningContains(t, warnings, "deadline")
	})

	t.Run("nil budget", func(t *testing.T) {
		if budget := (*patchIDBudget)(nil); !budget.allow() {
			t.Fatal("nil budget disallowed patch ID call")
		}
	})

	t.Run("source patch error", func(t *testing.T) {
		_, _, err := pairByPatchID(discover(t, initRepository(t)), []string{"bad"}, nil, nil)
		assertErrorContains(t, err, "calculate source patch ID")
	})

	t.Run("target patch error", func(t *testing.T) {
		_, _, err := pairByPatchID(discover(t, initRepository(t)), nil, []string{"bad"}, nil)
		assertErrorContains(t, err, "calculate target patch ID")
	})
}

func TestPairByPatchIDHandlesEmptyAndDuplicatePatches(t *testing.T) {
	t.Run("empty patches", func(t *testing.T) {
		root := initRepository(t)
		writeFile(t, root, "file.txt", "base\n")
		commit(t, root, "base")
		runGit(t, root, "commit", "--allow-empty", "-m", "empty source")
		source := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
		runGit(t, root, "commit", "--allow-empty", "-m", "empty target")
		target := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))

		pairs, warnings, err := pairByPatchID(discover(t, root), []string{source}, []string{target}, nil)
		if err != nil || len(pairs) != 0 || len(warnings) != 2 {
			t.Fatalf("empty patches = %v, %v, %v", pairs, warnings, err)
		}
		assertWarningContains(t, warnings, "empty patch")
	})

	t.Run("ambiguous source patch", func(t *testing.T) {
		root, commits := identicalPatchCommits(t, 3)
		pairs, warnings, err := pairByPatchID(
			discover(t, root), commits[:2], commits[2:3], nil,
		)
		if err != nil || len(pairs) != 0 {
			t.Fatalf("ambiguous patch = %v, %v, %v", pairs, warnings, err)
		}
		assertWarningContains(t, warnings, "ambiguous source patch")
	})

	t.Run("duplicate target patch", func(t *testing.T) {
		root, commits := identicalPatchCommits(t, 3)
		pairs, warnings, err := pairByPatchID(
			discover(t, root), commits[:1], commits[1:], nil,
		)
		if err != nil || len(pairs) != 1 {
			t.Fatalf("duplicate patch = %v, %v, %v", pairs, warnings, err)
		}
		assertWarningContains(t, warnings, "duplicate source patch")
	})

	t.Run("nil budget", func(t *testing.T) {
		root, commits := identicalPatchCommits(t, 2)
		pairs, warnings, err := pairByPatchID(
			discover(t, root), commits[:1], commits[1:], nil,
		)
		if err != nil || len(pairs) != 1 || len(warnings) != 0 {
			t.Fatalf("nil budget = %v, %v, %v", pairs, warnings, err)
		}
	})
}

func identicalPatchCommits(t *testing.T, count int) (string, []string) {
	t.Helper()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "base\n")
	base := commit(t, root, "base")
	commits := make([]string, 0, count)
	for index := 0; index < count; index++ {
		runGit(t, root, "reset", "--hard", base)
		writeFile(t, root, "file.txt", "same patch\n")
		commits = append(commits, commit(t, root, fmt.Sprintf("same patch %d", index)))
	}
	return root, commits
}

func fakeCIRepo(t *testing.T, cases string) *gitcmd.Repo {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$PWD" ;;
  *"--git-dir"*) printf '%s/.git\n' "$PWD" ;;
  *"--git-common-dir"*) printf '%s/.git\n' "$PWD" ;;
` + cases + `  *) exit 1 ;;
esac
`
	gitPath := filepath.Join(bin, "git")
	if err := os.WriteFile(gitPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatalf("Discover() = %v", err)
	}
	return repo
}

func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func assertWarningContains(t *testing.T, warnings []string, want string) {
	t.Helper()
	for _, warning := range warnings {
		if strings.Contains(warning, want) {
			return
		}
	}
	t.Fatalf("warnings = %v, want %q", warnings, want)
}
