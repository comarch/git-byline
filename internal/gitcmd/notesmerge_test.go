package gitcmd

import (
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

func runGitWithInput(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Stdin = strings.NewReader(input)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// treeCommit writes a commit whose tree is built from git mktree input.
func treeCommit(t *testing.T, root, entries string) string {
	t.Helper()
	tree := runGitWithInput(t, root, entries, "mktree")
	return strings.TrimSpace(runGit(t, root, "commit-tree", tree, "-m", "notes"))
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

func TestFindMergeBase(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	base := refOID(t, root, testNotesRef)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "local", commits[1])
	runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-m", "remote", commits[2])
	unrelated := treeCommit(t, root, "")
	tests := []struct {
		name          string
		first, second string
		want          string
		found         bool
	}{
		{name: "diverged", first: refOID(t, root, testNotesRef), second: refOID(t, root, testOtherSide), want: base, found: true},
		{name: "unrelated", first: refOID(t, root, testNotesRef), second: unrelated},
	}
	for _, test := range tests {
		got, found, err := repo.FindMergeBase(test.first, test.second)
		if err != nil || got != test.want || found != test.found {
			t.Errorf("%s: FindMergeBase() = %q, %t, %v, want %q, %t", test.name, got, found, err, test.want, test.found)
		}
	}
	if _, _, err := repo.FindMergeBase("HEAD", base); err == nil {
		t.Error("FindMergeBase accepted a symbolic revision")
	}
	if _, _, err := coverageOutputRepo(t, "", "fatal: bad object", 128).FindMergeBase(coverageOID, coverageOID2); err == nil {
		t.Error("FindMergeBase accepted a Git failure")
	}
}

func TestObjectType(t *testing.T) {
	root, repo, _ := notesMergeRepo(t)
	notes := refOID(t, root, testNotesRef)
	for oid, want := range map[string]string{
		notes:                            "commit",
		refOID(t, root, notes+"^{tree}"): "tree",
		runGitWithInput(t, root, "x\n", "hash-object", "-w", "--stdin"): "blob",
	} {
		if got, err := repo.ObjectType(oid); err != nil || got != want {
			t.Errorf("ObjectType(%s) = %q, %v, want %q", oid, got, err, want)
		}
	}
	if _, err := repo.ObjectType("HEAD"); err == nil {
		t.Error("ObjectType accepted a symbolic revision")
	}
	if _, err := coverageOutputRepo(t, "", "fatal: bad object", 128).ObjectType(coverageOID); err == nil {
		t.Error("ObjectType accepted a Git failure")
	}
}

func TestNoteBlobs(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	blob := runGitWithInput(t, root, "note\n", "hash-object", "-w", "--stdin")
	note := func(mode, kind, oid, path string) string {
		return mode + " " + kind + " " + oid + "\t" + path + "\n"
	}
	fanout := runGitWithInput(t, root, note("100644", "blob", blob, commits[1][2:]), "mktree")
	valid := treeCommit(t, root, note("100644", "blob", blob, commits[0])+
		note("040000", "tree", fanout, commits[1][:2]))
	got, err := repo.NoteBlobs(valid)
	if err != nil || len(got) != 2 || got[commits[0]] != blob || got[commits[1]] != blob {
		t.Fatalf("NoteBlobs(flat and fanout) = %v, %v", got, err)
	}
	if got, err := repo.NoteBlobs(treeCommit(t, root, "")); err != nil || len(got) != 0 {
		t.Fatalf("NoteBlobs(empty tree) = %v, %v", got, err)
	}

	wideFanout := runGitWithInput(t, root, note("100644", "blob", blob, commits[1][3:]), "mktree")
	invalid := map[string]string{
		"file name":      note("100644", "blob", blob, "README"),
		"executable":     note("100755", "blob", blob, commits[0]),
		"symlink":        note("120000", "blob", blob, commits[0]),
		"short name":     note("100644", "blob", blob, commits[0][:39]),
		"uppercase":      note("100644", "blob", blob, strings.ToUpper(commits[0])),
		"wide fanout":    note("040000", "tree", wideFanout, commits[1][:3]),
		"duplicate note": note("100644", "blob", blob, commits[1]) + note("040000", "tree", fanout, commits[1][:2]),
	}
	for name, entries := range invalid {
		if got, err := repo.NoteBlobs(treeCommit(t, root, entries)); err == nil {
			t.Errorf("%s: NoteBlobs() = %v, want an error", name, got)
		}
	}
	if _, err := repo.NoteBlobs("HEAD"); err == nil {
		t.Error("NoteBlobs accepted a symbolic revision")
	}
	if _, err := coverageOutputRepo(t, "garbage\\0", "", 0).NoteBlobs(coverageOID); err == nil {
		t.Error("NoteBlobs accepted a malformed ls-tree record")
	}
	if _, err := coverageOutputRepo(t, "", "fatal: bad tree", 128).NoteBlobs(coverageOID); err == nil {
		t.Error("NoteBlobs accepted a Git failure")
	}
}

// slashNameTree writes a tree with a single entry named like a fanout path
// to commit, slash included. mktree refuses such a name, so the tree is
// written raw, like a remote that skips fsck could serve it.
func slashNameTree(t *testing.T, root, commit, blob string) string {
	t.Helper()
	raw, err := hex.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	entry := "100644 " + commit[:2] + "/" + commit[2:] + "\x00" + string(raw)
	return runGitWithInput(t, root, entry, "hash-object", "-t", "tree", "--literally", "-w", "--stdin")
}

func TestFanoutDirectory(t *testing.T) {
	t.Parallel()
	deepest := strings.TrimSuffix(strings.Repeat("ab/", 19), "/")
	tests := map[string]bool{
		"ab":            true,
		"0f/a9":         true,
		deepest:         true,
		deepest + "/cd": false,
		"":              false,
		"a":             false,
		"abc":           false,
		"AB":            false,
		"zz":            false,
		"ab/c":          false,
		"ab//cd":        false,
	}
	for path, want := range tests {
		if got := fanoutDirectory(path, 40); got != want {
			t.Errorf("fanoutDirectory(%q) = %t, want %t", path, got, want)
		}
	}
}

func TestCheckNotesHistory(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	base := refOID(t, root, testNotesRef)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "head", commits[1])
	local := refOID(t, root, testNotesRef)
	localEntries := runGit(t, root, "ls-tree", local)
	entry := func(mode, kind, oid, path string) string {
		return mode + " " + kind + " " + oid + "\t" + path + "\n"
	}
	notesCommit := func(tree string, parents ...string) string {
		t.Helper()
		args := []string{"commit-tree", tree, "-m", "notes"}
		for _, parent := range parents {
			args = append(args, "-p", parent)
		}
		return strings.TrimSpace(runGit(t, root, args...))
	}
	onLocal := func(entries string) string {
		t.Helper()
		return notesCommit(runGitWithInput(t, root, localEntries+entries, "mktree", "--missing"), local)
	}
	blob := runGitWithInput(t, root, "note\n", "hash-object", "-w", "--stdin")
	fanout := runGitWithInput(t, root, entry("100644", "blob", blob, commits[2][2:]), "mktree")
	valid := []struct {
		name, tip, exclude string
	}{
		{name: "whole history", tip: local},
		{name: "new commits", tip: local, exclude: base},
		{name: "nothing new", tip: base, exclude: local},
		{name: "fanout tree", tip: onLocal(entry("040000", "tree", fanout, commits[2][:2])), exclude: local},
	}
	for _, test := range valid {
		if err := repo.CheckNotesHistory(test.tip, test.exclude); err != nil {
			t.Errorf("%s: CheckNotesHistory() = %v", test.name, err)
		}
	}

	payload := notesCommit(runGitWithInput(t, root, entry("100644", "blob", blob, "README"), "mktree"))
	large := runGitWithInput(t, root, strings.Repeat("x", maxNoteBytes+1), "hash-object", "-w", "--stdin")
	nested := runGitWithInput(t, root, entry("100644", "blob", blob, commits[2]), "mktree")
	injected := "100644 blob " + blob + "\tx\n" + blob + " " + commits[2] + "\x00"
	missing := strings.Repeat("0", 39) + "1"
	invalid := []struct {
		name, tip, want string
	}{
		{name: "file in an older commit", tip: notesCommit(local+"^{tree}", local, payload), want: `at "README"`},
		{name: "directory", tip: onLocal(entry("040000", "tree", nested, "notes")), want: `at "notes`},
		{name: "note over the limit", tip: onLocal(entry("100644", "blob", large, commits[2])), want: "byte note limit"},
		{name: "missing blob", tip: onLocal(entry("100644", "blob", missing, commits[2])), want: missing + " is missing"},
		{
			// rev-list prints the name only up to the newline.
			name: "name with a newline",
			tip:  notesCommit(runGitWithInput(t, root, injected, "mktree", "-z"), local),
			want: `at "x"`,
		},
	}
	for _, test := range invalid {
		err := repo.CheckNotesHistory(test.tip, local)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: CheckNotesHistory() = %v, want %q", test.name, err, test.want)
		}
	}
	for _, args := range [][2]string{{"HEAD", ""}, {local, "HEAD"}} {
		if err := repo.CheckNotesHistory(args[0], args[1]); err == nil {
			t.Errorf("CheckNotesHistory(%q, %q) accepted a symbolic revision", args[0], args[1])
		}
	}
}

