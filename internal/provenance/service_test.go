package provenance

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrwogu/git-byline/internal/gitcmd"
	"github.com/mrwogu/git-byline/internal/model"
	"github.com/mrwogu/git-byline/internal/notes"
	"github.com/mrwogu/git-byline/internal/preset"
)

func TestEndToEndAttributionAndPartialCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := Annotate(repo); err != nil || result.Files != 1 {
		t.Fatalf("Annotate(base) = %+v, %v", result, err)
	}

	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if result, err := Capture(repo, human, now); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(human) = %+v, %v", result, err)
	}
	write(t, root, "file.txt", "base\nai-one\n")
	if result, err := Capture(repo, ai, now.Add(time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(ai one) = %+v, %v", result, err)
	}
	git(t, root, "add", "file.txt")
	write(t, root, "file.txt", "base\nai-one\nai-two\n")
	if result, err := Capture(repo, ai, now.Add(2*time.Second)); err != nil || result.Recorded != 1 {
		t.Fatalf("Capture(ai two) = %+v, %v", result, err)
	}
	git(t, root, "gc", "--prune=now")
	git(t, root, "commit", "-m", "partial")
	if result, err := Annotate(repo); err != nil || result.Files != 1 {
		t.Fatalf("Annotate(partial) = %+v, %v", result, err)
	}
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingFiles != 1 || status.PendingCheckpoints != 0 || status.RetainedSnapshots != 1 {
		t.Fatalf("Status(partial) = %+v", status)
	}
	blame, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 2 ||
		blame.Lines[0].Attribution.Author != model.AuthorHuman ||
		blame.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame(partial) = %+v", blame.Lines)
	}

	git(t, root, "add", "file.txt")
	git(t, root, "commit", "-m", "finish")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err = Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 3 {
		t.Fatalf("Blame(finish) lines = %d", len(blame.Lines))
	}
	for i := 1; i < 3; i++ {
		if blame.Lines[i].Attribution.Author != model.AuthorAI || blame.Lines[i].Attribution.Agent != "droid" {
			t.Fatalf("line %d attribution = %+v", i+1, blame.Lines[i].Attribution)
		}
	}
	status, err = Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingFiles != 0 || status.RetainedSnapshots != 0 || status.Head != status.LastAnnotatedCommit {
		t.Fatalf("Status(finish) = %+v", status)
	}
	if result, err := Annotate(repo); err != nil || !result.Noop {
		t.Fatalf("idempotent Annotate() = %+v, %v", result, err)
	}
}

func TestLegacyHistoryStartsUntracked(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "legacy.txt", "legacy\n")
	first := commit(t, root, "legacy")
	write(t, root, "legacy.txt", "legacy\nnew\n")
	head := commit(t, root, "change")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("Annotate did not warn about existing history")
	}
	data, ok, err := repo.ReadNote(head)
	if err != nil || !ok {
		t.Fatalf("ReadNote() = %t, %v", ok, err)
	}
	note, err := notes.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	ranges := note.Files["legacy.txt"].Ranges
	if len(ranges) != 2 || ranges[0].Author != model.AuthorUntracked || ranges[1].Author != model.AuthorHuman {
		t.Fatalf("legacy ranges = %+v", ranges)
	}
	if _, ok, err := repo.ReadNote(first); err != nil || ok {
		t.Fatalf("unexpected note on first commit: %t, %v", ok, err)
	}
}

func TestBlamePreservesLoneCarriageReturn(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "content\r")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 1 || result.Lines[0].Content != "content\r" {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestLinkedWorktreeKeepsStateSeparateAndNotesShared(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	mainRepo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(mainRepo); err != nil {
		t.Fatal(err)
	}

	linked := filepath.Join(t.TempDir(), "linked")
	git(t, root, "worktree", "add", "-b", "feature", linked)
	linked, err = filepath.EvalSymlinks(linked)
	if err != nil {
		t.Fatal(err)
	}
	linkedRepo, err := gitcmd.Discover(linked)
	if err != nil {
		t.Fatal(err)
	}
	if linkedRepo.GitDir == mainRepo.GitDir || linkedRepo.CommonDir != mainRepo.CommonDir {
		t.Fatalf("worktree directories are not isolated: main=%+v linked=%+v", mainRepo, linkedRepo)
	}

	write(t, linked, "file.txt", "base\nlinked\n")
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	if _, err := Capture(linkedRepo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	if retained, err := linkedRepo.ProtectedBlobCount(); err != nil || retained != 1 {
		t.Fatalf("linked retained snapshots = %d, %v", retained, err)
	}
	if retained, err := mainRepo.ProtectedBlobCount(); err != nil || retained != 0 {
		t.Fatalf("main retained snapshots = %d, %v", retained, err)
	}
	commit(t, linked, "linked")
	if _, err := Annotate(linkedRepo); err != nil {
		t.Fatal(err)
	}

	mainStatus, err := Status(mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	linkedStatus, err := Status(linkedRepo)
	if err != nil {
		t.Fatal(err)
	}
	if mainStatus.Head == linkedStatus.Head ||
		mainStatus.Head != mainStatus.LastAnnotatedCommit ||
		linkedStatus.Head != linkedStatus.LastAnnotatedCommit {
		t.Fatalf("worktree states are not separate: main=%+v linked=%+v", mainStatus, linkedStatus)
	}
	if _, found, err := mainRepo.ReadNote(linkedStatus.Head); err != nil || !found {
		t.Fatalf("shared linked-worktree note found = %t, error = %v", found, err)
	}
}

func TestNoopAnnotateKeepsPendingCheckpointBlobs(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	write(t, root, "file.txt", "base\npending\n")
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"file.txt"},
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	if result, err := Annotate(repo); err != nil || !result.Noop {
		t.Fatalf("Annotate() = %+v, %v", result, err)
	}
	git(t, root, "gc", "--prune=now")
	commit(t, root, "pending")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 2 || result.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestMissingCurrentNoteFailsClosed(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file.txt", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "notes", "--ref=refs/notes/byline", "remove", "HEAD")
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Annotate() error = %v", err)
	}
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Warnings) != 1 || !strings.Contains(status.Warnings[0], "missing") {
		t.Fatalf("Status() warnings = %v", status.Warnings)
	}
}

func TestUnsupportedFileDoesNotBlockLaterDeletion(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "binary", "a\x00b")
	commit(t, root, "binary")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil || len(result.Warnings) == 0 {
		t.Fatalf("Annotate(binary) = %+v, %v", result, err)
	}
	if err := os.Remove(filepath.Join(root, "binary")); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "delete binary")
	result, err = Annotate(repo)
	if err != nil || len(result.Warnings) == 0 {
		t.Fatalf("Annotate(delete binary) = %+v, %v", result, err)
	}
}

