package provenance

import (
	"encoding/hex"
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
	t.Run("nothing fetched", testMergeRemoteNotesNothingFetched)
	t.Run("up to date", testMergeRemoteNotesUpToDate)
	t.Run("local ahead", testMergeRemoteNotesLocalAhead)
	t.Run("behind", testMergeRemoteNotesBehind)
	t.Run("no local notes yet", testMergeRemoteNotesNoLocalNotes)
	t.Run("diverged on different commits", testMergeRemoteNotesDiverged)
	t.Run("same note added on both sides", testMergeRemoteNotesSameNoteOnBothSides)
	t.Run("unrelated histories", testMergeRemoteNotesUnrelatedHistories)
	t.Run("conflict on the same commit", testMergeRemoteNotesConflict)
}

func testMergeRemoteNotesNothingFetched(t *testing.T) {
	root, repo, _ := remoteNotesRepo(t)
	local := notesOID(t, root, attributionNotesRef)
	result, err := MergeRemoteNotes(repo)
	if err != nil || result.FastForwarded || result.Merged || len(result.Warnings) != 0 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if got := notesOID(t, root, attributionNotesRef); got != local {
		t.Fatalf("local notes moved to %s", got)
	}
}

func testMergeRemoteNotesUpToDate(t *testing.T) {
	root, repo, _ := remoteNotesRepo(t)
	fetchOtherSide(t, root)
	result, err := MergeRemoteNotes(repo)
	if err != nil || result.FastForwarded || result.Merged {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	assertFetchedRefRemoved(t, repo)
}

func testMergeRemoteNotesLocalAhead(t *testing.T) {
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
}

func testMergeRemoteNotesBehind(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
	remote := fetchOtherSide(t, root)
	result, err := MergeRemoteNotes(repo)
	if err != nil || !result.FastForwarded || result.Merged || result.Added != 1 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if got := notesOID(t, root, attributionNotesRef); got != remote {
		t.Fatalf("local notes = %s, want fast-forward to %s", got, remote)
	}
	assertFetchedRefRemoved(t, repo)
}

func testMergeRemoteNotesNoLocalNotes(t *testing.T) {
	root, repo, _ := remoteNotesRepo(t)
	remote := fetchOtherSide(t, root)
	git(t, root, "update-ref", "-d", attributionNotesRef)
	result, err := MergeRemoteNotes(repo)
	if err != nil || !result.FastForwarded || result.Added != 1 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if got := notesOID(t, root, attributionNotesRef); got != remote {
		t.Fatalf("local notes = %s, want %s", got, remote)
	}
}

func testMergeRemoteNotesDiverged(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[2])
	fetchOtherSide(t, root)
	result, err := MergeRemoteNotes(repo)
	if err != nil || result.FastForwarded || !result.Merged || result.Added != 1 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if noteText(t, root, commits[1]) != "local" || noteText(t, root, commits[2]) != "remote" {
		t.Fatal("merged notes lost one side")
	}
	assertFetchedRefRemoved(t, repo)
}

func testMergeRemoteNotesSameNoteOnBothSides(t *testing.T) {
	// Local writes its other note first. Otherwise both sides can
	// create the same notes commit within one second, and the remote
	// notes are then simply an ancestor of local notes.
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[2])
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "same", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "same", commits[1])
	fetchOtherSide(t, root)
	result, err := MergeRemoteNotes(repo)
	if err != nil || !result.Merged || result.Added != 0 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if noteText(t, root, commits[1]) != "same" || noteText(t, root, commits[2]) != "local" {
		t.Fatal("merge changed a note that both sides agree on")
	}
}

func testMergeRemoteNotesUnrelatedHistories(t *testing.T) {
	// The forge job can start remote notes before any clone pushed
	// its own, so the two histories share no commit.
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "update-ref", "-d", otherSideNotesRef)
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[2])
	fetchOtherSide(t, root)
	result, err := MergeRemoteNotes(repo)
	if err != nil || !result.Merged || result.Added != 1 {
		t.Fatalf("MergeRemoteNotes() = %+v, %v", result, err)
	}
	if noteText(t, root, commits[0]) != "shared" || noteText(t, root, commits[2]) != "remote" {
		t.Fatal("history-less merge lost one side")
	}
}

func testMergeRemoteNotesConflict(t *testing.T) {
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
}

