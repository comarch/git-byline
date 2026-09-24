package provenance

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const otherSideNotesRef = "refs/notes/other-side"

// remoteNotesRepo returns a repository with three commits and one shared
// note on base. otherSideNotesRef plays the remote notes history until a
// test copies it into gitcmd.RemoteNotesRef, like the pre-push fetch does.
func remoteNotesRepo(t *testing.T) (string, *gitcmd.Repo, []string) {
	t.Helper()
	root := testRepo(t)
	var commits []string
	for _, name := range []string{"base", "head", "other"} {
		write(t, root, "file.txt", name+"\n")
		commits = append(commits, commit(t, root, name))
	}
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "shared", commits[0])
	git(t, root, "update-ref", otherSideNotesRef, attributionNotesRef)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, commits
}

func fetchOtherSide(t *testing.T, root string) string {
	t.Helper()
	git(t, root, "update-ref", gitcmd.RemoteNotesRef, otherSideNotesRef)
	return notesOID(t, root, otherSideNotesRef)
}

func notesOID(t *testing.T, root, ref string) string {
	t.Helper()
	return strings.TrimSpace(git(t, root, "rev-parse", ref))
}

func noteText(t *testing.T, root, commit string) string {
	t.Helper()
	return strings.TrimSpace(git(t, root, "notes", "--ref="+attributionNotesRef, "show", commit))
}

func assertFetchedRefRemoved(t *testing.T, repo *gitcmd.Repo) {
	t.Helper()
	if _, found, err := repo.RefValue(gitcmd.RemoteNotesRef); err != nil || found {
		t.Fatalf("%s after merge = %t, %v", gitcmd.RemoteNotesRef, found, err)
	}
}

