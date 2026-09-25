package gitcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

const (
	coverageOID   = "1111111111111111111111111111111111111111"
	coverageOID2  = "2222222222222222222222222222222222222222"
	coverageShell = "#!/bin/sh\n"
	coverageBad   = "bad"
)

func coverageRepo(t *testing.T, body string) *Repo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = resolved
	gitBin := filepath.Join(root, "fake-git")
	// A parallel test that forks while the script is open for writing hands
	// the write descriptor to its child, and Linux then refuses to execute
	// the script with ETXTBSY. Every fork holds ForkLock for writing.
	syscall.ForkLock.RLock()
	err = os.WriteFile(gitBin, []byte(coverageShell+body), 0o700)
	syscall.ForkLock.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	return &Repo{Root: root, GitDir: root, gitBin: gitBin}
}

func coverageOutputRepo(t *testing.T, stdout, stderr string, exitCode int) *Repo {
	t.Helper()
	var body strings.Builder
	if stdout != "" {
		fmt.Fprintf(&body, "printf '%%b' %s\n", coverageShellQuote(stdout))
	}
	if stderr != "" {
		fmt.Fprintf(&body, "printf '%%b' %s >&2\n", coverageShellQuote(stderr))
	}
	fmt.Fprintf(&body, "exit %d\n", exitCode)
	return coverageRepo(t, body.String())
}

func coverageShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

type coverageCommandRun struct {
	name    string
	output  string
	wantErr bool
	run     func(*Repo) error
}

// Shared command probes reused by the error-path and malformed-output
// tables: the same Git calls checked against different fake behaviors.
func coverageCommandRuns() []coverageCommandRun {
	return []coverageCommandRun{
		{
			name:    "head",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.Head()
				return err
			},
		},
		{
			name:    "object format",
			output:  "sha512",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.ObjectIDLength()
				return err
			},
		},
		{
			name:    "previous head",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.PreviousHead()
				return err
			},
		},
		{
			name:    "current branch",
			output:  "refs/tags/v1",
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.CurrentBranchRef()
				return err
			},
		},
		{
			name:    "parents",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.Parents(coverageOID)
				return err
			},
		},
		{
			name:    "changes",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.Changes(coverageOID, "")
				return err
			},
		},
		{
			name:    "rev-list",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.RevList("", "", 0)
				return err
			},
		},
		{
			name:    "patch",
			output:  "diff text",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.PatchID(coverageOID)
				return err
			},
		},
	}
}