// TestMergeRemoteNotesRefusesRemoteEdits covers remote changes that Git would
// apply without a conflict: the local side left the note alone, so a
// fast-forward or a merge takes the remote version.
func TestMergeRemoteNotesRefusesRemoteEdits(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string, commits []string)
	}{
		{name: "fast-forward changes a note", setup: func(t *testing.T, root string, commits []string) {
			git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-f", "-m", "rewritten", commits[0])
		}},
		{name: "fast-forward removes a note", setup: func(t *testing.T, root string, commits []string) {
			git(t, root, "notes", "--ref="+otherSideNotesRef, "remove", commits[0])
		}},
		{name: "merge changes a note", setup: func(t *testing.T, root string, commits []string) {
			git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
			git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-f", "-m", "rewritten", commits[0])
		}},
		{name: "merge removes a note", setup: func(t *testing.T, root string, commits []string) {
			git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
			git(t, root, "notes", "--ref="+otherSideNotesRef, "remove", commits[0])
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, repo, commits := remoteNotesRepo(t)
			test.setup(t, root, commits)
			fetchOtherSide(t, root)
			local := notesOID(t, root, attributionNotesRef)
			result, err := MergeRemoteNotes(repo)
			if !errors.Is(err, gitcmd.ErrNotesConflict) || result.Merged || result.FastForwarded {
				t.Fatalf("MergeRemoteNotes() = %+v, %v, want a conflict", result, err)
			}
			if !strings.Contains(err.Error(), commits[0]) {
				t.Fatalf("conflict %q does not name commit %s", err, commits[0])
			}
			if got := notesOID(t, root, attributionNotesRef); got != local || noteText(t, root, commits[0]) != "shared" {
				t.Fatalf("remote edit changed local notes to %s", got)
			}
			assertFetchedRefRemoved(t, repo)
		})
	}
}

