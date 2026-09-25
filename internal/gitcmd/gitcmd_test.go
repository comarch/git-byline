package gitcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/model"
)

func TestRepositoryOperations(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Root != root || repo.GitDir == "" || repo.CommonDir == "" {
		t.Fatalf("Discover() = %+v", repo)
	}
	if head, err := repo.Head(); err != nil || head != "" {
		t.Fatalf("Head() before commit = %q, %v", head, err)
	}

	writeFile(t, root, "a file.txt", "one\n")
	snapshot, err := repo.SnapshotWorktree("a file.txt")
	if err != nil || !snapshot.Exists || snapshot.Blob == "" {
		t.Fatalf("SnapshotWorktree() = %+v, %v", snapshot, err)
	}
	first := commitAll(t, root, "first")
	if head, err := repo.Head(); err != nil || head != first {
		t.Fatalf("Head() = %q, %v, want %q", head, err, first)
	}
	if path, err := repo.GitPath("hooks"); err != nil || !filepath.IsAbs(path) {
		t.Fatalf("GitPath(hooks) = %q, %v", path, err)
	}
	if _, err := repo.GitPath(""); err == nil {
		t.Fatal("GitPath accepted empty name")
	}
	if parent, err := repo.Parent(first); err != nil || parent != "" {
		t.Fatalf("Parent(root) = %q, %v", parent, err)
	}
	if changes, err := repo.Changes(first, ""); err != nil || len(changes) != 1 || changes[0].Status != 'A' {
		t.Fatalf("Changes(root) = %+v, %v", changes, err)
	}
	blob, exists, err := repo.BlobID(first, "a file.txt")
	if err != nil || !exists || blob == "" {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	if data, err := repo.ReadBlob(blob); err != nil || string(data) != "one\n" {
		t.Fatalf("ReadBlob() = %q, %v", data, err)
	}
	if size, err := repo.BlobSize(blob); err != nil || size != 4 {
		t.Fatalf("BlobSize() = %d, %v", size, err)
	}
	if _, err := repo.BlobSize("bad"); err == nil {
		t.Fatal("BlobSize accepted invalid object ID")
	}
	if _, exists, err := repo.BlobID(first, "missing"); err != nil || exists {
		t.Fatalf("BlobID(missing) exists = %t, error = %v", exists, err)
	}
	if _, ok, err := repo.ReadNote(first); err != nil || ok {
		t.Fatalf("ReadNote(missing) = %t, %v", ok, err)
	}

	writeFile(t, root, "a file.txt", "one\ntwo\n")
	second := commitAll(t, root, "second")
	parents, err := repo.Parents(second)
	if err != nil || len(parents) != 1 || parents[0] != first {
		t.Fatalf("Parents() = %v, %v", parents, err)
	}
	changes, err := repo.Changes(second, first)
	if err != nil || len(changes) != 1 || changes[0].Path != "a file.txt" {
		t.Fatalf("Changes() = %+v, %v", changes, err)
	}
	if message, err := repo.CommitMessage(second); err != nil || !strings.Contains(message, "second") {
		t.Fatalf("CommitMessage() = %q, %v", message, err)
	}
	if previous, found, err := repo.PreviousHead(); err != nil || !found || previous != first {
		t.Fatalf("PreviousHead() = %q, %t, %v", previous, found, err)
	}
	history, err := repo.FirstParentHistory(second)
	if err != nil || len(history) != 2 {
		t.Fatalf("FirstParentHistory() = %v, %v", history, err)
	}

	note := []byte("{\"version\":1,\"files\":{}}\n")
	if err := repo.WriteNote(second, note); err != nil {
		t.Fatal(err)
	}
	if data, ok, err := repo.ReadNote(second); err != nil || !ok || strings.TrimSpace(string(data)) != strings.TrimSpace(string(note)) {
		t.Fatalf("ReadNote() = %q, %t, %v", data, ok, err)
	}
	if err := repo.WriteNote(second, note); err != nil {
		t.Fatalf("idempotent WriteNote: %v", err)
	}
	if err := repo.WriteNote(second, []byte("different\n")); err == nil {
		t.Fatal("WriteNote overwrote a different note")
	}

	if err := repo.ProtectBlobs([]string{blob, blob}); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ProtectedBlobCount(); err != nil || count != 1 {
		t.Fatalf("ProtectedBlobCount() = %d, %v", count, err)
	}
	if err := repo.ProtectBlobs(nil); err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ProtectedBlobCount(); err != nil || count != 0 {
		t.Fatalf("ProtectedBlobCount() after delete = %d, %v", count, err)
	}
	major, minor, err := repo.Version()
	if err != nil || major < 2 || minor < 0 {
		t.Fatalf("Version() = %d.%d, %v", major, minor, err)
	}
	if major == 0 && minor == 0 {
		t.Fatal("Version() returned an empty version")
	}

	if err := os.Rename(filepath.Join(root, "a file.txt"), filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	third := commitAll(t, root, "rename")
	changes, err = repo.Changes(third, second)
	if err != nil || len(changes) != 1 || changes[0].Status != 'R' ||
		changes[0].OldPath != "a file.txt" || changes[0].Path != "renamed.txt" {
		t.Fatalf("Changes(rename) = %+v, %v", changes, err)
	}
}

func TestExtendedRepositoryOperations(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	first := commitAll(t, root, "first")
	writeFile(t, root, "file.txt", "one\ntwo\n")
	second := commitAll(t, root, "second")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "rev-list",
			run: func() error {
				commits, err := repo.RevList(first, second, 10)
				if err != nil {
					return err
				}
				if len(commits) != 1 || commits[0] != second {
					return fmt.Errorf("commits = %v", commits)
				}
				return nil
			},
		},
		{
			name: "patch-id",
			run: func() error {
				value, err := repo.PatchID(second)
				if err != nil {
					return err
				}
				if !model.ValidObjectID(value) {
					return fmt.Errorf("patch ID = %q", value)
				}
				return nil
			},
		},
		{
			name: "commit-time",
			run: func() error {
				value, err := repo.CommitTime(second)
				if err != nil {
					return err
				}
				if !strings.Contains(value, "T") {
					return fmt.Errorf("commit time = %q", value)
				}
				return nil
			},
		},
		{
			name: "merge-base",
			run: func() error {
				value, err := repo.MergeBase(first, second)
				if err != nil {
					return err
				}
				if value != first {
					return fmt.Errorf("merge base = %q, want %q", value, first)
				}
				return nil
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); err != nil {
				t.Fatal(err)
			}
		})
	}

	note := []byte("{\"version\":1,\"files\":{}}\n")
	if err := repo.WriteNoteRef("refs/notes/custom", second, note); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := repo.ReadNoteRef("refs/notes/custom", second); err != nil || !ok || string(got) != string(note) {
		t.Fatalf("ReadNoteRef() = %q, %t, %v", got, ok, err)
	}
	commits, err := repo.NoteCommits("refs/notes/custom")
	if err != nil || len(commits) != 1 || commits[0] != second {
		t.Fatalf("NoteCommits() = %v, %v", commits, err)
	}
	if value, found, err := repo.RefValue("refs/notes/custom"); err != nil || !found || value == "" {
		t.Fatalf("RefValue() = %q, %t, %v", value, found, err)
	}
	if err := repo.DeleteNoteRef("refs/notes/custom", second); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.ReadNoteRef("refs/notes/custom", second); err != nil || found {
		t.Fatalf("deleted custom note = %t, %v", found, err)
	}

	writeFile(t, root, "z-dirty", "dirty\n")
	writeFile(t, root, "a-dirty", "dirty\n")
	paths, err := repo.DirtyPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"a-dirty", "z-dirty"}) {
		t.Fatalf("DirtyPaths() = %v", paths)
	}
}