func TestPendingRenameKeepsAttribution(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "old.txt", "content\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	event := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test-model",
		Session: "session-1", Paths: []string{"old.txt", "new.txt"},
	}
	if _, err := Capture(repo, event, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "other.txt", "other\n")
	git(t, root, "add", "other.txt")
	git(t, root, "commit", "-m", "other")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}

	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", "rename")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	result, err := Blame(repo, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Lines) != 1 || result.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() lines = %+v", result.Lines)
	}
}

func TestDivergentHistoryFailsClosed(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file", "one\n")
	commit(t, root, "one")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "two\n")
	commit(t, root, "two")
	write(t, root, "file", "three\n")
	commit(t, root, "three")
	if _, err := Annotate(repo); err == nil || !strings.Contains(err.Error(), "gap") {
		t.Fatalf("Annotate error = %v", err)
	}
}

func TestCaptureSkipsUnsafePaths(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := Capture(repo, preset.Event{
		Type:  model.AuthorHuman,
		Paths: []string{"../outside", ".git/config"},
	}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Recorded != 0 || len(result.Warnings) != 2 {
		t.Fatalf("Capture() = %+v", result)
	}
}

func TestRenameDeleteAndUntrackedBlame(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "old.txt", "one\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	untracked, err := Blame(repo, "old.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(untracked.Lines) != 1 || untracked.Lines[0].Attribution.Author != model.AuthorUntracked {
		t.Fatalf("untracked blame = %+v", untracked)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"old.txt"}}
	if _, err := Capture(repo, human, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "new.txt", "one\ntwo\n")
	ai := preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test",
		Paths: []string{"old.txt", "new.txt"},
	}
	if _, err := Capture(repo, ai, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "rename")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	renamed, err := Blame(repo, "new.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(renamed.Lines) != 2 ||
		renamed.Lines[0].Attribution.Author != model.AuthorHuman ||
		renamed.Lines[1].Attribution.Author != model.AuthorAI {
		t.Fatalf("renamed blame = %+v", renamed)
	}
	if _, err := Capture(repo, preset.Event{Type: model.AuthorHuman, Paths: []string{"new.txt"}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Paths: []string{"new.txt"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "delete")
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Files != 0 {
		t.Fatalf("delete annotated %d files", result.Files)
	}
	if _, err := Blame(repo, "new.txt"); err == nil {
		t.Fatal("Blame accepted deleted path")
	}
}

func TestUnbornOperationsAndUnannotatedStatus(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err == nil {
		t.Fatal("Annotate accepted unborn repository")
	}
	if _, err := Blame(repo, "file"); err == nil {
		t.Fatal("Blame accepted unborn repository")
	}
	write(t, root, "file", "one\n")
	commit(t, root, "one")
	status, err := Status(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Warnings) == 0 {
		t.Fatal("Status did not warn about unannotated HEAD")
	}
}

func TestCheckpointAfterCommitCarriesToNextCommit(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, "file", "base\n")
	commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "base\ncommitted\n")
	commit(t, root, "first")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorHuman, Paths: []string{"file"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	write(t, root, "file", "base\ncommitted\nfuture-ai\n")
	if _, err := Capture(repo, preset.Event{
		Type: model.AuthorAI, Agent: "droid", Model: "test", Paths: []string{"file"},
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	status, err := Status(repo)
	if err != nil || status.PendingFiles != 1 {
		t.Fatalf("Status() = %+v, %v", status, err)
	}
	commit(t, root, "second")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	blame, err := Blame(repo, "file")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 3 || blame.Lines[2].Attribution.Author != model.AuthorAI {
		t.Fatalf("Blame() = %+v", blame)
	}
}

func testRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-b", "main")
	git(t, root, "config", "user.name", "Test User")
	git(t, root, "config", "user.email", "test@example.invalid")
	return root
}

func write(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commit(t *testing.T, root, message string) string {
	t.Helper()
	git(t, root, "add", "-A")
	git(t, root, "commit", "-m", message)
	return strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