func TestMergeRemoteNotesConflictNamesEveryCommit(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	git(t, root, "notes", "--ref="+attributionNotesRef, "add", "-m", "local", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-m", "remote", commits[1])
	git(t, root, "notes", "--ref="+otherSideNotesRef, "remove", commits[0])
	fetchOtherSide(t, root)
	first := min(commits[0], commits[1])
	_, err := MergeRemoteNotes(repo)
	if !errors.Is(err, gitcmd.ErrNotesConflict) || !strings.Contains(err.Error(), "commit "+first+" and 1 more") {
		t.Fatalf("MergeRemoteNotes() = %v, want both commits counted", err)
	}
}

func gitWithInput(t *testing.T, root, input string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Stdin = strings.NewReader(input)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// slashNameNotes writes a notes commit on top of parent whose tree holds a
// single entry named like the fanout path of commit, slash included, with
// blob as the note. mktree refuses such a name, so the tree is written raw,
// like a remote that skips fsck could serve it.
func slashNameNotes(t *testing.T, root, parent, commit, blob string) string {
	t.Helper()
	raw, err := hex.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	entry := "100644 " + commit[:2] + "/" + commit[2:] + "\x00" + string(raw)
	tree := gitWithInput(t, root, entry, "hash-object", "-t", "tree", "--literally", "-w", "--stdin")
	args := []string{"commit-tree", tree, "-m", "notes"}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return strings.TrimSpace(git(t, root, args...))
}

// slashRemote moves the shared note of the other side into a slash name.
// ls-tree shows the same notes, so only the check after the fast-forward
// sees that Git no longer finds the note.
func slashRemote(t *testing.T, root string, commits []string) {
	t.Helper()
	blob := notesOID(t, root, attributionNotesRef+":"+commits[0])
	remote := slashNameNotes(t, root, notesOID(t, root, otherSideNotesRef), commits[0], blob)
	git(t, root, "update-ref", otherSideNotesRef, remote)
}

// TestMergeRemoteNotesRefusesBrokenNotes covers notes refs that point to
// something other than a notes commit, and remote histories with other
// files, oversized notes, or trees that Git reads differently.
func TestMergeRemoteNotesRefusesBrokenNotes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root string) (fetched string)
	}{
		{name: "remote tag", setup: func(t *testing.T, root string) string {
			git(t, root, "tag", "-a", "-m", "tag", "notes-tag", otherSideNotesRef)
			return notesOID(t, root, "notes-tag")
		}},
		{name: "remote tree", setup: func(t *testing.T, root string) string {
			return notesOID(t, root, otherSideNotesRef+"^{tree}")
		}},
		{name: "remote blob", setup: func(t *testing.T, root string) string {
			return gitWithInput(t, root, "not notes\n", "hash-object", "-w", "--stdin")
		}},
		{name: "local tag", setup: func(t *testing.T, root string) string {
			git(t, root, "tag", "-a", "-m", "tag", "notes-tag", attributionNotesRef)
			git(t, root, "update-ref", attributionNotesRef, notesOID(t, root, "notes-tag"))
			return notesOID(t, root, otherSideNotesRef)
		}},
		{name: "remote file entry", setup: func(t *testing.T, root string) string {
			blob := gitWithInput(t, root, "payload\n", "hash-object", "-w", "--stdin")
			entries := git(t, root, "ls-tree", otherSideNotesRef) + "100644 blob " + blob + "\tpayload.sh\n"
			tree := gitWithInput(t, root, entries, "mktree")
			return strings.TrimSpace(git(t, root, "commit-tree", tree, "-p", otherSideNotesRef, "-m", "payload"))
		}},
		{name: "remote file in an older commit", setup: func(t *testing.T, root string) string {
			// The tip tree is clean, the file hides in a second parent.
			blob := gitWithInput(t, root, "payload\n", "hash-object", "-w", "--stdin")
			tree := gitWithInput(t, root, "100644 blob "+blob+"\tpayload.sh\n", "mktree")
			payload := strings.TrimSpace(git(t, root, "commit-tree", tree, "-m", "payload"))
			return strings.TrimSpace(git(t, root, "commit-tree", otherSideNotesRef+"^{tree}",
				"-p", otherSideNotesRef, "-p", payload, "-m", "notes"))
		}},
		{name: "remote note over the limit", setup: func(t *testing.T, root string) string {
			large := gitWithInput(t, root, strings.Repeat("x", 16<<20+1), "hash-object", "-w", "--stdin")
			git(t, root, "notes", "--ref="+otherSideNotesRef, "add", "-C", large, notesOID(t, root, "HEAD"))
			return notesOID(t, root, otherSideNotesRef)
		}},
		{name: "remote slash in an entry name", setup: func(t *testing.T, root string) string {
			slashRemote(t, root, []string{notesOID(t, root, "HEAD~2")})
			return notesOID(t, root, otherSideNotesRef)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, repo, _ := remoteNotesRepo(t)
			fetched := test.setup(t, root)
			git(t, root, "update-ref", gitcmd.RemoteNotesRef, fetched)
			local := notesOID(t, root, attributionNotesRef)
			result, err := MergeRemoteNotes(repo)
			if err == nil || errors.Is(err, gitcmd.ErrNotesConflict) || result.FastForwarded || result.Merged {
				t.Fatalf("MergeRemoteNotes() = %+v, %v, want a failure", result, err)
			}
			if got := notesOID(t, root, attributionNotesRef); got != local {
				t.Fatalf("broken notes moved local notes to %s, want %s", got, local)
			}
			assertFetchedRefRemoved(t, repo)
		})
	}
}

// TestMergeRemoteNotesRemovesUncheckedFirstNotes fast-forwards a clone
// without notes to a remote tree that Git reads differently. The check
// after the fast-forward must delete the ref again.
func TestMergeRemoteNotesRemovesUncheckedFirstNotes(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	blob := notesOID(t, root, attributionNotesRef+":"+commits[0])
	git(t, root, "update-ref", gitcmd.RemoteNotesRef, slashNameNotes(t, root, "", commits[1], blob))
	git(t, root, "update-ref", "-d", attributionNotesRef)
	result, err := MergeRemoteNotes(repo)
	if err == nil || errors.Is(err, gitcmd.ErrNotesConflict) || result.FastForwarded {
		t.Fatalf("MergeRemoteNotes() = %+v, %v, want a failure", result, err)
	}
	if _, found, err := repo.RefValue(attributionNotesRef); err != nil || found {
		t.Fatalf("local notes after the failed check = %t, %v, want none", found, err)
	}
	assertFetchedRefRemoved(t, repo)
}