func TestGitcmdEarlyErrorPaths(t *testing.T) {
	tests := make([]coverageCommandRun, 0, 60)
	for _, entry := range coverageCommandRuns() {
		tests = append(tests, coverageCommandRun{name: entry.name + " command", run: entry.run})
	}
	tests = append(tests,
		coverageCommandRun{
			name: "worktree revision validation",
			run: func(repo *Repo) error {
				_, err := repo.WorktreeMatchesRevision("", coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name: "worktree revision validation",
			run: func(repo *Repo) error {
				_, err := repo.WorktreeMatchesRevision("", coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name: "worktree path validation",
			run: func(repo *Repo) error {
				_, err := repo.WorktreeMatchesRevision(coverageOID, "../file")
				return err
			},
		},
		coverageCommandRun{
			name: "worktree blob command",
			run: func(repo *Repo) error {
				_, err := repo.WorktreeMatchesRevision(coverageOID, coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name: "stash parent command",
			run: func(repo *Repo) error {
				_, err := repo.StashPaths(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "stash applied command",
			run: func(repo *Repo) error {
				_, err := repo.StashApplied(coverageOID, []string{coverageFileName})
				return err
			},
		},
		coverageCommandRun{
			name: "dirty paths command",
			run: func(repo *Repo) error {
				_, err := repo.DirtyPaths()
				return err
			},
		},
		coverageCommandRun{
			name: "commit time command",
			run: func(repo *Repo) error {
				_, err := repo.CommitTime(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "commit message command",
			run: func(repo *Repo) error {
				_, err := repo.CommitMessage(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "merge-base command",
			run: func(repo *Repo) error {
				_, err := repo.MergeBase(coverageOID, coverageOID2)
				return err
			},
		},
		coverageCommandRun{
			name: "note list command",
			run: func(repo *Repo) error {
				_, err := repo.NoteCommits(coverageNotesName)
				return err
			},
		},
		coverageCommandRun{
			name: "blob command",
			run: func(repo *Repo) error {
				_, _, err := repo.BlobID(coverageOID, coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name: "read blob command",
			run: func(repo *Repo) error {
				_, err := repo.ReadBlob(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "blob size command",
			run: func(repo *Repo) error {
				_, err := repo.BlobSize(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "hash command",
			run: func(repo *Repo) error {
				_, err := repo.HashBytes([]byte("content"))
				return err
			},
		},
		coverageCommandRun{
			name: "ignored command",
			run: func(repo *Repo) error {
				_, err := repo.Ignored(coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name: "read note ref validation",
			run: func(repo *Repo) error {
				_, _, err := repo.ReadNoteRef(coverageBranch, coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "read note commit validation",
			run: func(repo *Repo) error {
				_, _, err := repo.ReadNoteRef(coverageNotesName, "")
				return err
			},
		},
		coverageCommandRun{
			name: "write note ref validation",
			run: func(repo *Repo) error {
				return repo.WriteNoteRef(coverageBranch, coverageOID, nil)
			},
		},
		coverageCommandRun{
			name: "write note commit validation",
			run: func(repo *Repo) error {
				return repo.WriteNoteRef(coverageNotesName, "", nil)
			},
		},
		coverageCommandRun{
			name: "delete note ref validation",
			run: func(repo *Repo) error {
				return repo.DeleteNoteRef(coverageBranch, coverageOID)
			},
		},
		coverageCommandRun{
			name: "delete note commit validation",
			run: func(repo *Repo) error {
				return repo.DeleteNoteRef(coverageNotesName, "")
			},
		},
		coverageCommandRun{
			name: "delete note command",
			run: func(repo *Repo) error {
				return repo.DeleteNoteRef(coverageNotesName, coverageOID)
			},
		},
		coverageCommandRun{
			name: "delete equal read command",
			run: func(repo *Repo) error {
				_, err := repo.DeleteNoteRefIfEqual(coverageNotesName, coverageOID, nil)
				return err
			},
		},
		coverageCommandRun{
			name: "ref validation",
			run: func(repo *Repo) error {
				_, _, err := repo.RefValue("refs/heads/../bad")
				return err
			},
		},
		coverageCommandRun{
			name: "ref command",
			run: func(repo *Repo) error {
				_, _, err := repo.RefValue(coverageBranch)
				return err
			},
		},
		coverageCommandRun{
			name: "retention tree command",
			run: func(repo *Repo) error {
				return repo.ProtectBlobs([]string{coverageOID})
			},
		},
		coverageCommandRun{
			name: "retained count command",
			run: func(repo *Repo) error {
				_, err := repo.ProtectedBlobCount()
				return err
			},
		},
		coverageCommandRun{
			name: "history command",
			run: func(repo *Repo) error {
				_, err := repo.FirstParentHistory(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name: "config command",
			run: func(repo *Repo) error {
				_, _, err := repo.ConfigPath("core.hooksPath")
				return err
			},
		},
		coverageCommandRun{
			name: "Git path command",
			run: func(repo *Repo) error {
				_, err := repo.GitPath("hooks")
				return err
			},
		},
		coverageCommandRun{
			name: "version command",
			run: func(repo *Repo) error {
				_, _, err := repo.Version()
				return err
			},
		},
	)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			repo := coverageOutputRepo(t, "", "failure", 2)
			if err := test.run(repo); err == nil {
				t.Fatal("operation accepted fake Git failure")
			}
		})
	}
}

func TestGitcmdMalformedOutputs(t *testing.T) {
	tests := append(coverageCommandRuns(),
		coverageCommandRun{
			name:    "patch ID",
			output:  "",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.PatchID(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name:    "commit time",
			output:  "author only",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.CommitTime(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name:    "commit author",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.CommitAuthor(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name:    "merge base",
			output:  "one\n",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.MergeBase(coverageOID, coverageOID2)
				return err
			},
		},
		coverageCommandRun{
			name:    "note list length",
			output:  coverageOID + " " + coverageOID + " " + coverageOID,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.NoteCommits(coverageNotesName)
				return err
			},
		},
		coverageCommandRun{
			name:    "note list object",
			output:  "100644 blob " + coverageOID + "\tother",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.NoteCommits(coverageNotesName)
				return err
			},
		},
		coverageCommandRun{
			name:    "tree entry",
			output:  "100644 tree " + coverageOID + "\tfile",
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.BlobID(coverageOID, coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name:    "tree metadata",
			output:  "-1",
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.BlobID(coverageOID, coverageFileName)
				return err
			},
		},
		coverageCommandRun{
			name:    "blob size",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.BlobSize(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name:    "hash object",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.HashBytes([]byte("content"))
				return err
			},
		},
		coverageCommandRun{
			name:    "retention tree",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				return repo.ProtectBlobs([]string{coverageOID})
			},
		},
		coverageCommandRun{
			name:    "ref value",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, _, err := repo.RefValue(coverageBranch)
				return err
			},
		},
		coverageCommandRun{
			name:    "history object",
			output:  coverageBad,
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.FirstParentHistory(coverageOID)
				return err
			},
		},
		coverageCommandRun{
			name:    "Git path",
			output:  "relative/path",
			wantErr: true,
			run: func(repo *Repo) error {
				_, err := repo.GitPath("hooks")
				return err
			},
		},
	)
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			var body string
			switch test.name {
			case "patch ID":
				body = "if [ \"$1\" = 'diff-tree' ]; then printf 'diff text'; else printf 'bad'; fi\n"
			case "note list object":
				body = "printf '" + coverageOID + " " + coverageOID + " bad " + coverageOID + "'\n"
			default:
				body = "printf '%b' " + coverageShellQuote(test.output) + "\n"
			}
			repo := coverageRepo(t, body)
			if err := test.run(repo); (err != nil) != test.wantErr {
				t.Fatalf("operation error = %v, want error: %t", err, test.wantErr)
			}
		})
	}
}

func TestGitcmdValidationBranches(t *testing.T) {
	testGitcmdCommitValidation(t)
	testGitcmdRevisionValidation(t)
	testGitcmdMessageValidation(t)
	testGitcmdMergeValidation(t)
	testGitcmdObjectValidation(t)
	testGitcmdReferenceValidation(t)
}

func testGitcmdCommitValidation(t *testing.T) {
	t.Helper()
	if _, err := repoWithOutput(t, "").Parent(""); err == nil {
		t.Fatal("Parent accepted an invalid commit")
	}
	if _, err := repoWithOutput(t, "").Changes("", ""); err == nil {
		t.Fatal("Changes accepted an invalid commit")
	}
	if _, err := repoWithOutput(t, "").Changes(coverageOID, coverageBad); err == nil {
		t.Fatal("Changes accepted an invalid parent")
	}
}

func testGitcmdMessageValidation(t *testing.T) {
	t.Helper()
	if _, err := repoWithOutput(t, "").CommitMessage(""); err == nil {
		t.Fatal("CommitMessage accepted an empty revision")
	}
}

func testGitcmdRevisionValidation(t *testing.T) {
	t.Helper()
	if _, err := repoWithOutput(t, "").RevList("", "", -1); err == nil {
		t.Fatal("RevList accepted a negative limit")
	}
	if _, err := repoWithOutput(t, "").RevList("-bad", "", 0); err == nil {
		t.Fatal("RevList accepted an invalid from revision")
	}
	if _, err := repoWithOutput(t, "").RevList("", "has space", 0); err == nil {
		t.Fatal("RevList accepted an invalid to revision")
	}
	if _, err := repoWithOutput(t, "").PatchID(""); err == nil {
		t.Fatal("PatchID accepted an empty revision")
	}
	if _, err := repoWithOutput(t, "").CommitTime(""); err == nil {
		t.Fatal("CommitTime accepted an empty revision")
	}
	if _, _, err := repoWithOutput(t, "").CommitAuthor(""); err == nil {
		t.Fatal("CommitAuthor accepted an empty revision")
	}
}

func testGitcmdMergeValidation(t *testing.T) {
	t.Helper()
	if _, err := repoWithOutput(t, "").MergeBase("", coverageOID); err == nil {
		t.Fatal("MergeBase accepted an empty first revision")
	}
	if _, err := repoWithOutput(t, "").MergeBase(coverageOID, ""); err == nil {
		t.Fatal("MergeBase accepted an empty second revision")
	}
	if _, err := repoWithOutput(t, "").AnyBranchContains(""); err == nil {
		t.Fatal("AnyBranchContains accepted an empty revision")
	}
	if _, err := repoWithOutput(t, "").FirstParentHistory(""); err == nil {
		t.Fatal("FirstParentHistory accepted an empty revision")
	}
}

func testGitcmdObjectValidation(t *testing.T) {
	t.Helper()
	if _, _, err := repoWithOutput(t, "").BlobID("", coverageFileName); err == nil {
		t.Fatal("BlobID accepted an empty revision")
	}
	if _, _, err := repoWithOutput(t, "").BlobID(coverageOID, "../file"); err == nil {
		t.Fatal("BlobID accepted an escaping path")
	}
	if _, err := repoWithOutput(t, "").ReadBlob(""); err == nil {
		t.Fatal("ReadBlob accepted an empty object ID")
	}
	if _, err := repoWithOutput(t, "").BlobSize(""); err == nil {
		t.Fatal("BlobSize accepted an empty object ID")
	}
}

func testGitcmdReferenceValidation(t *testing.T) {
	t.Helper()
	if _, _, err := repoWithOutput(t, "").RefValue("refs/heads/../main"); err == nil {
		t.Fatal("RefValue accepted an invalid ref")
	}
	if err := repoWithOutput(t, "").ProtectBlobs([]string{coverageBad}); err == nil {
		t.Fatal("ProtectBlobs accepted an invalid object ID")
	}
}

func repoWithOutput(t *testing.T, output string) *Repo {
	t.Helper()
	return coverageOutputRepo(t, output, "", 0)
}

func TestGitcmdChangesAndRevListShapes(t *testing.T) {
	commit := coverageOID
	parent := coverageOID2
	changedOutput := "M\\000file\\000\\000R100\\000old.txt\\000new.txt\\000"
	repo := coverageOutputRepo(t, changedOutput, "", 0)
	changes, err := repo.Changes(commit, parent)
	if err != nil || len(changes) != 2 || changes[0].Path != coverageFileName ||
		changes[1].OldPath != "old.txt" || changes[1].Path != "new.txt" {
		t.Fatalf("Changes() = %+v, %v", changes, err)
	}

	for _, output := range []string{commit + "\n", commit + "\n" + parent + "\n"} {
		repo := coverageOutputRepo(t, output, "", 0)
		commits, err := repo.RevList(commit, "", 0)
		if err != nil || !reflect.DeepEqual(commits, strings.Fields(output)) {
			t.Fatalf("RevList(from) = %v, %v", commits, err)
		}
		repo = coverageOutputRepo(t, output, "", 0)
		commits, err = repo.RevList("", "", 1)
		if err != nil || !reflect.DeepEqual(commits, strings.Fields(output)) {
			t.Fatalf("RevList(HEAD) = %v, %v", commits, err)
		}
	}
}

func TestGitcmdChangesMalformedPaths(t *testing.T) {
	tests := []struct {
		name      string
		output    string
		wantError bool
	}{
		{name: "empty status", output: "\\000"},
		{name: "truncated rename", output: "R100\\000old\\000", wantError: true},
		{name: "bad old path", output: "R100\\000../old\\000new\\000", wantError: true},
		{name: "bad new path", output: "R100\\000old\\000../new\\000", wantError: true},
		{name: "truncated path", output: "M\\000", wantError: true},
		{name: "bad path", output: "M\\000../file\\000", wantError: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			repo := coverageOutputRepo(t, test.output, "", 0)
			_, err := repo.Changes(coverageOID, "")
			if (err != nil) != test.wantError {
				t.Fatalf("Changes() error = %v, want error: %t", err, test.wantError)
			}
		})
	}
}

func TestGitcmdPatchIDBranches(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "patch command failure",
			body: "printf 'diff text' >&2\nexit 2\n",
		},
		{
			name: "patch ID command failure",
			body: "if [ \"$1\" = 'diff-tree' ]; then printf 'diff text'; else exit 2; fi\n",
		},
		{
			name: "invalid patch ID",
			body: "if [ \"$1\" = 'diff-tree' ]; then printf 'diff text'; else printf 'bad'; fi\n",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := coverageRepo(t, test.body).PatchID(coverageOID); err == nil {
				t.Fatal("PatchID accepted fake failure")
			}
		})
	}
}

func TestGitcmdSpecialObjectBranches(t *testing.T) {
	if _, err := coverageOutputRepo(t, "sha256", "", 0).ObjectIDLength(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coverageOutputRepo(t, "", "", 128).PreviousHead(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coverageOutputRepo(t, "", "", 1).CurrentBranchRef(); err != nil {
		t.Fatal(err)
	}
	parents, err := coverageOutputRepo(t, coverageOID+" "+coverageOID2, "", 0).Parents(coverageOID)
	if err != nil || !reflect.DeepEqual(parents, []string{coverageOID2}) {
		t.Fatalf("Parents() = %v, %v", parents, err)
	}
	if _, err := coverageOutputRepo(t, coverageOID+" bad", "", 0).Parents(coverageOID); err == nil {
		t.Fatal("Parents accepted an invalid parent object ID")
	}
}
