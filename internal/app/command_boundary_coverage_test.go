package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckMissingNoteBoundaryFailures(t *testing.T) {
	for _, mode := range []string{"missing", "read-error", "duplicate"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			configureFakeCommandGit(t, root, fakeCheckGitScript, map[string]string{
				"FAKE_CHECK_ROOT":      root,
				"FAKE_CHECK_MODE":      mode,
				"FAKE_CHECK_COMMIT":    strings.Repeat("1", 40),
				"FAKE_CHECK_NOTE":      strings.Repeat("2", 40),
				"FAKE_CHECK_NOTE_DATA": `{"version":3,"files":{}}`,
				"FAKE_CHECK_COUNT":     filepath.Join(root, "show-count"),
			})
			code, _, _, err := appRun(root, zeroTime(), nil, "check", "--require-note")
			if code != ExitFailure || err == nil {
				t.Fatalf("check %s = %d, %v", mode, code, err)
			}
		})
	}
}

func TestExportHeadReadFailure(t *testing.T) {
	root := t.TempDir()
	configureFakeCommandGit(t, root, fakeExportGitScript, map[string]string{
		"FAKE_EXPORT_ROOT": root,
	})
	code, _, _, err := appRun(root, zeroTime(), nil, "export", "--format", coverageGitAI)
	if code != ExitFailure || err == nil {
		t.Fatalf("export HEAD failure = %d, %v", code, err)
	}
}

func TestCheckpointDiscoverNonRepositoryFailure(t *testing.T) {
	root := t.TempDir()
	configureFakeCommandGit(t, root, fakeCheckpointGitScript, map[string]string{
		"FAKE_CHECKPOINT_ROOT": root,
	})
	payload := `{"session_id":"s","tool_name":"Edit","tool_input":{"file_path":"file.txt"}}`
	code, _, _, err := appRun(root, zeroTime(), strings.NewReader(payload),
		"checkpoint", "droid", "--type", "human", "--hook-input", "stdin")
	if code != ExitFailure || err == nil {
		t.Fatalf("checkpoint discover failure = %d, %v", code, err)
	}
}

func configureFakeCommandGit(t *testing.T, root, script string, values map[string]string) {
	t.Helper()
	path := filepath.Join(root, "git")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, value := range values {
		t.Setenv(name, value)
	}
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
}

const fakeCheckGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_CHECK_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_CHECK_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_CHECK_ROOT" ;;
  *"rev-list"*)
    printf '%s\n' "$FAKE_CHECK_COMMIT"
    if [ "$FAKE_CHECK_MODE" = "duplicate" ]; then printf '%s\n' "$FAKE_CHECK_COMMIT"; fi
    ;;
  *"notes --ref=refs/notes/byline list"*)
    if [ "$FAKE_CHECK_MODE" = "duplicate" ]; then exit 0; fi
    printf '%s %s\n' "$FAKE_CHECK_NOTE" "$FAKE_CHECK_COMMIT"
    ;;
  *"notes --ref=refs/notes/byline show"*)
    count=0
    if [ -f "$FAKE_CHECK_COUNT" ]; then count=$(cat "$FAKE_CHECK_COUNT"); fi
    count=$((count + 1))
    printf '%s\n' "$count" >"$FAKE_CHECK_COUNT"
    if [ "$FAKE_CHECK_MODE" = "read-error" ] && [ "$count" -gt 1 ]; then exit 2; fi
    if [ "$FAKE_CHECK_MODE" = "missing" ]; then exit 1; fi
    printf '%s\n' "$FAKE_CHECK_NOTE_DATA"
    ;;
  *"show -s"*) printf '2026-01-02T03:04:05+00:00\n' ;;
  *) exit 1 ;;
esac
`

const fakeExportGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_EXPORT_ROOT" ;;
  *"--path-format=absolute --git-dir"*) printf '%s/.git\n' "$FAKE_EXPORT_ROOT" ;;
  *"--path-format=absolute --git-common-dir"*) printf '%s/.git\n' "$FAKE_EXPORT_ROOT" ;;
  *"rev-parse --verify HEAD"*) exit 2 ;;
  *) exit 1 ;;
esac
`

const fakeCheckpointGitScript = `#!/bin/sh
case "$*" in
  *"--show-toplevel"*) printf '%s\n' "$FAKE_CHECKPOINT_ROOT" ;;
  *) exit 2 ;;
esac
`