func TestCheckNotesHistoryGitOutput(t *testing.T) {
	listed := `if [ "$1" = rev-list ]; then
  printf '%s\n%s x\n' ` + coverageOID + " " + coverageOID2 + `
  exit 0
fi
`
	tests := map[string]string{
		"rev-list failure":  "exit 128\n",
		"bad rev-list line": "printf 'bad\\n'\n",
		"cat-file failure":  listed + "exit 128\n",
		"short answer":      listed + "printf '%s\\n' '" + coverageOID + " commit 1'\n",
		"other object":      listed + "printf '%s\\n' '" + coverageOID2 + " commit 1' '" + coverageOID + " blob 1'\n",
		"bad size":          listed + "printf '%s\\n' '" + coverageOID + " commit x' '" + coverageOID2 + " blob 1'\n",
		"blob outside tree": listed + "printf '%s\\n' '" + coverageOID + " commit 1' '" + coverageOID2 + " blob 1'\n",
	}
	for name, script := range tests {
		if err := coverageRepo(t, script).CheckNotesHistory(coverageOID, ""); err == nil {
			t.Errorf("%s: CheckNotesHistory() accepted it", name)
		}
	}
}

func TestListNotes(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "head", commits[1])
	want := map[string]string{
		commits[0]: refOID(t, root, testNotesRef+":"+commits[0]),
		commits[1]: refOID(t, root, testNotesRef+":"+commits[1]),
	}
	if got, err := repo.ListNotes(); err != nil || !maps.Equal(got, want) {
		t.Fatalf("ListNotes() = %v, %v, want %v", got, err, want)
	}

	// ls-tree shows the slash name like a fanout path, but Git's notes code
	// does not read it as a note. That gap is why merge results are listed.
	tree := slashNameTree(t, root, commits[2], want[commits[0]])
	malformed := strings.TrimSpace(runGit(t, root, "commit-tree", tree, "-m", "notes"))
	if blobs, err := repo.NoteBlobs(malformed); err != nil || blobs[commits[2]] != want[commits[0]] {
		t.Fatalf("NoteBlobs(slash name) = %v, %v", blobs, err)
	}
	runGit(t, root, "update-ref", testNotesRef, malformed)
	if got, err := repo.ListNotes(); err != nil || len(got) != 0 {
		t.Fatalf("ListNotes(slash name) = %v, %v, want no notes", got, err)
	}

	for name, output := range map[string]string{
		"odd field count":  coverageOID,
		"invalid object":   coverageOID + " HEAD",
		"duplicate object": coverageOID + " " + coverageOID2 + "\n" + coverageOID + " " + coverageOID2,
	} {
		if got, err := coverageOutputRepo(t, output, "", 0).ListNotes(); err == nil {
			t.Errorf("%s: ListNotes() = %v, want an error", name, got)
		}
	}
	if _, err := coverageOutputRepo(t, "", "fatal: bad notes", 128).ListNotes(); err == nil {
		t.Error("ListNotes accepted a Git failure")
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
	runGit(t, root, "tag", "NOTES_MERGE_PARTIAL", commits[0])
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
		t.Fatalf("NotesMergeInProgress() with a NOTES_MERGE_PARTIAL tag = %t, %v", inProgress, err)
	}
	runGit(t, root, "tag", "-d", "NOTES_MERGE_PARTIAL")
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
		t.Fatal("NotesMergeInProgress accepted a merge worktree that is not a directory")
	}
}