func TestRepositoryBoundaryOperations(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	first := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("empty patch has no patch ID", func(t *testing.T) {
		runGit(t, root, "commit", "--allow-empty", "-m", "empty")
		empty, err := repo.Head()
		if err != nil {
			t.Fatal(err)
		}
		patch, err := repo.PatchID(empty)
		if err != nil {
			t.Fatal(err)
		}
		if patch != "" {
			t.Fatalf("PatchID(empty) = %q, want empty", patch)
		}
	})

	t.Run("patch IDs distinguish commits", func(t *testing.T) {
		same, err := repo.PatchID(first)
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, root, "file.txt", "one\ntwo\n")
		second := commitAll(t, root, "second")
		different, err := repo.PatchID(second)
		if err != nil {
			t.Fatal(err)
		}
		if same == different || !model.ValidObjectID(same) || !model.ValidObjectID(different) {
			t.Fatalf("PatchID() = %q, %q", same, different)
		}
		identical, err := repo.PatchID(second)
		if err != nil || identical != different {
			t.Fatalf("repeated PatchID() = %q, %v; want %q", identical, err, different)
		}
	})

	t.Run("commit time and merge base", func(t *testing.T) {
		value, err := repo.CommitTime(first)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			if _, nanoErr := time.Parse(time.RFC3339Nano, value); nanoErr != nil {
				t.Fatalf("CommitTime() = %q, %v", value, err)
			}
		}
		if base, err := repo.MergeBase(first, first); err != nil || base != first {
			t.Fatalf("MergeBase(same) = %q, %v", base, err)
		}
		otherRoot := initRepository(t)
		writeFile(t, otherRoot, "other.txt", "other\n")
		other := commitAll(t, otherRoot, "other")
		if _, err := repo.MergeBase(first, other); err == nil {
			t.Fatal("MergeBase accepted unrelated histories")
		}
	})

	t.Run("note commits empty and populated", func(t *testing.T) {
		empty, err := repo.NoteCommits("refs/notes/empty")
		if err != nil || len(empty) != 0 {
			t.Fatalf("empty NoteCommits() = %v, %v", empty, err)
		}
		if err := repo.WriteNoteRef("refs/notes/empty", first, []byte("note\n")); err != nil {
			t.Fatal(err)
		}
		populated, err := repo.NoteCommits("refs/notes/empty")
		if err != nil || !reflect.DeepEqual(populated, []string{first}) {
			t.Fatalf("populated NoteCommits() = %v, %v", populated, err)
		}
	})

	t.Run("snapshot byte budget", func(t *testing.T) {
		writeFile(t, root, "snapshot.txt", "1234")
		snapshot, used, err := repo.SnapshotWorktreeWithLimit("snapshot.txt", 4)
		if err != nil || !snapshot.Exists || snapshot.Blob == "" || used != 4 {
			t.Fatalf("SnapshotWorktreeWithLimit(limit) = %+v, %d, %v", snapshot, used, err)
		}
		if _, _, err := repo.SnapshotWorktreeWithLimit("snapshot.txt", 3); !errors.Is(err, ErrSnapshotBudget) {
			t.Fatalf("SnapshotWorktreeWithLimit(over limit) = %v", err)
		}
		missing, used, err := repo.SnapshotWorktreeWithLimit("missing.txt", 4)
		if err != nil || missing.Exists || used != 0 || missing.Path != "missing.txt" {
			t.Fatalf("SnapshotWorktreeWithLimit(missing) = %+v, %d, %v", missing, used, err)
		}
		if _, _, err := repo.SnapshotWorktreeWithLimit("snapshot.txt", 0); !errors.Is(err, ErrSnapshotBudget) {
			t.Fatalf("SnapshotWorktreeWithLimit(zero) = %v", err)
		}
	})
}

