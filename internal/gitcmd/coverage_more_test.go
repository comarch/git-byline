package gitcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const (
	coverageFileName  = "file"
	coverageNotesName = "refs/notes/test"
	coverageBranch    = "refs/heads/main"
)

func TestGitcmdDiscoveryFailures(t *testing.T) {
	emptyPath := t.TempDir()
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", emptyPath)
	if _, err := Discover(t.TempDir()); err == nil {
		t.Fatal("Discover accepted a missing Git executable")
	}

	t.Setenv("PATH", originalPath)
	// Windows cannot remove a directory the process is inside.
	if runtime.GOOS != "windows" {
		removedDirectory := t.TempDir()
		t.Chdir(removedDirectory)
		if err := os.RemoveAll(removedDirectory); err != nil {
			t.Fatal(err)
		}
		if _, discoverErr := Discover("."); discoverErr == nil {
			t.Fatal("Discover accepted a removed working directory")
		}
	}

	root := t.TempDir()
	t.Setenv("COVERAGE_DISCOVER_ROOT", root)
	installCoverageGit(t, `
if [ "$2" = "--show-toplevel" ]; then
	printf '%s\n' "$COVERAGE_DISCOVER_ROOT"
	exit 0
fi
exit 2
`)
	if _, err := Discover(root); err == nil {
		t.Fatal("Discover accepted a failed Git directory lookup")
	}

	installCoverageGit(t, `
if [ "$2" = "--show-toplevel" ]; then
	printf '%s\n' "$COVERAGE_DISCOVER_ROOT"
	exit 0
fi
if [ "$3" = "--git-dir" ]; then
	printf '%s/.git\n' "$COVERAGE_DISCOVER_ROOT"
	exit 0
fi
exit 2
`)
	if _, err := Discover(root); err == nil {
		t.Fatal("Discover accepted a failed common directory lookup")
	}
}

