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
	for _, path := range []string{"", "../outside", ".git/config", ".GIT/config", "/outside", "bad\npath", "bad\x1bpath"} {
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