func TestRewriteRepositoryHelpers(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	first := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if length, err := repo.ObjectIDLength(); err != nil || length != 40 {
		t.Fatalf("ObjectIDLength() = %d, %v", length, err)
	}
	if ref, attached, err := repo.CurrentBranchRef(); err != nil ||
		!attached || !strings.HasPrefix(ref, "refs/heads/") {
		t.Fatalf("CurrentBranchRef() = %q, %t, %v", ref, attached, err)
	}
	runGit(t, root, "checkout", "--detach", "-q", first)
	if ref, attached, err := repo.CurrentBranchRef(); err != nil || attached || ref != "" {
		t.Fatalf("detached CurrentBranchRef() = %q, %t, %v", ref, attached, err)
	}
	if matches, err := repo.WorktreeMatchesRevision(first, "file.txt"); err != nil || !matches {
		t.Fatalf("WorktreeMatchesRevision(clean) = %t, %v", matches, err)
	}
	writeFile(t, root, "file.txt", "changed\n")
	if matches, err := repo.WorktreeMatchesRevision(first, "file.txt"); err != nil || matches {
		t.Fatalf("WorktreeMatchesRevision(dirty) = %t, %v", matches, err)
	}
	if matches, err := repo.WorktreeMatchesRevision(first, "missing.txt"); err != nil || !matches {
		t.Fatalf("WorktreeMatchesRevision(both missing) = %t, %v", matches, err)
	}

	runGit(t, root, "stash", "push", "-qm", "helper stash")
	stash := strings.TrimSpace(runGit(t, root, "rev-parse", "refs/stash"))
	paths, err := repo.StashPaths(stash)
	if err != nil || !reflect.DeepEqual(paths, []string{"file.txt"}) {
		t.Fatalf("StashPaths() = %v, %v", paths, err)
	}
	if applied, err := repo.StashApplied(stash, nil); err != nil || applied {
		t.Fatalf("StashApplied(empty) = %t, %v", applied, err)
	}
	if applied, err := repo.StashApplied(stash, paths); err != nil || applied {
		t.Fatalf("StashApplied(before apply) = %t, %v", applied, err)
	}
	runGit(t, root, "stash", "apply", "-q", stash)
	if applied, err := repo.StashApplied(stash, paths); err != nil || !applied {
		t.Fatalf("StashApplied(after apply) = %t, %v", applied, err)
	}

	note := []byte("note\n")
	if err := repo.WriteNoteRef("refs/notes/helper", first, note); err != nil {
		t.Fatal(err)
	}
	if deleted, err := repo.DeleteNoteRefIfEqual("refs/notes/helper", first, []byte("other\n")); err != nil || deleted {
		t.Fatalf("DeleteNoteRefIfEqual(mismatch) = %t, %v", deleted, err)
	}
	if deleted, err := repo.DeleteNoteRefIfEqual("refs/notes/helper", first, note); err != nil || !deleted {
		t.Fatalf("DeleteNoteRefIfEqual(match) = %t, %v", deleted, err)
	}
}

func TestReadNoteRefEnforcesNoteOutputLimit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fake git is not executable on Windows")
	}
	// This test emits 16 MiB, so keep it serial with other Git integration tests.
	root := t.TempDir()
	gitBin := filepath.Join(root, "fake-git")
	if err := os.WriteFile(
		gitBin,
		[]byte("#!/bin/sh\nhead -c 16777217 /dev/zero\n"),
		0o700,
	); err != nil {
		t.Fatal(err)
	}
	repo := &Repo{Root: root, gitBin: gitBin}
	_, _, err := repo.ReadNoteRef(
		"refs/notes/test",
		strings.Repeat("a", 40),
	)
	if !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("ReadNoteRef(oversized) = %v, want %v", err, ErrOutputLimit)
	}
}

func TestDirtyPathsIncludesAllStatuses(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "staged.txt", "staged\n")
	writeFile(t, root, "unstaged.txt", "one\n")
	writeFile(t, root, "renamed.txt", "rename\n")
	writeFile(t, root, "deleted.txt", "delete\n")
	commitAll(t, root, "base")

	writeFile(t, root, "staged.txt", "staged\nchanged\n")
	runGit(t, root, "add", "staged.txt")
	writeFile(t, root, "unstaged.txt", "one\nchanged\n")
	writeFile(t, root, "untracked.txt", "untracked\n")
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "renamed.txt"), filepath.Join(root, "renamed-to.txt")); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "-A", "--", "renamed-to.txt")
	paths, err := func() ([]string, error) {
		repo, err := Discover(root)
		if err != nil {
			return nil, err
		}
		return repo.DirtyPaths()
	}()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"deleted.txt", "renamed-to.txt", "renamed.txt", "staged.txt", "unstaged.txt", "untracked.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("DirtyPaths() = %v, want %v", paths, want)
	}
}

func TestDirtyPathsDisablesFSMonitor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX helper script")
	}
	root := t.TempDir()
	argsPath := filepath.Join(root, "args")
	script := filepath.Join(root, "git")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$BYLINE_TEST_ARGS\"\nprintf '?? file\\000'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BYLINE_TEST_ARGS", argsPath)
	repo := &Repo{Root: root, gitBin: script}
	paths, err := repo.DirtyPaths()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths, []string{"file"}) {
		t.Fatalf("DirtyPaths() = %v", paths)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	want := []string{"-c", "core.fsmonitor=false", "status", "--porcelain=v1", "-z", "-uall"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DirtyPaths arguments = %v, want %v", got, want)
	}
}

