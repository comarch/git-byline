package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

func TestDashboardRangeRejectsInvalidUTF8Revision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := t.TempDir()
	configureFakeCommandGit(t, root, fakeDashboardRangeGitScript, map[string]string{
		"FAKE_DASHBOARD_RANGE_ROOT": root,
	})

	invalidRevision := string([]byte{0xff})
	code, _, stderr, err := appRun(root, zeroTime(), nil,
		"dashboard", "--range", invalidRevision)
	if code != ExitFailure || err == nil {
		t.Fatalf("dashboard invalid revision = %d, %v", code, err)
	}
	if !strings.Contains(stderr, "not valid UTF-8") {
		t.Fatalf("dashboard stderr = %q, want UTF-8 validation error", stderr)
	}
}

func TestDashboardRejectsStatusHeadMismatch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	root := appRepo(t)
	appWrite(t, root, "file.txt", "one\n")
	appCommit(t, root, "first")
	first := appHead(t, root)
	appWrite(t, root, "file.txt", "one\ntwo\n")
	appCommit(t, root, "second")
	second := appHead(t, root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(second, "file.txt")
	if err != nil || !exists {
		t.Fatalf("BlobID = %q, %t, %v", blob, exists, err)
	}
	writeVerifyNote(t, repo, second, model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"file.txt": {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1,
					End:   2,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			},
		},
	})

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "git")
	if err := os.WriteFile(wrapper, []byte(fakeDashboardMismatchGitScript), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_DASHBOARD_REAL_GIT", realGit)
	t.Setenv("FAKE_DASHBOARD_STATUS_HEAD", first)
	t.Setenv("FAKE_DASHBOARD_REPORT_HEAD", second)
	t.Setenv("FAKE_DASHBOARD_HEAD_COUNT", filepath.Join(t.TempDir(), "head-count"))
	t.Setenv("PATH", filepath.Dir(wrapper)+string(os.PathListSeparator)+os.Getenv("PATH"))

	code, _, stderr, err := appRun(root, zeroTime(), nil, "dashboard")
	if code != ExitFailure || err == nil {
		t.Fatalf("dashboard mismatched status = %d, %v", code, err)
	}
	if !strings.Contains(stderr, "status HEAD") {
		t.Fatalf("dashboard stderr = %q, want status mismatch", stderr)
	}
}

const fakeDashboardRangeGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_DASHBOARD_RANGE_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_DASHBOARD_RANGE_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_DASHBOARD_RANGE_ROOT" ;;
  *"rev-list"*) exit 0 ;;
  *"notes --ref=refs/notes/byline list"*) exit 0 ;;
  *) exit 1 ;;
esac
`

const fakeDashboardMismatchGitScript = `#!/bin/sh
if [ "$1" = "rev-parse" ] && [ "$2" = "--verify" ] && [ "$3" = "HEAD" ]; then
  count=0
  if [ -f "$FAKE_DASHBOARD_HEAD_COUNT" ]; then count=$(cat "$FAKE_DASHBOARD_HEAD_COUNT"); fi
  count=$((count + 1))
  printf '%s\n' "$count" >"$FAKE_DASHBOARD_HEAD_COUNT"
  if [ "$count" -eq 1 ]; then
    printf '%s\n' "$FAKE_DASHBOARD_STATUS_HEAD"
  else
    printf '%s\n' "$FAKE_DASHBOARD_REPORT_HEAD"
  fi
  exit 0
fi
exec "$FAKE_DASHBOARD_REAL_GIT" "$@"
`