func TestNotesMergeInProgressUnreadableWorktree(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("unreadable directories need a non-root POSIX user")
	}
	_, repo, _ := notesMergeRepo(t)
	worktree := filepath.Join(repo.GitDir, "NOTES_MERGE_WORKTREE")
	if err := os.Mkdir(worktree, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(worktree, 0o700); err != nil {
			t.Errorf("restore permissions for %s: %v", worktree, err)
		}
	})
	if _, err := repo.NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted an unreadable merge worktree")
	}
}

func TestNotesMergeInProgressFailures(t *testing.T) {
	if _, err := coverageOutputRepo(t, "", "fatal: broken", 2).NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted a ref read failure")
	}
	missingRefBrokenPath := coverageRepo(t, `if [ "$2" = "--verify" ]; then
  exit 1
fi
exit 2
`)
	if _, err := missingRefBrokenPath.NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted a Git path failure")
	}
	pathUnderFile := coverageRepo(t, `if [ "$2" = "--verify" ]; then
  exit 1
fi
printf '%s\n' "$(dirname "$0")/blocker/NOTES_MERGE_WORKTREE"
`)
	writeFile(t, pathUnderFile.Root, "blocker", "regular file\n")
	if _, err := pathUnderFile.NotesMergeInProgress(); err == nil {
		t.Fatal("NotesMergeInProgress accepted a merge worktree path it cannot stat")
	}
}