func TestWorktreePathSafety(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".gitignore", "ignored.txt\n")
	writeFile(t, root, "ignored.txt", "secret\n")
	if _, _, _, err := repo.WorktreeFile("ignored.txt"); err == nil {
		t.Fatal("WorktreeFile accepted ignored path")
	}
	if _, err := repo.NormalizeWorktreePath(filepath.Join(root, "file.go")); err != nil {
		t.Fatalf("NormalizeWorktreePath absolute: %v", err)
	}
	testWorktreePathAliases(t, repo, root)
	for _, path := range []string{"", "../outside", ".git/config", ".GIT/config", "dir/.git/config", "/outside", "bad\npath", "bad\x1bpath"} {
		if _, err := repo.NormalizeWorktreePath(path); err == nil {
			t.Fatalf("NormalizeWorktreePath accepted %q", path)
		}
	}
	testWorktreePathSymlinks(t, repo, root)
	if _, err := repo.ReadBlob("bad"); err == nil {
		t.Fatal("ReadBlob accepted invalid object ID")
	}
	if err := repo.ProtectBlobs([]string{"bad"}); err == nil {
		t.Fatal("ProtectBlobs accepted invalid object ID")
	}
	if data, exists, normalized, err := repo.WorktreeFile("missing"); err != nil || exists || data != nil || normalized != "missing" {
		t.Fatalf("WorktreeFile(missing) = %q, %t, %q, %v", data, exists, normalized, err)
	}
	writeFile(t, root, "binary", "a\x00b")
	if _, _, _, err := repo.WorktreeFile("binary"); err == nil {
		t.Fatal("WorktreeFile accepted binary content")
	}
	writeFile(t, root, "many.txt", strings.Repeat("\n", model.MaxTextLines+1))
	if _, _, _, err := repo.WorktreeFile("many.txt"); err == nil {
		t.Fatal("WorktreeFile accepted excessive line count")
	}
	if _, err := repo.Parent("bad"); err == nil {
		t.Fatal("Parent accepted invalid object ID")
	}
}

func testWorktreePathAliases(t *testing.T, repo *Repo, root string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	alias := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "aliased.txt", "content\n")
	tests := []struct {
		name string
		path string
		want string
	}{
		{"existing file", filepath.Join(alias, "aliased.txt"), "aliased.txt"},
		{"missing parent", filepath.Join(alias, "new", "file.txt"), "new/file.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := repo.NormalizeWorktreePath(test.path)
			if err != nil || normalized != test.want {
				t.Fatalf("NormalizeWorktreePath(%q) = %q, %v; want %q", test.path, normalized, err, test.want)
			}
		})
	}
	data, exists, normalized, err := repo.WorktreeFile(filepath.Join(alias, "aliased.txt"))
	if err != nil || !exists || normalized != "aliased.txt" || string(data) != "content\n" {
		t.Fatalf("WorktreeFile(alias) = %q, %t, %q, %v", data, exists, normalized, err)
	}
	outside := t.TempDir()
	writeFile(t, outside, "outside.txt", "outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "alias-escape")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(alias, "alias-escape", "outside.txt"),
		filepath.Join(alias, "alias-escape", "missing.txt"),
	} {
		if _, err := repo.NormalizeWorktreePath(path); err == nil {
			t.Fatalf("NormalizeWorktreePath accepted escaping alias %q", path)
		}
	}
	danglingEscape := filepath.Join(root, "dangling-escape")
	if err := os.Symlink(filepath.Join(outside, "missing"), danglingEscape); err != nil {
		t.Fatal(err)
	}
	danglingPath := filepath.Join(alias, "dangling-escape", "file.txt")
	if _, err := repo.NormalizeWorktreePath(danglingPath); err == nil {
		t.Fatalf("NormalizeWorktreePath accepted dangling escape %q", danglingPath)
	}
	if _, _, _, err := repo.WorktreeFile(danglingPath); err == nil {
		t.Fatalf("WorktreeFile accepted dangling escape %q", danglingPath)
	}
}

