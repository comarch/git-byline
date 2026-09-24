package gitcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testNotesRef  = "refs/notes/byline"
	testOtherSide = "refs/notes/other-side"
)

// notesMergeRepo returns a repository with three commits and one shared
// note on base in both the attribution ref and testOtherSide.
func notesMergeRepo(t *testing.T) (root string, repo *Repo, commits []string) {
	t.Helper()
	root = initRepository(t)
	for _, name := range []string{"base", "head", "other"} {
		writeFile(t, root, "file.txt", name+"\n")
		commits = append(commits, commitAll(t, root, name))
	}
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "shared", commits[0])
	runGit(t, root, "update-ref", testOtherSide, testNotesRef)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, commits
}

func refOID(t *testing.T, root, ref string) string {
	t.Helper()
	return strings.TrimSpace(runGit(t, root, "rev-parse", ref))
}

// startConflictingNotesMerge leaves an unfinished notes merge behind, like
// a person who is still resolving conflicts by hand.
func startConflictingNotesMerge(t *testing.T, root, commit string) {
	t.Helper()
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-f", "-m", "local", commit)
	runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-f", "-m", "remote", commit)
	command := exec.Command("git", "notes", "--ref="+testNotesRef, "merge", testOtherSide)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := command.CombinedOutput(); err == nil {
		t.Fatalf("conflicting notes merge succeeded: %s", out)
	}
}

func TestIsAncestor(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	first := refOID(t, root, testNotesRef)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "head", commits[1])
	second := refOID(t, root, testNotesRef)
	if ok, err := repo.IsAncestor(first, second); err != nil || !ok {
		t.Fatalf("IsAncestor(first, second) = %t, %v", ok, err)
	}
	if ok, err := repo.IsAncestor(second, first); err != nil || ok {
		t.Fatalf("IsAncestor(second, first) = %t, %v", ok, err)
	}
	if _, err := repo.IsAncestor("HEAD", second); err == nil {
		t.Fatal("IsAncestor accepted a symbolic revision")
	}
	if _, err := coverageOutputRepo(t, "", "fatal: not a commit", 128).IsAncestor(coverageOID, coverageOID2); err == nil {
		t.Fatal("IsAncestor accepted a Git failure")
	}
}

func TestUpdateAndDeleteRef(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	first := refOID(t, root, testNotesRef)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "head", commits[1])
	second := refOID(t, root, testNotesRef)
	if err := repo.UpdateRef(RemoteNotesRef, first, "", "test create"); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateRef(RemoteNotesRef, second, "", "test create again"); err == nil {
		t.Fatal("UpdateRef recreated an existing ref")
	}
	if err := repo.UpdateRef(RemoteNotesRef, second, second, "test stale"); err == nil {
		t.Fatal("UpdateRef moved a ref from a stale old value")
	}
	if err := repo.UpdateRef(RemoteNotesRef, second, first, "test move"); err != nil {
		t.Fatal(err)
	}
	if got := refOID(t, root, RemoteNotesRef); got != second {
		t.Fatalf("%s = %s, want %s", RemoteNotesRef, got, second)
	}
	if err := repo.DeleteRef(RemoteNotesRef, first); err == nil {
		t.Fatal("DeleteRef removed a ref from a stale old value")
	}
	if err := repo.DeleteRef(RemoteNotesRef, second); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.RefValue(RemoteNotesRef); err != nil || found {
		t.Fatalf("deleted ref = %t, %v", found, err)
	}
	for name, err := range map[string]error{
		"update bad ref":       repo.UpdateRef("bad ref", first, "", "x"),
		"update bad new value": repo.UpdateRef(RemoteNotesRef, "HEAD", "", "x"),
		"update bad old value": repo.UpdateRef(RemoteNotesRef, first, "HEAD", "x"),
		"delete bad ref":       repo.DeleteRef("bad ref", first),
		"delete bad old value": repo.DeleteRef(RemoteNotesRef, ""),
	} {
		if err == nil {
			t.Errorf("%s: accepted invalid input", name)
		}
	}
}

func TestRemoteNotesRefIsPerWorktree(t *testing.T) {
	root, repo, _ := notesMergeRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, root, "worktree", "add", "-q", "-b", "linked", linked)
	linkedRepo, err := Discover(linked)
	if err != nil {
		t.Fatal(err)
	}
	if err := linkedRepo.UpdateRef(RemoteNotesRef, refOID(t, root, testNotesRef), "", "test"); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repo.RefValue(RemoteNotesRef); err != nil || found {
		t.Fatalf("main worktree sees linked %s: %t, %v", RemoteNotesRef, found, err)
	}
}

