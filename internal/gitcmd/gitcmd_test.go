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
	t.Parallel()
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
	for _, path := range []string{"", "../outside", ".git/config", ".GIT/config", "dir/.git/config", "/outside", "bad\npath", "bad\x1bpath"} {
		if _, err := repo.NormalizeWorktreePath(path); err == nil {
			t.Fatalf("NormalizeWorktreePath accepted %q", path)
		}
	}
	if runtime.GOOS != "windows" {
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
	cases := []struct {
		name       string
		prepare    func(t *testing.T) string
		wantAction string
		wantErr    bool
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
			action, err := repo.HeadReflogAction()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("HeadReflogAction() = %q, want an error", action)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantAction == "" {
				if action != "" {
					t.Fatalf("HeadReflogAction() = %q, want empty", action)
				}
				return
			}
			if !strings.HasPrefix(action, tc.wantAction) {
				t.Fatalf("HeadReflogAction() = %q, want prefix %q", action, tc.wantAction)
			}
		})
	}
}
