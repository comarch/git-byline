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
			stdout: "git-byline: updated attribution notes from the remote, 1 added\n",
		},
		{
			name: "diverged",
			setup: func(t *testing.T, root string, commits []string) {
				addNote(t, root, "refs/notes/byline", commits[1], "local")
				addNote(t, root, sideNotesRef, commits[2], "remote")
				fetchSideNotes(t, root)
			},
			stdout: "git-byline: merged remote attribution notes, 1 added\n",
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
		{name: "remote name", remote: "origin", fetch: `"$(git remote get-url --push origin)"`},
		{name: "url with credentials", remote: "https://user:secret-token@example.invalid/repo.git", fetch: "<push URL>"},
		{name: "no remote", fetch: "<push URL>"},
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
			want := "git-byline merge-notes: local and remote attribution notes differ for commit " + commits[1] +
				"; nothing was changed\n" +
				"Fetch the remote notes and compare both versions of the note first:\n" +
				"  git -c fetch.fsckObjects=true fetch --no-tags --refmap= " + test.fetch +
				" +refs/notes/byline:refs/notes/byline-remote\n" +
				"  git notes --ref=refs/notes/byline show <commit>\n" +
				"  git notes --ref=refs/notes/byline-remote show <commit>\n" +
				"The merge below takes a remote change or removal without a conflict when\n" +
				"local notes left that note alone, and it fast-forwards when local notes are\n" +
				"behind. Run it only when the remote version is right, then push again:\n" +
				"  git notes --ref=refs/notes/byline merge --strategy=manual refs/notes/byline-remote\n" +
				"  (on a conflict, fix the files Git names, then run\n" +
				"   git notes --ref=refs/notes/byline merge --commit, or --abort to stop)\n" +
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

func TestPushURLSource(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"origin":                       `"$(git remote get-url --push origin)"`,
		"my-fork":                      `"$(git remote get-url --push my-fork)"`,
		"team/upstream.v2_x":           `"$(git remote get-url --push team/upstream.v2_x)"`,
		"":                             "<push URL>",
		"-origin":                      "<push URL>",
		"../remote.git":                "<push URL>",
		"/srv/git/repo.git":            "<push URL>",
		"git@example.invalid:org/repo": "<push URL>",
		"https://example.invalid/repo": "<push URL>",
		"origin; touch pwned":          "<push URL>",
		"origin)\"; touch pwned":       "<push URL>",
		"origin\nfake line":            "<push URL>",
		"\u00f3rigin":                  "<push URL>",
	}
	for remote, want := range tests {
		if got := pushURLSource(remote); got != want {
			t.Errorf("pushURLSource(%q) = %q, want %q", remote, got, want)
		}
	}
}