func testWorktreePathSymlinks(t *testing.T, repo *Repo, root string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	outside := filepath.Join(t.TempDir(), "outside")
	writeFile(t, filepath.Dir(outside), filepath.Base(outside), "outside\n")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repo.WorktreeFile("link"); err == nil {
		t.Fatal("WorktreeFile accepted escaping symlink")
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := repo.WorktreeFile("broken"); err == nil {
		t.Fatal("WorktreeFile accepted broken symlink")
	}
}

func TestDiscoverIgnoresGitEnvironment(t *testing.T) {
	root := initRepository(t)
	other := initRepository(t)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Root != root {
		t.Fatalf("Discover() root = %q, want %q", repo.Root, root)
	}
}

func TestDiscoverAndConfigErrors(t *testing.T) {
	t.Parallel()
	if _, err := Discover(t.TempDir()); err == nil {
		t.Fatal("Discover succeeded outside a repository")
	}
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if value, ok, err := repo.ConfigPath("core.hooksPath"); err != nil || ok || value != "" {
		t.Fatalf("ConfigPath absent = %q, %t, %v", value, ok, err)
	}
	runGit(t, root, "config", "core.hooksPath", ".hooks")
	if value, ok, err := repo.ConfigPath("core.hooksPath"); err != nil || !ok || value != ".hooks" {
		t.Fatalf("ConfigPath set = %q, %t, %v", value, ok, err)
	}
}

func TestRepositoryClassificationHelpers(t *testing.T) {
	t.Parallel()
	if _, err := Discover(t.TempDir()); err == nil || !IsNotRepository(err) {
		t.Fatalf("IsNotRepository() = false for error %v", err)
	}
	if IsNotRepository(errors.New("not a repository")) {
		t.Fatal("IsNotRepository accepted an unrelated error")
	}

	underlying := errors.New("underlying")
	commandErr := &CommandError{
		Operation: "operation",
		ExitCode:  1,
		Err:       underlying,
	}
	if !errors.Is(commandErr, underlying) {
		t.Fatal("CommandError did not unwrap underlying error")
	}
	if got := commandErr.Error(); !strings.Contains(got, "operation") ||
		!strings.Contains(got, "exit code 1") {
		t.Fatalf("CommandError.Error() = %q", got)
	}
	withStderr := &CommandError{Operation: "operation", ExitCode: 2, Stderr: "stderr"}
	if got := withStderr.Error(); !strings.Contains(got, "stderr") {
		t.Fatalf("CommandError.Error() with stderr = %q", got)
	}
}

func TestNoteWriteError(t *testing.T) {
	t.Parallel()
	identity := &CommandError{
		Operation: "write attribution note",
		ExitCode:  128,
		Stderr:    "Author identity unknown\n\n*** Please tell me who you are.",
	}
	other := &CommandError{
		Operation: "write attribution note",
		ExitCode:  128,
		Stderr:    "fatal: bad object HEAD",
	}
	plain := errors.New("disk full")
	if err := noteWriteError(nil); err != nil {
		t.Fatalf("noteWriteError(nil) = %v", err)
	}
	wrapped := noteWriteError(identity)
	if wrapped == identity || !strings.Contains(wrapped.Error(), "set user.name and user.email with git config") {
		t.Fatalf("noteWriteError(identity) = %v", wrapped)
	}
	if !errors.Is(wrapped, identity) {
		t.Fatalf("noteWriteError(identity) unwraps to %v", wrapped)
	}
	if err := noteWriteError(other); err != other {
		t.Fatalf("noteWriteError(other) = %v", err)
	}
	if err := noteWriteError(plain); err != plain {
		t.Fatalf("noteWriteError(plain) = %v", err)
	}
}

func TestValidateNoteRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		ref string
		err bool
	}{
		{ref: "refs/notes"},
		{ref: "refs/notes/byline"},
		{ref: "refs/notes/team/byline"},
		{ref: "refs/heads/main", err: true},
		{ref: "refs/tags/v1", err: true},
		{ref: "refs/remotes/origin/main", err: true},
		{ref: "refs/notes/../../heads/main", err: true},
		{ref: "refs/notes/feature/../main", err: true},
		{ref: "refs/notes/", err: true},
		{ref: "refs/notes/foo bar", err: true},
		{ref: "refs/notes/foo~bar", err: true},
		{ref: "refs/notes/foo..bar", err: true},
		{ref: "refs/notes/foo@{bar}", err: true},
		{ref: "refs/notes//nested", err: true},
		{ref: "refs/notes/foo.lock", err: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.ref, func(t *testing.T) {
			if err := validateNoteRef(test.ref); (err != nil) != test.err {
				t.Fatalf("validateNoteRef(%q) error = %v, want error: %t", test.ref, err, test.err)
			}
		})
	}
}

func TestIsMissingIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		stderr string
		want   bool
	}{
		{"fatal: unable to auto-detect email address (got 'root@host.(none)')", true},
		{"Author identity unknown\n\n*** Please tell me who you are.", true},
		{"error: cannot add note: something else", false},
		{"", false},
	} {
		if got := isMissingIdentity(test.stderr); got != test.want {
			t.Fatalf("isMissingIdentity(%q) = %t, want %t", test.stderr, got, test.want)
		}
	}
}
func TestCommandError(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.run("invalid operation", nil, "rev-parse", "--verify", "not-a-ref")
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.ExitCode == 0 || !strings.Contains(commandErr.Error(), "invalid operation") {
		t.Fatalf("error = %#v", err)
	}
}

func TestValidateRevision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		want  bool
	}{
		{value: "HEAD"},
		{value: "main~2"},
		{value: "", want: true},
		{value: "-bad", want: true},
		{value: "has space", want: true},
		{value: "has\nnewline", want: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.value, func(t *testing.T) {
			if err := validateRevision(test.value, "revision"); (err != nil) != test.want {
				t.Fatalf("validateRevision(%q) error = %v, want error: %t", test.value, err, test.want)
			}
		})
	}
}

