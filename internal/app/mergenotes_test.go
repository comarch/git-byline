package app

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
)

const sideNotesRef = "refs/notes/side"

// mergeNotesRepo returns a repository with three commits, one shared note
// on the first, and sideNotesRef as a copy of the attribution notes. Tests
// diverge the two refs and then copy sideNotesRef into
// gitcmd.RemoteNotesRef, like the pre-push fetch does.
func mergeNotesRepo(t *testing.T) (string, []string) {
	t.Helper()
	root := appRepo(t)
	var commits []string
	for _, name := range []string{"first", "second", "third"} {
		appWrite(t, root, "file.txt", name+"\n")
		appCommit(t, root, name)
		commits = append(commits, appHead(t, root))
	}
	appGit(t, root, "notes", "--ref=refs/notes/byline", "add", "-m", "shared", commits[0])
	appGit(t, root, "update-ref", sideNotesRef, "refs/notes/byline")
	return root, commits
}

func addNote(t *testing.T, root, ref, commit, text string) {
	t.Helper()
	appGit(t, root, "notes", "--ref="+ref, "add", "-m", text, commit)
}

func fetchSideNotes(t *testing.T, root string) {
	t.Helper()
	appGit(t, root, "update-ref", gitcmd.RemoteNotesRef, sideNotesRef)
}

func TestMergeNotesCommandUsage(t *testing.T) {
	root, _ := mergeNotesRepo(t)
	for _, args := range [][]string{
		{"merge-notes", "origin"},
		{"merge-notes", "--bad"},
	} {
		code, _, stderr, err := appRun(root, time.Time{}, nil, args...)
		if code != ExitUsage || err == nil || stderr == "" {
			t.Fatalf("Run(%v) = %d, %q, %v", args, code, stderr, err)
		}
	}
	code, stdout, _, err := appRun(root, time.Time{}, nil, "merge-notes", "-h")
	if code != ExitSuccess || err != nil || !strings.Contains(stdout, gitcmd.RemoteNotesRef) {
		t.Fatalf("merge-notes -h = %d, %q, %v", code, stdout, err)
	}
	code, _, stderr, err := appRun(t.TempDir(), time.Time{}, nil, "merge-notes")
	if code != ExitFailure || err == nil || !strings.Contains(stderr, "git-byline merge-notes:") {
		t.Fatalf("merge-notes outside a repository = %d, %q, %v", code, stderr, err)
	}
}

func TestMergeNotesCommandOutcomes(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, root string, commits []string)
		stdout string
	}{
		{name: "nothing fetched"},
		{
			name: "behind",
			setup: func(t *testing.T, root string, commits []string) {
				addNote(t, root, sideNotesRef, commits[1], "remote")
				fetchSideNotes(t, root)
			},
			stdout: "git-byline: updated attribution notes from the remote\n",
		},
		{
			name: "diverged",
			setup: func(t *testing.T, root string, commits []string) {
				addNote(t, root, "refs/notes/byline", commits[1], "local")
				addNote(t, root, sideNotesRef, commits[2], "remote")
				fetchSideNotes(t, root)
			},
			stdout: "git-byline: merged remote attribution notes\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, commits := mergeNotesRepo(t)
			if test.setup != nil {
				test.setup(t, root, commits)
			}
			code, stdout, stderr, err := appRun(root, time.Time{}, nil, "merge-notes", "--remote", "origin")
			if code != ExitSuccess || err != nil || stdout != test.stdout || stderr != "" {
				t.Fatalf("merge-notes = %d, %q, %q, %v", code, stdout, stderr, err)
			}
		})
	}
}

func TestMergeNotesCommandStopsDuringManualMerge(t *testing.T) {
	root, commits := mergeNotesRepo(t)
	addNote(t, root, "refs/notes/byline", commits[1], "local")
	addNote(t, root, sideNotesRef, commits[1], "remote")
	manual := exec.Command("git", "notes", "--ref=refs/notes/byline", "merge", sideNotesRef)
	manual.Dir = root
	manual.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := manual.CombinedOutput(); err == nil {
		t.Fatalf("manual notes merge did not conflict: %s", out)
	}
	fetchSideNotes(t, root)
	code, _, stderr, err := appRun(root, time.Time{}, nil, "merge-notes", "--remote", "origin")
	if code != ExitFailure || err == nil || !strings.Contains(stderr, "git notes merge --abort") {
		t.Fatalf("merge-notes = %d, %q, %v", code, stderr, err)
	}
}

func TestMergeNotesCommandConflictHelp(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		fetch  string
	}{
		{name: "remote name", remote: "origin", fetch: "origin"},
		{name: "url with credentials", remote: "https://user:secret-token@example.invalid/repo.git", fetch: "<remote>"},
		{name: "no remote", fetch: "<remote>"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, commits := mergeNotesRepo(t)
			addNote(t, root, "refs/notes/byline", commits[1], "local")
			addNote(t, root, sideNotesRef, commits[1], "remote")
			fetchSideNotes(t, root)
			local := strings.TrimSpace(appGit(t, root, "rev-parse", "refs/notes/byline"))
			code, stdout, stderr, err := appRun(root, time.Time{}, nil, "merge-notes", "--remote", test.remote)
			if code != ExitFailure || !errors.Is(err, gitcmd.ErrNotesConflict) || stdout != "" {
				t.Fatalf("merge-notes = %d, %q, %v", code, stdout, err)
			}
			want := "git-byline merge-notes: local and remote attribution notes differ for the same commit; nothing was changed\n" +
				"Merge the notes by hand, review the conflicts, then push again:\n" +
				"  git fetch " + test.fetch + " refs/notes/byline:refs/notes/byline-remote\n" +
				"  git notes --ref=refs/notes/byline merge refs/notes/byline-remote\n" +
				"  git update-ref -d refs/notes/byline-remote\n"
			if stderr != want {
				t.Fatalf("conflict help = %q, want %q", stderr, want)
			}
			if got := strings.TrimSpace(appGit(t, root, "rev-parse", "refs/notes/byline")); got != local {
				t.Fatalf("conflict moved local notes to %s, want %s", got, local)
			}
		})
	}
}

func TestPrintableRemote(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"origin":                       "origin",
		"my-fork":                      "my-fork",
		"team/upstream.v2_x":           "team/upstream.v2_x",
		"":                             "<remote>",
		"-origin":                      "<remote>",
		"../remote.git":                "<remote>",
		"/srv/git/repo.git":            "<remote>",
		"git@example.invalid:org/repo": "<remote>",
		"https://example.invalid/repo": "<remote>",
		"origin; touch pwned":          "<remote>",
		"origin\nfake line":            "<remote>",
		"\u00f3rigin":                  "<remote>",
	}
	for remote, want := range tests {
		if got := printableRemote(remote); got != want {
			t.Errorf("printableRemote(%q) = %q, want %q", remote, got, want)
		}
	}
}