func TestMergeRemoteNotesRelations(t *testing.T) {
	t.Run("nothing fetched", func(t *testing.T) {
		root, repo, _ := remoteNotesRepo(t)
		local := notesOID(t, root, attributionNotesRef)
		result, err := MergeRemoteNotes(repo)
		if err != nil || result.FastForwarded || result.Merged || len(result.Warnings) != 0 {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if got := notesOID(t, root, attributionNotesRef); got != local {
			t.Fatalf("local notes moved to %s", got)
		}
	})
	t.Run("up to date", func(t *testing.T) {
		root, repo, _ := remoteNotesRepo(t)
		fetchOtherSide(t, root)
		result, err := MergeRemoteNotes(repo)
		if err != nil || result.FastForwarded || result.Merged {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		assertFetchedRefRemoved(t, repo)
	})
	t.Run("local ahead", func(t *testing.T) {
		root, repo, commits := remoteNotesRepo(t)
		fetchOtherSide(t, root)
		git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
		local := notesOID(t, root, attributionNotesRef)
		result, err := MergeRemoteNotes(repo)
		if err != nil || result.FastForwarded || result.Merged {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if got := notesOID(t, root, attributionNotesRef); got != local {
			t.Fatalf("local notes moved to %s, want %s", got, local)
		}
		assertFetchedRefRemoved(t, repo)
	})
	t.Run("behind", func(t *testing.T) {
		root, repo, commits := remoteNotesRepo(t)
		git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
		remote := fetchOtherSide(t, root)
		result, err := MergeRemoteNotes(repo)
		if err != nil || !result.FastForwarded || result.Merged {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if got := notesOID(t, root, attributionNotesRef); got != remote {
			t.Fatalf("local notes = %s, want fast-forward to %s", got, remote)
		}
		assertFetchedRefRemoved(t, repo)
	})
	t.Run("no local notes yet", func(t *testing.T) {
		root, repo, _ := remoteNotesRepo(t)
		remote := fetchOtherSide(t, root)
		git(t, root, "update-ref", "-d", attributionNotesRef)
		result, err := MergeRemoteNotes(repo)
		if err != nil || !result.FastForwarded {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if got := notesOID(t, root, attributionNotesRef); got != remote {
			t.Fatalf("local notes = %s, want %s", got, remote)
		}
	})
	t.Run("diverged on different commits", func(t *testing.T) {
		root, repo, commits := remoteNotesRepo(t)
		git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
		git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[2])
		fetchOtherSide(t, root)
		result, err := MergeRemoteNotes(repo)
		if err != nil || result.FastForwarded || !result.Merged {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if noteText(t, root, commits[1]) != "local" || noteText(t, root, commits[2]) != "remote" {
			t.Fatal("merged notes lost one side")
		}
		assertFetchedRefRemoved(t, repo)
	})
	t.Run("remote changed a shared note", func(t *testing.T) {
		root, repo, commits := remoteNotesRepo(t)
		git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
		git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-f", "-m", "remote update", commits[0])
		fetchOtherSide(t, root)
		result, err := MergeRemoteNotes(repo)
		if err != nil || !result.Merged {
			t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
		}
		if noteText(t, root, commits[0]) != "remote update" || noteText(t, root, commits[1]) != "local" {
			t.Fatal("merge did not take the only changed side of each note")
		}
	})
	t.Run("conflict on the same commit", func(t *testing.T) {
		root, repo, commits := remoteNotesRepo(t)
		git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
		git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
		fetchOtherSide(t, root)
		local := notesOID(t, root, attributionNotesRef)
		result, err := MergeRemoteNotes(repo)
		if !errors.Is(err, gitcmd.ErrNotesConflict) || result.Merged || result.FastForwarded {
			t.Fatalf("MergeRemoteNotes() = %+v, %v, want a conflict", result, err)
		}
		if got := notesOID(t, root, attributionNotesRef); got != local || noteText(t, root, commits[1]) != "local" {
			t.Fatalf("conflict changed local notes to %s", got)
		}
		if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
			t.Fatalf("conflict left merge state: %t, %v", inProgress, err)
		}
		assertFetchedRefRemoved(t, repo)
	})
}

func TestMergeRemoteNotesKeepsManualMerge(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
	command := exec.Command("git", "notes", "--ref="+attributionNotesRef, "merge", otherSideNotesRef)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("manual notes merge did not conflict: %s", out)
	}
	resolved := filepath.Join(repo.GitDir, "NOTES_MERGE_WORKTREE", commits[1])
	if err := os.WriteFile(resolved, []byte("hand resolved\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote other", commits[2])
	fetchOtherSide(t, root)
	local := notesOID(t, root, attributionNotesRef)
	if _, err := MergeRemoteNotes(repo); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("MergeRemoteNotes() = %v, want an in-progress error", err)
	}
	if got := notesOID(t, root, attributionNotesRef); got != local {
		t.Fatalf("local notes moved to %s during a manual merge", got)
	}
	if data, err := os.ReadFile(resolved); err != nil || string(data) != "hand resolved\n" {
		t.Fatalf("hand resolution = %q, %v", data, err)
	}
}

func TestMergeRemoteNotesLockFailure(t *testing.T) {
	root, repo, _ := remoteNotesRepo(t)
	fetchOtherSide(t, root)
	if err := os.MkdirAll(filepath.Join(repo.CommonDir, "byline", "notes.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := MergeRemoteNotes(repo); err == nil {
		t.Fatal("MergeRemoteNotes ran without the notes lock")
	}
}

// fakeNotesGit puts a Git wrapper first on PATH that fails every call whose
// arguments match pattern and runs the real Git for everything else.
func fakeNotesGit(t *testing.T, root, pattern string) *gitcmd.Repo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  $FAKE_NOTES_GIT_FAIL)
    printf '%s\n' 'forced failure' >&2
    exit 2
    ;;
esac
exec "$FAKE_NOTES_GIT_REAL" "$@"
`
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_NOTES_GIT_REAL", realGit)
	t.Setenv("FAKE_NOTES_GIT_FAIL", pattern)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestMergeRemoteNotesFailures(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		setup   func(t *testing.T, root string, commits []string)
		warning bool
	}{
		{name: "fetched ref read", pattern: "rev-parse --verify " + gitcmd.RemoteNotesRef},
		{name: "local ref read", pattern: "rev-parse --verify " + attributionNotesRef},
		{name: "remote ancestry", pattern: "merge-base --is-ancestor *", setup: divergeNotes},
		{name: "local ancestry", pattern: "merge-base --is-ancestor LOCAL *", setup: divergeNotes},
		{name: "merge state", pattern: "rev-parse --verify NOTES_MERGE_PARTIAL", setup: divergeNotes},
		{name: "fast-forward", pattern: "update-ref -m *", setup: remoteAhead},
		{name: "merge", pattern: "notes --ref=refs/notes/byline merge *", setup: divergeNotes},
		{name: "fetched ref cleanup", pattern: "update-ref -d *", setup: remoteAhead, warning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _, commits := remoteNotesRepo(t)
			if test.setup != nil {
				test.setup(t, root, commits)
			}
			fetchOtherSide(t, root)
			pattern := strings.ReplaceAll(test.pattern, "LOCAL", notesOID(t, root, attributionNotesRef))
			repo := fakeNotesGit(t, root, pattern)
			result, err := MergeRemoteNotes(repo)
			if test.warning {
				if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], gitcmd.RemoteNotesRef) {
					t.Fatalf("MergeRemoteNotes() = %+v, %v, want a cleanup warning", result, err)
				}
				return
			}
			if err == nil || errors.Is(err, gitcmd.ErrNotesConflict) {
				t.Fatalf("MergeRemoteNotes() = %+v, %v, want a failure", result, err)
			}
		})
	}
}

func divergeNotes(t *testing.T, root string, commits []string) {
	t.Helper()
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[2])
}

func remoteAhead(t *testing.T, root string, commits []string) {
	t.Helper()
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
}