func TestTempFileAndLimitedBuffer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path, err := tempFile(dir, "test-", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "payload" {
		t.Fatalf("tempFile() = %q, %v", data, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := tempFile(filepath.Join(dir, "missing"), "test-", []byte("payload")); err == nil {
		t.Fatal("tempFile accepted an unavailable directory")
	}

	var buffer limitedBuffer
	data = make([]byte, maxOutputBytes+1)
	if written, err := buffer.Write(data); err != nil || written != len(data) || !buffer.exceeded {
		t.Fatalf("limitedBuffer.Write(oversized) = %d, %v, exceeded=%t", written, err, buffer.exceeded)
	}
	if written, err := buffer.Write([]byte("x")); err != nil || written != 1 {
		t.Fatalf("limitedBuffer.Write(after limit) = %d, %v", written, err)
	}
	if len(buffer.Bytes()) != maxOutputBytes || len(buffer.String()) != maxOutputBytes {
		t.Fatalf("limitedBuffer size = %d", len(buffer.Bytes()))
	}
}

func TestLimitedBufferOutputError(t *testing.T) {
	t.Parallel()
	const operation = "read"
	small := limitedBuffer{limit: 2}
	if _, err := small.Write([]byte("ab")); err != nil || small.outputError(operation) != nil {
		t.Fatalf("limitedBuffer at its limit = %v, %v", err, small.outputError(operation))
	}
	if _, err := small.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	err := small.outputError(operation)
	if !errors.Is(err, ErrOutputLimit) || !strings.Contains(err.Error(), "git read:") || !strings.Contains(err.Error(), " 2 bytes") {
		t.Fatalf("limitedBuffer over its limit = %v", err)
	}
	unset := limitedBuffer{exceeded: true}
	if err := unset.outputError(operation); err == nil || !strings.Contains(err.Error(), fmt.Sprint(maxOutputBytes)) {
		t.Fatalf("limitedBuffer without a limit = %v, want the default limit", err)
	}
}

func TestRecordWriter(t *testing.T) {
	t.Parallel()
	const operation = "list"
	var records []string
	writer := &recordWriter{operation: operation, delimiter: []byte{0}, handle: func(record string) error {
		records = append(records, record)
		return nil
	}}
	full := strings.Repeat("x", maxRecordBytes)
	for _, chunk := range []string{"a", "b\x00\x00c\x00" + full, "\x00d"} {
		if written, err := writer.Write([]byte(chunk)); err != nil || written != len(chunk) {
			t.Fatalf("Write(%d bytes) = %d, %v", len(chunk), written, err)
		}
	}
	if err := writer.finish(); err != nil || len(records) != 4 ||
		records[0] != "ab" || records[1] != "c" || records[2] != full || records[3] != "d" {
		t.Fatalf("finish() = %v with %d records, want ab, c, one full record, d", err, len(records))
	}

	stop := errors.New("stop")
	stopped := &recordWriter{operation: operation, delimiter: []byte{'\n'}, handle: func(string) error { return stop }}
	for _, chunk := range []string{"x\ny\n", "z\n"} {
		if written, err := stopped.Write([]byte(chunk)); written != 0 || !errors.Is(err, stop) {
			t.Fatalf("Write(%q) after a handle error = %d, %v", chunk, written, err)
		}
	}
	if err := stopped.outputError(operation); !errors.Is(err, stop) {
		t.Fatalf("outputError() = %v, want %v", err, stop)
	}
}

func TestRecordWriterLimit(t *testing.T) {
	t.Parallel()
	full := strings.Repeat("x", maxRecordBytes)
	for name, chunks := range map[string][]string{
		"in one write":  {full + "x\n"},
		"across writes": {full, "x"},
	} {
		writer := &recordWriter{operation: "list", delimiter: []byte{'\n'}, handle: func(string) error { return nil }}
		var err error
		for _, chunk := range chunks {
			_, err = writer.Write([]byte(chunk))
		}
		if !errors.Is(err, ErrOutputLimit) || !strings.Contains(err.Error(), "git list: record over") {
			t.Errorf("%s: Write() = %v, want %v", name, err, ErrOutputLimit)
		}
	}
}

func TestStreamRecordsStopsGit(t *testing.T) {
	stop := errors.New("stop")
	records := 0
	// yes never ends on its own, so only the broken pipe can stop it.
	err := coverageRepo(t, "yes line\n").streamRecords("stream lines", '\n', func(string) error {
		records++
		return stop
	})
	if !errors.Is(err, stop) || records != 1 {
		t.Fatalf("streamRecords() = %v after %d records, want %v after 1", err, records, stop)
	}
}

func TestCommitAuthorReadsNameAndEmail(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	runGit(t, root, "config", "user.name", "John Doe")
	runGit(t, root, "config", "user.email", "john.doe@example.invalid")
	writeFile(t, root, "file.txt", "one\n")
	commit := commitAll(t, root, "one")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	name, email, err := repo.CommitAuthor(commit)
	if err != nil || name != "John Doe" || email != "john.doe@example.invalid" {
		t.Fatalf("CommitAuthor() = %q, %q, %v", name, email, err)
	}
	if got := model.NormalizeIdentity(name, email); got != "john.doe" {
		t.Fatalf("NormalizeIdentity() = %q", got)
	}
	if _, _, err := repo.CommitAuthor(""); err == nil {
		t.Fatal("CommitAuthor accepted an empty revision")
	}
	if _, _, err := repo.CommitAuthor("--upload-pack=touch"); err == nil {
		t.Fatal("CommitAuthor accepted an option-like revision")
	}
	if _, _, err := repo.CommitAuthor(strings.Repeat("a", 40)); err == nil {
		t.Fatal("CommitAuthor accepted a missing commit")
	}
}

type branchContainmentCase struct {
	name         string
	prepare      func(*testing.T, *Repo, string) string
	wantBranches []string
	wantExists   bool
	wantErr      bool
}

func TestBranchContainment(t *testing.T) {
	t.Parallel()
	tests := []branchContainmentCase{
		{
			name:         "main branch contains commit",
			wantBranches: []string{"refs/heads/main"},
			wantExists:   true,
		},
		{
			name: "local feature branch contains commit",
			prepare: func(t *testing.T, _ *Repo, root string) string {
				return commitOnFeatureBranch(t, root)
			},
			wantBranches: []string{"refs/heads/feature"},
			wantExists:   true,
		},
		{
			name: "deleted branch leaves uncontained object",
			prepare: func(t *testing.T, _ *Repo, root string) string {
				commit := commitOnFeatureBranch(t, root)
				runGit(t, root, "branch", "-q", "-D", "feature")
				return commit
			},
			wantExists: true,
		},
		{
			name: "remote-tracking branch contains commit",
			prepare: func(t *testing.T, _ *Repo, root string) string {
				commit := commitOnFeatureBranch(t, root)
				runGit(t, root, "branch", "-q", "-D", "feature")
				runGit(t, root, "update-ref", "refs/remotes/origin/kept", commit)
				return commit
			},
			wantBranches: []string{"refs/remotes/origin/kept"},
			wantExists:   true,
		},
		{
			name: "broken branch reference fails closed",
			prepare: func(t *testing.T, repo *Repo, root string) string {
				commit := commitOnFeatureBranch(t, root)
				runGit(t, root, "branch", "-q", "-D", "feature")
				broken := filepath.Join(repo.GitDir, "refs", "heads", "broken")
				if err := os.WriteFile(broken, []byte("broken\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				return commit
			},
			wantExists: true,
			wantErr:    true,
		},
		{
			name: "missing object is uncontained",
			prepare: func(_ *testing.T, _ *Repo, _ string) string {
				return "0000000000000000000000000000000000000001"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runBranchContainmentCase(t, test)
		})
	}
}

func runBranchContainmentCase(t *testing.T, test branchContainmentCase) {
	t.Helper()
	root := initRepository(t)
	writeFile(t, root, "one.txt", "one\n")
	commit := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if test.prepare != nil {
		commit = test.prepare(t, repo, root)
	}
	branches, exists, err := repo.BranchesContaining(commit)
	if test.wantErr {
		if err == nil {
			t.Fatalf("BranchesContaining() = %v, %v, nil, want error", branches, exists)
		}
		if ok, containsErr := repo.AnyBranchContains(commit); containsErr == nil || ok {
			t.Fatalf("AnyBranchContains() = %v, %v, want false and error", ok, containsErr)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if exists != test.wantExists || !reflect.DeepEqual(branches, test.wantBranches) {
		t.Fatalf("BranchesContaining() = %v, %v, want %v, %v",
			branches, exists, test.wantBranches, test.wantExists)
	}
	wantContains := len(test.wantBranches) > 0
	if contains, err := repo.AnyBranchContains(commit); err != nil || contains != wantContains {
		t.Fatalf("AnyBranchContains() = %v, %v, want %v", contains, err, wantContains)
	}
}

func TestBranchContainmentRejectsInvalidRevision(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"", "-"} {
		if _, err := repo.AnyBranchContains(revision); err == nil {
			t.Fatalf("AnyBranchContains(%q) returned no error", revision)
		}
		if _, _, err := repo.BranchesContaining(revision); err == nil {
			t.Fatalf("BranchesContaining(%q) returned no error", revision)
		}
	}
}

func TestValidateRefNameRejectsUnicodeWhitespace(t *testing.T) {
	t.Parallel()
	if err := validateRefName("refs/heads/bad\u00a0name"); err == nil {
		t.Fatal("validateRefName accepted Unicode whitespace")
	}
}

type branchScannerVerificationCase struct {
	name      string
	verifyErr error
}

func TestBranchScannerBoundsObjectVerification(t *testing.T) {
	t.Parallel()
	tests := []branchScannerVerificationCase{
		{name: "healthy object database"},
		{name: "verification failure stays fail closed", verifyErr: errors.New("object database unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runBranchScannerVerificationCase(t, test)
		})
	}
}

func runBranchScannerVerificationCase(t *testing.T, test branchScannerVerificationCase) {
	t.Helper()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	scanner := repo.newBranchScanner(func() error {
		verifyCalls++
		return test.verifyErr
	})
	for _, commit := range []string{
		"0000000000000000000000000000000000000001",
		"0000000000000000000000000000000000000002",
	} {
		assertMissingBranchContainment(t, scanner, commit, test.verifyErr)
	}
	if verifyCalls != 1 {
		t.Fatalf("object database verification calls = %d, want 1", verifyCalls)
	}
}

func assertMissingBranchContainment(
	t *testing.T,
	scanner *BranchScanner,
	commit string,
	verifyErr error,
) {
	t.Helper()
	branches, exists, err := scanner.BranchesContaining(commit)
	if verifyErr != nil {
		if !errors.Is(err, verifyErr) || exists || len(branches) != 0 {
			t.Fatalf("BranchesContaining(%s) = %v, %v, %v", commit, branches, exists, err)
		}
		return
	}
	if err != nil || exists || len(branches) != 0 {
		t.Fatalf("BranchesContaining(%s) = %v, %v, %v", commit, branches, exists, err)
	}
}

func commitOnFeatureBranch(t *testing.T, root string) string {
	t.Helper()
	runGit(t, root, "checkout", "-q", "-b", "feature")
	writeFile(t, root, "two.txt", "two\n")
	commit := commitAll(t, root, "second")
	runGit(t, root, "checkout", "-q", "main")
	return commit
}

func TestAnyBranchContainsReportsUnreadableObject(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unreadable objects need a non-root POSIX user")
	}
	for _, test := range []struct {
		name   string
		suffix string
	}{
		{name: "loose commit object"},
		{name: "packed object", suffix: ".pack"},
		{name: "packed index", suffix: ".idx"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := initRepository(t)
			writeFile(t, root, "one.txt", "one\n")
			first := commitAll(t, root, "first")
			repo, err := Discover(root)
			if err != nil {
				t.Fatal(err)
			}

			var object string
			if test.suffix == "" {
				object = filepath.Join(root, ".git", "objects", first[:2], first[2:])
				if _, err := os.Stat(object); err != nil {
					t.Skipf("loose object unavailable: %v", err)
				}
			} else {
				runGit(t, root, "repack", "-ad")
				objects, err := filepath.Glob(filepath.Join(root, ".git", "objects", "pack", "pack-*"+test.suffix))
				if err != nil {
					t.Fatal(err)
				}
				if len(objects) != 1 {
					t.Fatalf("generated %s files = %v, want one", test.suffix, objects)
				}
				object = objects[0]
			}

			info, err := os.Stat(object)
			if err != nil {
				t.Fatal(err)
			}
			mode := info.Mode().Perm()
			if err := os.Chmod(object, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(object, mode); err != nil {
					t.Errorf("restore permissions for %s: %v", object, err)
				}
			})
			if ok, err := repo.AnyBranchContains(first); err == nil || ok {
				t.Fatalf("AnyBranchContains(%s) = %v, %v, want false and an error", test.name, ok, err)
			}
		})
	}
}

func initRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init", "-b", "main")
	runGit(t, root, "config", "user.name", "Test User")
	runGit(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func commitAll(t *testing.T, root, message string) string {
	t.Helper()
	runGit(t, root, "add", "-A")
	runGit(t, root, "commit", "-m", message)
	return strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func TestCwdRelative(t *testing.T) {
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "f.txt", "root\n")
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		chdir string
		path  string
		want  string
		ok    bool
	}{
		{name: "cwd at root yields no candidate", chdir: root, path: "f.txt", ok: false},
		{name: "subdirectory rewrites path", chdir: filepath.Join(root, "sub"), path: "f.txt", want: "sub/f.txt", ok: true},
		{name: "parent reference resolves inside the worktree", chdir: filepath.Join(root, "sub"), path: "../f.txt", want: "f.txt", ok: true},
		{name: "absolute path yields no candidate", chdir: filepath.Join(root, "sub"), path: root, ok: false},
		{name: "cwd outside the worktree yields no candidate", chdir: t.TempDir(), path: "f.txt", ok: false},
		{name: "escape above the worktree yields no candidate", chdir: filepath.Join(root, "sub"), path: "../../escape.txt", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(tc.chdir)
			got, ok, err := repo.CwdRelative(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHeadReflogAction(t *testing.T) {
	// withSide returns a repository on main one commit behind branch side.
	withSide := func(t *testing.T) string {
		root := initRepository(t)
		writeFile(t, root, "f.txt", "one\n")
		runGit(t, root, "add", "f.txt")
		runGit(t, root, "commit", "-m", "one")
		runGit(t, root, "checkout", "-q", "-b", "side")
		writeFile(t, root, "side.txt", "side\n")
		runGit(t, root, "add", "side.txt")
		runGit(t, root, "commit", "-m", "side")
		runGit(t, root, "checkout", "-q", "main")
		return root
	}
	cases := []struct {
		name            string
		prepare         func(t *testing.T) string
		wantAction      string
		wantFastForward bool
		wantErr         bool
	}{
		{
			name:       "unborn repository errors",
			prepare:    func(t *testing.T) string { return initRepository(t) },
			wantErr:    true,
			wantAction: "",
		},
		{
			name: "commit records a commit action",
			prepare: func(t *testing.T) string {
				root := initRepository(t)
				writeFile(t, root, "f.txt", "one\n")
				runGit(t, root, "add", "f.txt")
				runGit(t, root, "commit", "-m", "one")
				return root
			},
			wantAction: "commit",
		},
		{
			name: "bare update-ref records an empty action",
			prepare: func(t *testing.T) string {
				root := initRepository(t)
				writeFile(t, root, "f.txt", "one\n")
				runGit(t, root, "add", "f.txt")
				runGit(t, root, "commit", "-m", "one")
				writeFile(t, root, "f.txt", "one\ntwo\n")
				runGit(t, root, "commit", "-am", "two")
				two := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD"))
				one := strings.TrimSpace(runGit(t, root, "rev-parse", "HEAD~1"))
				runGit(t, root, "reset", "--hard", one)
				runGit(t, root, "update-ref", "refs/heads/main", two, one)
				return root
			},
			wantAction: "",
		},
		{
			name: "merge fast-forward",
			prepare: func(t *testing.T) string {
				root := withSide(t)
				runGit(t, root, "merge", "-q", "--ff-only", "side")
				return root
			},
			wantAction:      "merge side",
			wantFastForward: true,
		},
		{
			// The refspec colon must not hide the fast-forward suffix.
			name: "pull fast-forward with a colon in the refspec",
			prepare: func(t *testing.T) string {
				root := withSide(t)
				runGit(t, root, "pull", "-q", "--ff-only", ".", "side:refs/remotes/local/side")
				return root
			},
			wantAction:      "pull",
			wantFastForward: true,
		},
		{
			name: "merge fast-forward ignoring a message",
			prepare: func(t *testing.T) string {
				root := withSide(t)
				runGit(t, root, "merge", "-q", "--ff", "-m", "ignored", "side")
				return root
			},
			wantAction:      "merge side",
			wantFastForward: true,
		},
		{
			name: "merge commit is not a fast-forward",
			prepare: func(t *testing.T) string {
				root := withSide(t)
				runGit(t, root, "merge", "-q", "--no-ff", "-m", "merge side", "side")
				return root
			},
			wantAction: "merge side",
		},
		{
			name: "commit subject ending like a fast-forward",
			prepare: func(t *testing.T) string {
				root := withSide(t)
				writeFile(t, root, "f.txt", "one\ntwo\n")
				runGit(t, root, "commit", "-am", "docs: Fast-forward")
				return root
			},
			wantAction: "commit",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := tc.prepare(t)
			repo, err := Discover(root)
			if err != nil {
				t.Fatal(err)
			}
			assertHeadReflogAction(t, repo, tc.wantAction, tc.wantFastForward, tc.wantErr)
		})
	}
}

func assertHeadReflogAction(t *testing.T, repo *Repo, wantAction string, wantFastForward, wantErr bool) {
	t.Helper()
	action, fastForward, err := repo.HeadReflogAction()
	if wantErr {
		if err == nil {
			t.Fatalf("HeadReflogAction() = %q, want an error", action)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if fastForward != wantFastForward {
		t.Fatalf("HeadReflogAction() fast-forward = %v, want %v (action %q)", fastForward, wantFastForward, action)
	}
	if wantAction == "" {
		if action != "" {
			t.Fatalf("HeadReflogAction() = %q, want empty", action)
		}
		return
	}
	if !strings.HasPrefix(action, wantAction) {
		t.Fatalf("HeadReflogAction() = %q, want prefix %q", action, wantAction)
	}
}