func installCoverageGit(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "git")
	if err := os.WriteFile(path, []byte(coverageShell+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func TestGitcmdWorktreeFailures(t *testing.T) {
	if _, _, _, err := coverageOutputRepo(t, "", "", 1).worktreeFile(coverageFileName, 0); err == nil {
		t.Fatal("worktreeFile accepted a zero limit")
	}
	if _, _, _, err := coverageOutputRepo(t, "", "", 1).worktreeFile("../file", 10); err == nil {
		t.Fatal("worktreeFile accepted an escaping path")
	}
	if _, _, _, err := coverageOutputRepo(t, "", "", 2).WorktreeFile(coverageFileName); err == nil {
		t.Fatal("WorktreeFile accepted an ignored-path command failure")
	}

	longPath := strings.Repeat("a", 4096)
	if _, _, _, err := coverageOutputRepo(t, "", "", 1).WorktreeFile(longPath); err == nil {
		t.Fatal("WorktreeFile accepted a path that cannot be statted")
	}

	repo := coverageOutputRepo(t, "", "", 1)
	if err := os.Mkdir(filepath.Join(repo.Root, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repo.WorktreeFile("directory"); err == nil {
		t.Fatal("WorktreeFile accepted a directory")
	}

	outside := t.TempDir()
	writeFile(t, outside, coverageFileName, "outside\n")
	if err := os.Symlink(outside, filepath.Join(repo.Root, "outside-link")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repo.WorktreeFile("outside-link/file"); err == nil {
		t.Fatal("WorktreeFile accepted a path through an escaping directory symlink")
	}

	t.Run("protected file", func(t *testing.T) {
		// Root reads through mode 0, and Windows maps it to a read-only
		// attribute that does not block reads either.
		if runtime.GOOS != "windows" && os.Geteuid() == 0 {
			t.Skip("root ignores file permissions")
		}
		protected := filepath.Join(repo.Root, "protected")
		if err := os.WriteFile(protected, []byte("secret\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(protected, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(protected, 0o600)
		})
		if _, _, _, err := repo.WorktreeFile("protected"); err == nil {
			t.Fatal("WorktreeFile opened a protected file")
		}
	})

	if _, err := NormalizePath(""); err == nil {
		t.Fatal("NormalizePath accepted an empty path")
	}
	if _, err := NormalizePath("bad\x00path"); err == nil {
		t.Fatal("NormalizePath accepted a NUL path")
	}
	if _, err := NormalizePath("/outside"); err == nil {
		t.Fatal("NormalizePath accepted an absolute path")
	}
}

func TestGitcmdSnapshotFailures(t *testing.T) {
	repo := coverageOutputRepo(t, "", "", 1)
	if _, err := repo.SnapshotWorktree("../file"); err == nil {
		t.Fatal("SnapshotWorktree accepted an invalid path")
	}
	if snapshot, err := repo.SnapshotWorktree("missing"); err != nil || snapshot.Exists {
		t.Fatalf("SnapshotWorktree(missing) = %+v, %v", snapshot, err)
	}
	if _, _, err := repo.SnapshotWorktreeWithLimit("../file", 10); err == nil {
		t.Fatal("SnapshotWorktreeWithLimit accepted an invalid path")
	}

	writeFile(t, repo.Root, coverageFileName, "content\n")
	hashFailure := coverageRepo(t, `
if [ "$1" = "check-ignore" ]; then
	exit 1
fi
exit 2
`)
	writeFile(t, hashFailure.Root, coverageFileName, "content\n")
	if content, exists, _, err := hashFailure.WorktreeFile(coverageFileName); err != nil || !exists {
		t.Fatalf("WorktreeFile(hash failure fixture) = %q, %t, %v", content, exists, err)
	}
	if _, err := hashFailure.HashBytes([]byte("content\n")); err == nil {
		t.Fatal("HashBytes accepted a failed hash")
	}
	if _, err := hashFailure.SnapshotWorktree(coverageFileName); err == nil {
		t.Fatal("SnapshotWorktree accepted a failed hash")
	}
	if _, _, err := hashFailure.SnapshotWorktreeWithLimit(coverageFileName, 100); err == nil {
		t.Fatal("SnapshotWorktreeWithLimit accepted a failed hash")
	}
}

func TestGitcmdCwdErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows cannot remove a directory the process is inside")
	}
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	removedDirectory := t.TempDir()
	t.Chdir(removedDirectory)
	if err := os.RemoveAll(removedDirectory); err != nil {
		t.Fatal(err)
	}
	_, _, cwdErr := (&Repo{Root: oldWorkingDirectory}).CwdRelative(coverageFileName)
	if cwdErr == nil {
		t.Fatal("CwdRelative accepted a removed working directory")
	}
}

func TestGitcmdWorktreeMatchAndStashFailures(t *testing.T) {
	matchFailure := coverageRepo(t, `
case "$1" in
ls-tree)
	printf '%b' '100644 blob `+coverageOID+`\tfile\000'
	exit 0
	;;
check-ignore)
	exit 2
	;;
esac
exit 2
`)
	writeFile(t, matchFailure.Root, coverageFileName, "content\n")
	if blob, exists, err := matchFailure.BlobID(coverageOID, coverageFileName); err != nil || !exists {
		t.Fatalf("BlobID(match failure fixture) = %q, %t, %v", blob, exists, err)
	}
	if _, _, _, err := matchFailure.WorktreeFile(coverageFileName); err == nil {
		t.Fatal("WorktreeFile accepted a failed ignored-path check")
	}
	if _, err := matchFailure.WorktreeMatchesRevision(coverageOID, coverageFileName); err == nil {
		t.Fatal("WorktreeMatchesRevision accepted a failed worktree read")
	}

	readBlobFailure := coverageRepo(t, `
case "$1" in
ls-tree)
	printf '%b' '100644 blob `+coverageOID+`\tfile\000'
	exit 0
	;;
check-ignore)
	exit 1
	;;
cat-file)
	exit 2
	;;
esac
exit 2
`)
	writeFile(t, readBlobFailure.Root, coverageFileName, "content\n")
	if _, err := readBlobFailure.WorktreeMatchesRevision(coverageOID, coverageFileName); err == nil {
		t.Fatal("WorktreeMatchesRevision accepted a failed blob read")
	}

	stashFailure := coverageRepo(t, `
if [ "$1" = "rev-list" ]; then
	printf '`+coverageOID+` `+coverageOID2+`'
	exit 0
fi
exit 2
`)
	if _, err := stashFailure.StashPaths(coverageOID); err == nil {
		t.Fatal("StashPaths accepted a failed change listing")
	}
}

func TestGitcmdDirtyPathMalformedOutput(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantError bool
		wantPaths []string
	}{
		{name: "short status", output: "M", wantError: true},
		{name: "invalid path", output: "M  ../file\\000", wantError: true},
		{name: "truncated rename", output: "R  old\\000", wantError: true},
		{name: "invalid renamed path", output: "R  old\\000../new\\000", wantError: true},
		{name: "renamed path", output: "R  old\\000new\\000", wantPaths: []string{"new", "old"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			repo := coverageOutputRepo(t, test.output, "", 0)
			paths, err := repo.DirtyPaths()
			if (err != nil) != test.wantError {
				t.Fatalf("DirtyPaths() error = %v, want error: %t", err, test.wantError)
			}
			if test.wantPaths != nil && !reflect.DeepEqual(paths, test.wantPaths) {
				t.Fatalf("DirtyPaths() = %v, want %v", paths, test.wantPaths)
			}
		})
	}
}

func TestGitcmdCommitOutputFailures(t *testing.T) {
	if _, err := coverageOutputRepo(t, "", "", 0).CommitTime(coverageOID); err == nil {
		t.Fatal("CommitTime accepted empty output")
	}
	if _, err := coverageOutputRepo(t, "not a timestamp", "", 0).CommitTime(coverageOID); err == nil {
		t.Fatal("CommitTime accepted invalid output")
	}
	if _, _, err := coverageOutputRepo(t, "author only", "", 0).CommitAuthor(coverageOID); err == nil {
		t.Fatal("CommitAuthor accepted malformed output")
	}
}

func TestGitcmdContainmentAndNoteErrors(t *testing.T) {
	if _, err := coverageOutputRepo(t, "", "", 2).AnyBranchContains(coverageOID); err == nil {
		t.Fatal("AnyBranchContains accepted a failed containment command")
	}
	if exists, err := coverageOutputRepo(t, "", "", 0).NewBranchScanner().commitExistsQuiet(coverageOID); err != nil || !exists {
		t.Fatalf("commitExistsQuiet(existing) = %v, %v", exists, err)
	}
	// rev-parse reports a missing commit with exit 1 and no stderr, and
	// the fsck integrity probe must succeed for the quiet missing path.
	// A uniform exit 1 fake also fails fsck, which is an error case.
	if _, err := coverageRepo(t, `
if [ "$1" = "rev-parse" ]; then
	exit 1
fi
exit 0
`).NewBranchScanner().commitExistsQuiet(coverageOID); err != nil {
		t.Fatalf("commitExistsQuiet(missing) = %v", err)
	}
	if _, err := coverageOutputRepo(t, "", "", 2).NewBranchScanner().commitExistsQuiet(coverageOID); err == nil {
		t.Fatal("commitExistsQuiet accepted a failed existence command")
	}
	if _, err := coverageOutputRepo(t, "", "", 2).NewBranchScanner().commitExistsQuiet(""); err == nil {
		t.Fatal("commitExistsQuiet accepted an invalid revision")
	}
	if _, err := coverageOutputRepo(t, "", "", 1).NoteCommits(coverageNotesName); err != nil {
		t.Fatalf("NoteCommits(missing) = %v", err)
	}
	if _, err := coverageOutputRepo(t, "", "", 0).NoteCommits("refs/heads/main"); err == nil {
		t.Fatal("NoteCommits accepted an invalid note ref")
	}
}

func TestGitcmdNoteAndRefBranches(t *testing.T) {
	repo := coverageOutputRepo(t, "", "", 128)
	if _, _, err := repo.RefValue(coverageBranch); err != nil {
		t.Fatalf("RefValue(missing) = %v", err)
	}
	if err := coverageOutputRepo(t, "", "", 1).DeleteNoteRef(coverageNotesName, coverageOID); err != nil {
		t.Fatalf("DeleteNoteRef(missing) = %v", err)
	}

	deleteFailure := coverageRepo(t, `
if [ "$1" = "notes" ] && [ "$3" = "show" ]; then
	printf 'note'
	exit 0
fi
exit 2
`)
	if _, err := deleteFailure.DeleteNoteRefIfEqual(coverageNotesName, coverageOID, []byte("note")); err == nil {
		t.Fatal("DeleteNoteRefIfEqual accepted a failed delete")
	}

	readFailure := coverageOutputRepo(t, "", "", 2)
	if err := readFailure.WriteNoteRef(coverageNotesName, coverageOID, []byte("note")); err == nil {
		t.Fatal("WriteNoteRef accepted a failed note read")
	}

	missingDirectory := coverageOutputRepo(t, "", "", 1)
	missingDirectory.GitDir = filepath.Join(missingDirectory.Root, "missing")
	if err := missingDirectory.WriteNoteRef(coverageNotesName, coverageOID, []byte("note")); err == nil {
		t.Fatal("WriteNoteRef accepted an unavailable note directory")
	}
}

func TestGitcmdProtectAndHistoryBranches(t *testing.T) {
	if err := coverageOutputRepo(t, "", "", 1).ProtectBlobs(nil); err != nil {
		t.Fatalf("ProtectBlobs(missing ref) = %v", err)
	}
	if _, err := coverageOutputRepo(t, "", "", 2).ProtectedBlobCount(); err == nil {
		t.Fatal("ProtectedBlobCount accepted a failed count")
	}
	if count, err := coverageOutputRepo(t, "\n", "", 0).ProtectedBlobCount(); err != nil || count != 0 {
		t.Fatalf("ProtectedBlobCount(empty) = %d, %v", count, err)
	}
	if _, err := coverageOutputRepo(t, "", "", 2).FirstParentHistory(coverageOID); err == nil {
		t.Fatal("FirstParentHistory accepted a failed history command")
	}
	if _, err := coverageOutputRepo(t, "bad", "", 0).FirstParentHistory(coverageOID); err == nil {
		t.Fatal("FirstParentHistory accepted malformed history")
	}
}

func TestGitcmdValidationHelpers(t *testing.T) {
	if err := validateNoteRef(""); err == nil {
		t.Fatal("validateNoteRef accepted an empty ref")
	}
}

func TestGitcmdConfigAndPathFailures(t *testing.T) {
	if _, _, err := coverageOutputRepo(t, "", "", 2).ConfigPath("core.hooksPath"); err == nil {
		t.Fatal("ConfigPath accepted a failed config command")
	}
	if _, err := coverageOutputRepo(t, "", "", 2).GitPath("hooks"); err == nil {
		t.Fatal("GitPath accepted a failed path command")
	}
}

func TestGitcmdMalformedVersion(t *testing.T) {
	tests := []string{"git", "git version", "git version 1", "git version x.y", "git version 1.x"}
	for _, output := range tests {
		output := output
		t.Run(output, func(t *testing.T) {
			if _, _, err := coverageOutputRepo(t, output, "", 0).Version(); err == nil {
				t.Fatalf("Version accepted %q", output)
			}
		})
	}
}

func TestGitcmdGlobalConfigFailureBranches(t *testing.T) {
	t.Setenv("PATH", installCoverageGit(t, `
if [ "$1" != "config" ]; then
	exit 2
fi
if [ "$5" = "--get-all" ]; then
	printf 'value'
	exit 0
fi
exit 0
`))
	if _, err := GlobalConfigValues("init.templateDir"); err == nil {
		t.Fatal("GlobalConfigValues accepted malformed output")
	}

	t.Setenv("PATH", installCoverageGit(t, `
exit 2
`))
	if _, err := GlobalConfigValues("init.templateDir"); err == nil {
		t.Fatal("GlobalConfigValues accepted a command failure")
	}

	t.Setenv("PATH", installCoverageGit(t, `
if [ "$5" = "--get-all" ]; then
	exit 2
fi
exit 0
`))
	if _, err := UnsetGlobalConfig("init.templateDir", "managed"); err == nil {
		t.Fatal("UnsetGlobalConfig accepted a failed remaining-value check")
	}

	t.Setenv("PATH", installCoverageGit(t, `
if [ "$3" = "--fixed-value" ]; then
	exit 5
fi
exit 2
`))
	if _, err := UnsetGlobalConfig("init.templateDir", "managed"); err == nil {
		t.Fatal("UnsetGlobalConfig accepted a failed remaining-value check")
	}

	t.Setenv("PATH", installCoverageGit(t, `
if [ "$3" = "--fixed-value" ]; then
	exit 5
fi
if [ "$5" = "--get-all" ]; then
	printf '%b' '\000'
	exit 0
fi
exit 2
`))
	if removed, err := UnsetGlobalConfig("init.templateDir", "managed"); err != nil || removed {
		t.Fatalf("UnsetGlobalConfig(no remaining value) = %v, %v", removed, err)
	}

	t.Setenv("PATH", installCoverageGit(t, `
exit 2
`))
	if removed, err := UnsetGlobalConfig("init.templateDir", "managed"); err == nil || removed {
		t.Fatalf("UnsetGlobalConfig(command failure) = %v, %v", removed, err)
	}

	t.Setenv("PATH", t.TempDir())
	if _, err := GlobalConfigValues("init.templateDir"); err == nil {
		t.Fatal("GlobalConfigValues accepted a missing Git executable")
	}
}

func TestGitcmdStderrOutputLimit(t *testing.T) {
	if err := assertOutputLimit(t, `
head -c 67108865 /dev/zero >&2
`); err != nil {
		t.Fatal(err)
	}
}

func assertOutputLimit(t *testing.T, body string) error {
	t.Helper()
	_, err := coverageRepo(t, body).run("large stderr", nil, "fake")
	if !errors.Is(err, ErrOutputLimit) {
		return err
	}
	return nil
}

func TestGitcmdCommandTimeout(t *testing.T) {
	repo := coverageRepo(t, "sleep 31\n")
	_, err := repo.run("timeout", nil, "fake")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("run(timeout) = %v", err)
	}
}