func TestNotesMergeInProgress(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
		t.Fatalf("clean NotesMergeInProgress() = %t, %v", inProgress, err)
	}
	startConflictingNotesMerge(t, root, commits[1])
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || !inProgress {
		t.Fatalf("conflicted NotesMergeInProgress() = %t, %v", inProgress, err)
	}
	runGit(t, root, "notes", "--ref="+testNotesRef, "merge", "--abort")
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
		t.Fatalf("aborted NotesMergeInProgress() = %t, %v", inProgress, err)
	}
	worktree := filepath.Join(repo.GitDir, "NOTES_MERGE_WORKTREE")
	writeFile(t, worktree, "resolved", "hand edit\n")
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || !inProgress {
		t.Fatalf("resolved files NotesMergeInProgress() = %t, %v", inProgress, err)
	}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo.GitDir, "NOTES_MERGE_WORKTREE", "not a directory\n")
	if _, err := repo.NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted an unreadable merge worktree")
	}
}

func TestNotesMergeInProgressFailures(t *testing.T) {
	if _, err := coverageOutputRepo(t, "", "fatal: broken", 2).NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted a ref read failure")
	}
	missingRefBrokenPath := coverageRepo(t, `if [ "$2" = "--verify" ]; then
  exit 128
fi
exit 2
`)
	if _, err := missingRefBrokenPath.NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted a Git path failure")
	}
}

func TestMergeNotes(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		root, repo, commits := notesMergeRepo(t)
		runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "local", commits[1])
		runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-m", "remote", commits[2])
		local := refOID(t, root, testNotesRef)
		remote := refOID(t, root, testOtherSide)
		if err := repo.MergeNotes(testOtherSide); err != nil {
			t.Fatal(err)
		}
		parents := strings.Fields(runGit(t, root, "show", "-s", "--format=%P", testNotesRef))
		if len(parents) != 2 || parents[0] != local || parents[1] != remote {
			t.Fatalf("merge parents = %v, want %s %s", parents, local, remote)
		}
		for commit, want := range map[string]string{commits[0]: "shared", commits[1]: "local", commits[2]: "remote"} {
			if got := strings.TrimSpace(runGit(t, root, "notes", "--ref="+testNotesRef, "show", commit)); got != want {
				t.Fatalf("note for %s = %q, want %q", commit, got, want)
			}
		}
	})
	t.Run("configured strategy does not pick a side", func(t *testing.T) {
		root, repo, commits := notesMergeRepo(t)
		runGit(t, root, "config", "notes.mergeStrategy", "theirs")
		runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "local", commits[1])
		runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-m", "remote", commits[1])
		local := refOID(t, root, testNotesRef)
		if err := repo.MergeNotes(testOtherSide); !errors.Is(err, ErrNotesConflict) {
			t.Fatalf("MergeNotes() = %v, want %v", err, ErrNotesConflict)
		}
		if got := refOID(t, root, testNotesRef); got != local {
			t.Fatalf("conflict moved local notes to %s, want %s", got, local)
		}
		if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
			t.Fatalf("conflict left merge state: %t, %v", inProgress, err)
		}
	})
	t.Run("invalid remote", func(t *testing.T) {
		_, repo, _ := notesMergeRepo(t)
		for _, remote := range []string{"", "--abort", "refs/notes/a b"} {
			if err := repo.MergeNotes(remote); err == nil {
				t.Errorf("MergeNotes(%q) accepted an invalid revision", remote)
			}
		}
	})
}

func TestMergeNotesFailures(t *testing.T) {
	failed := coverageOutputRepo(t, "", "fatal: broken notes", 128)
	if err := failed.MergeNotes(coverageOID); err == nil || errors.Is(err, ErrNotesConflict) {
		t.Fatalf("MergeNotes(Git failure) = %v", err)
	}
	abortFails := coverageRepo(t, `if [ "$4" = "--abort" ]; then
  exit 2
fi
exit 1
`)
	err := abortFails.MergeNotes(coverageOID)
	if err == nil || errors.Is(err, ErrNotesConflict) || !strings.Contains(err.Error(), "abort") {
		t.Fatalf("MergeNotes(abort failure) = %v", err)
	}
	noIdentity := coverageOutputRepo(t, "", "Author identity unknown", 128)
	if err := noIdentity.MergeNotes(coverageOID); err == nil || !strings.Contains(err.Error(), "user.email") {
		t.Fatalf("MergeNotes(missing identity) = %v", err)
	}
}