func TestMergeCommitNeedsBothSides(t *testing.T) {
	root, repo, commits := remoteNotesRepo(t)
	divergeNotes(t, root, commits)
	local := notesOID(t, root, attributionNotesRef)
	remote := notesOID(t, root, otherSideNotesRef)
	if err := repo.MergeNotes(remote); err != nil {
		t.Fatal(err)
	}
	merged := notesOID(t, root, attributionNotesRef)
	if got, err := mergeCommit(repo, local, remote); err != nil || got != merged {
		t.Fatalf("mergeCommit() = %q, %v, want %q", got, err, merged)
	}
	if _, err := mergeCommit(repo, remote, local); err == nil || !strings.Contains(err.Error(), "during the merge") {
		t.Fatalf("mergeCommit(other parents) = %v, want an error", err)
	}
	// Each fake needs its own subtest: its PATH change must be gone before
	// the next fake looks up the real Git, or that fake would exec itself.
	for _, test := range []struct{ name, pattern string }{
		{name: "ref read", pattern: "rev-parse --verify " + attributionNotesRef},
		{name: "parents read", pattern: "rev-list --parents -n 1 " + merged},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := mergeCommit(fakeNotesGit(t, root, test.pattern), local, remote); err == nil {
				t.Error("mergeCommit accepted a Git failure")
			}
		})
	}
	git(t, root, "update-ref", "-d", attributionNotesRef)
	if _, err := mergeCommit(repo, local, remote); err == nil {
		t.Fatal("mergeCommit accepted a missing notes ref")
	}
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
		{name: "remote type", pattern: "cat-file -t REMOTE"},
		{name: "local type", pattern: "cat-file -t LOCAL"},
		{name: "remote ancestry", pattern: "merge-base --is-ancestor *", setup: divergeNotes},
		{name: "local ancestry", pattern: "merge-base --is-ancestor LOCAL *", setup: divergeNotes},
		{name: "merge state", pattern: "rev-parse --verify --quiet --symbolic-full-name NOTES_MERGE_PARTIAL", setup: divergeNotes},
		{name: "local notes tree", pattern: "ls-tree * LOCAL", setup: divergeNotes},
		{name: "merge base", pattern: "merge-base LOCAL REMOTE", setup: divergeNotes},
		{name: "merge base notes tree", pattern: "ls-tree * BASE", setup: divergeNotes},
		{name: "remote notes tree", pattern: "ls-tree * REMOTE", setup: remoteAhead},
		{name: "history objects", pattern: "rev-list --objects *", setup: remoteAhead},
		{name: "history object types", pattern: "cat-file --batch-check*", setup: remoteAhead},
		{name: "fast-forward", pattern: "update-ref -m *", setup: remoteAhead},
		{name: "merge", pattern: "notes --ref=refs/notes/byline merge *", setup: divergeNotes},
		{name: "merge parents", pattern: "rev-list --parents -n 1 *", setup: divergeNotes},
		{name: "merged notes list", pattern: "notes --ref=refs/notes/byline list", setup: remoteAhead},
		{name: "restore", pattern: "update-ref -m git-byline: restore*", setup: slashRemote},
		{name: "fetched ref cleanup", pattern: "update-ref -d *", setup: remoteAhead, warning: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, _, commits := remoteNotesRepo(t)
			if test.setup != nil {
				test.setup(t, root, commits)
			}
			remote := fetchOtherSide(t, root)
			local := notesOID(t, root, attributionNotesRef)
			base := strings.TrimSpace(git(t, root, "merge-base", local, remote))
			pattern := strings.NewReplacer("LOCAL", local, "REMOTE", remote, "BASE", base).Replace(test.pattern)
			repo := fakeNotesGit(t, root, pattern)
			result, err := MergeRemoteNotes(repo)
			if test.warning {
				assertCleanupWarning(t, result, err)
				return
			}
			if err == nil || errors.Is(err, gitcmd.ErrNotesConflict) {
				t.Fatalf("MergeRemoteNotes() = %+v, %v, want a failure", result, err)
			}
		})
	}
}

func assertCleanupWarning(t *testing.T, result NotesMergeResult, err error) {
	t.Helper()
	if err != nil || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], gitcmd.RemoteNotesRef) {
		t.Fatalf("MergeRemoteNotes() = %+v, %v, want a cleanup warning", result, err)
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