func TestMergeNotes(t *testing.T) {
	t.Run("clean", testMergeNotesClean)
	t.Run("configured strategy does not pick a side", testMergeNotesIgnoresConfiguredStrategy)
	t.Run("failure aborts only its own merge state", testMergeNotesFailureAbortsOwnState)
	t.Run("invalid remote", testMergeNotesInvalidRemote)
}

func testMergeNotesClean(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "local", commits[1])
	runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-m", "remote", commits[2])
	local := refOID(t, root, testNotesRef)
	remote := refOID(t, root, testOtherSide)
	if err := repo.MergeNotes(remote); err != nil {
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
}

func testMergeNotesIgnoresConfiguredStrategy(t *testing.T) {
	root, repo, commits := notesMergeRepo(t)
	runGit(t, root, "config", "notes.mergeStrategy", "theirs")
	runGit(t, root, "notes", "--ref="+testNotesRef, "add", "-m", "local", commits[1])
	runGit(t, root, "notes", "--ref="+testOtherSide, "add", "-m", "remote", commits[1])
	local := refOID(t, root, testNotesRef)
	if err := repo.MergeNotes(refOID(t, root, testOtherSide)); !errors.Is(err, ErrNotesConflict) {
		t.Fatalf("MergeNotes() = %v, want %v", err, ErrNotesConflict)
	}
	if got := refOID(t, root, testNotesRef); got != local {
		t.Fatalf("conflict moved local notes to %s, want %s", got, local)
	}
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
		t.Fatalf("conflict left merge state: %t, %v", inProgress, err)
	}
}

func testMergeNotesFailureAbortsOwnState(t *testing.T) {
	// Git writes the conflict state, then dies with 128 because the
	// linked worktree already merges into the same notes ref.
	root, repo, commits := notesMergeRepo(t)
	linked := filepath.Join(t.TempDir(), "linked")
	runGit(t, root, "worktree", "add", "-q", "-b", "linked", linked)
	startConflictingNotesMerge(t, linked, commits[1])
	local := refOID(t, root, testNotesRef)
	err := repo.MergeNotes(refOID(t, root, testOtherSide))
	if err == nil || errors.Is(err, ErrNotesConflict) {
		t.Fatalf("MergeNotes() = %v, want a failure", err)
	}
	if inProgress, err := repo.NotesMergeInProgress(); err != nil || inProgress {
		t.Fatalf("failed merge left merge state: %t, %v", inProgress, err)
	}
	linkedRepo, err := Discover(linked)
	if err != nil {
		t.Fatal(err)
	}
	if inProgress, err := linkedRepo.NotesMergeInProgress(); err != nil || !inProgress {
		t.Fatalf("linked worktree merge = %t, %v, want it kept", inProgress, err)
	}
	if got := refOID(t, root, testNotesRef); got != local {
		t.Fatalf("failed merge moved local notes to %s, want %s", got, local)
	}
}

func testMergeNotesInvalidRemote(t *testing.T) {
	_, repo, _ := notesMergeRepo(t)
	for _, remote := range []string{"", "--abort", "refs/notes/a b", "HEAD", testOtherSide} {
		if err := repo.MergeNotes(remote); err == nil {
			t.Errorf("MergeNotes(%q) accepted an invalid object ID", remote)
		}
	}
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
	for _, test := range []struct {
		name       string
		abortExit  int
		wantDetail string
	}{
		{name: "left state aborted", abortExit: 0, wantDetail: "broken merge"},
		{name: "abort fails", abortExit: 2, wantDetail: "abort failed notes merge"},
	} {
		leftState := coverageRepo(t, fmt.Sprintf(`case "$*" in
  *"merge --strategy=manual"*) printf 'broken merge\n' >&2; exit 128 ;;
  *NOTES_MERGE_PARTIAL*) printf 'NOTES_MERGE_PARTIAL\n' ;;
  *"merge --abort"*) exit %d ;;
esac
`, test.abortExit))
		err := leftState.MergeNotes(coverageOID)
		if err == nil || errors.Is(err, ErrNotesConflict) || !strings.Contains(err.Error(), test.wantDetail) {
			t.Errorf("%s: MergeNotes() = %v, want %q", test.name, err, test.wantDetail)
		}
	}
}
