package provenance

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
)

func TestCaptureEventRecordsEditsInOwningWorktree(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, ".gitignore", ".worktrees/\n")
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	mainRepo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(mainRepo); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, ".worktrees", "wt")
	git(t, root, "worktree", "add", "-q", "-b", "nested", nested)
	sibling := filepath.Join(resolvedDir(t), "sibling")
	git(t, root, "worktree", "add", "-q", "-b", "sibling", sibling)

	paths := []string{filepath.Join(nested, "file.txt"), filepath.Join(sibling, "file.txt")}
	human := preset.Event{Type: model.AuthorHuman, Paths: paths}
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: paths,
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if result, err := CaptureEvent(mainRepo, human, now); err != nil || result.Recorded != 2 || len(result.Warnings) != 0 {
		t.Fatalf("CaptureEvent(human) = %+v, %v", result, err)
	}
	write(t, nested, "file.txt", "base\nnested\n")
	write(t, sibling, "file.txt", "base\nsibling\n")
	if result, err := CaptureEvent(mainRepo, ai, now.Add(time.Second)); err != nil || result.Recorded != 2 || len(result.Warnings) != 0 {
		t.Fatalf("CaptureEvent(ai) = %+v, %v", result, err)
	}
	if retained, err := mainRepo.ProtectedBlobCount(); err != nil || retained != 0 {
		t.Fatalf("main retained snapshots = %d, %v", retained, err)
	}
	for _, dir := range []string{nested, sibling} {
		commit(t, dir, "agent line")
		repo, err := gitcmd.Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Annotate(repo); err != nil {
			t.Fatal(err)
		}
		blame, err := Blame(repo, "file.txt")
		if err != nil {
			t.Fatal(err)
		}
		if len(blame.Lines) != 2 ||
			blame.Lines[0].Attribution.Author != model.AuthorHuman ||
			blame.Lines[1].Attribution.Author != model.AuthorAI ||
			blame.Lines[1].Attribution.Agent != "droid" {
			t.Fatalf("Blame(%s) = %+v", dir, blame.Lines)
		}
	}
}

func TestCaptureEventWorktreeBoundaries(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, ".gitignore", ".worktrees/\nsecret.txt\n")
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	nested := filepath.Join(root, ".worktrees", "wt")
	git(t, root, "worktree", "add", "-q", "-b", "nested", nested)
	mainRepo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	nestedRepo, err := gitcmd.Discover(nested)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if result, err := CaptureEvent(mainRepo, preset.Event{Type: model.AuthorHuman}, now); err != nil ||
		result.Recorded != 0 || len(result.Warnings) != 0 {
		t.Fatalf("CaptureEvent(no paths) = %+v, %v", result, err)
	}

	write(t, nested, "secret.txt", "secret\n")
	ignored := preset.Event{Type: model.AuthorHuman, Paths: []string{filepath.Join(nested, "secret.txt")}}
	want := []string{fmt.Sprintf("worktree %q: skipped %q: path %q is ignored by Git", nestedRepo.Root, "secret.txt", "secret.txt")}
	if result, err := CaptureEvent(mainRepo, ignored, now); err != nil || result.Recorded != 0 ||
		!reflect.DeepEqual(result.Warnings, want) {
		t.Fatalf("CaptureEvent(ignored) = %+v, %v; want warnings %q", result, err, want)
	}

	// Shell paths come from the dirty state of the worktree that runs the hook.
	write(t, root, "file.txt", "base\nmain\n")
	write(t, nested, "file.txt", "base\nnested\n")
	shell := preset.Event{
		Type: model.AuthorHuman, Kind: model.CheckpointKindShellPre, EventID: "shell-1",
		Paths: []string{filepath.Join(nested, "file.txt")},
	}
	if result, err := CaptureEvent(mainRepo, shell, now); err != nil || result.Recorded != 1 {
		t.Fatalf("CaptureEvent(shell_pre) = %+v, %v", result, err)
	}
	if retained, err := nestedRepo.ProtectedBlobCount(); err != nil || retained != 0 {
		t.Fatalf("nested retained snapshots = %d, %v", retained, err)
	}

	nestedStore := filepath.Join(nestedRepo.GitDir, "byline")
	if err := os.RemoveAll(nestedStore); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nestedStore, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	edit := preset.Event{Type: model.AuthorHuman, Paths: []string{filepath.Join(nested, "file.txt")}}
	if _, err := CaptureEvent(mainRepo, edit, now); err == nil ||
		!strings.HasPrefix(err.Error(), fmt.Sprintf("worktree %q: ", nestedRepo.Root)) {
		t.Fatalf("CaptureEvent(nested storage failure) = %v, want the worktree named", err)
	}

	missing := &gitcmd.Repo{Root: filepath.Join(t.TempDir(), "missing")}
	if _, err := CaptureEvent(missing, edit, now); err == nil ||
		!strings.Contains(err.Error(), "route checkpoint paths") {
		t.Fatalf("CaptureEvent(missing root) = %v, want a routing error", err)
	}
}

func resolvedDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
