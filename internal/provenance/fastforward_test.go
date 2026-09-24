package provenance

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

// annotatedRepo returns a repository on main whose base commit adds path
// and carries a note.
func annotatedRepo(t *testing.T, path string) (string, *gitcmd.Repo, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, path, "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	return root, repo, base
}

// fastForwardRepo returns an annotated repository on main and the tip of
// branch forge, count commits ahead without notes. The forge commits stand
// for commits made in another clone or on the forge.
func fastForwardRepo(t *testing.T, count int) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root, repo, base := annotatedRepo(t, "forge.txt")
	git(t, root, "checkout", "-q", "-b", "forge")
	content := "base\n"
	for i := 1; i <= count; i++ {
		content += fmt.Sprintf("forge %d\n", i)
		write(t, root, "forge.txt", content)
		commit(t, root, fmt.Sprintf("forge %d", i))
	}
	tip := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	git(t, root, "checkout", "-q", "main")
	return root, repo, base, tip
}

// runFastForwardHooks runs what the managed hooks run after main
// fast-forwards from old to tip: reference-transaction, then post-merge
// with its annotate step. It returns the reference-transaction result.
func runFastForwardHooks(t *testing.T, repo *gitcmd.Repo, old, tip string) RewriteResult {
	t.Helper()
	input := old + " " + tip + " refs/heads/main\n"
	transaction, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed")
	if err != nil {
		t.Fatal(err)
	}
	if result, err := HandlePostMerge(repo); err != nil || result.Written != 0 {
		t.Fatalf("post-merge after fast-forward = %+v, %v", result, err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Skipped || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "fast-forwarded") {
		t.Fatalf("annotate after fast-forward = %+v, want skipped", result)
	}
	return transaction
}

func captureAI(t *testing.T, repo *gitcmd.Repo, session, path string) {
	t.Helper()
	if _, err := Capture(repo, presetAI(session, path), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func readCheckpointsAndState(t *testing.T, repo *gitcmd.Repo) ([]model.Checkpoint, model.State) {
	t.Helper()
	dataStore := store.New(repo.GitDir)
	records, _, err := dataStore.ReadCheckpoints()
	if err != nil {
		t.Fatal(err)
	}
	state, err := dataStore.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	return records, state
}

// assertNoParkedCheckpoints checks that nothing is left for recover to
// report after the commit that follows a fast-forward.
func assertNoParkedCheckpoints(t *testing.T, repo *gitcmd.Repo, result AnnotateResult) {
	t.Helper()
	if result.ParkedCheckpoints != 0 {
		t.Fatalf("annotate parked checkpoints: %+v", result)
	}
	report, err := PreviewRecovery(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnrelatedCheckpoints != 0 || report.BlockedCheckpoints != 0 {
		t.Fatalf("recovery report = %+v, want no parked checkpoints", report)
	}
}

// commitAndAnnotate commits the worktree on main over parent and runs the
// post-commit flow.
func commitAndAnnotate(t *testing.T, root string, repo *gitcmd.Repo, parent, message string) AnnotateResult {
	t.Helper()
	local := commit(t, root, message)
	input := parent + " " + local + " refs/heads/main\n"
	if _, err := HandleReferenceTransaction(repo, strings.NewReader(input), "committed"); err != nil {
		t.Fatal(err)
	}
	result, err := Annotate(repo)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// commitLocalLine appends a line to path, commits it, and runs the
// post-commit flow.
func commitLocalLine(t *testing.T, root string, repo *gitcmd.Repo, path, parent string) AnnotateResult {
	t.Helper()
	write(t, root, path, git(t, root, "show", "HEAD:"+path)+"local\n")
	return commitAndAnnotate(t, root, repo, parent, "local")
}

func hasInitializingWarning(warnings []string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, "initializing attribution") {
			return true
		}
	}
	return false
}

func TestFastForwardDoesNotAnnotateForeignCommits(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		commits int
		move    []string
	}{
		{"merge --ff-only over one commit", 1, []string{"merge", "-q", "--ff-only", "forge"}},
		{"pull --ff-only over two commits", 2, []string{"pull", "-q", "--ff-only", ".", "forge"}},
		{"merge --ff with an ignored message", 1, []string{"merge", "-q", "--ff", "-m", "ignored", "forge"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, repo, base, tip := fastForwardRepo(t, tc.commits)
			git(t, root, tc.move...)
			runFastForwardHooks(t, repo, base, tip)
			for _, forged := range strings.Fields(git(t, root, "rev-list", base+".."+tip)) {
				if _, found, err := repo.ReadNote(forged); err != nil || found {
					t.Fatalf("note on fast-forwarded commit %s: found=%t err=%v", forged, found, err)
				}
			}
			state, err := store.New(repo.GitDir).ReadState()
			if err != nil {
				t.Fatal(err)
			}
			if state.LastAnnotatedCommit != "" {
				t.Fatalf("boundary after fast-forward = %q, want cleared", state.LastAnnotatedCommit)
			}
			// Without a note for the fast-forwarded lines, the next local
			// commit keeps them untracked instead of claiming them as human.
			result := commitLocalLine(t, root, repo, "forge.txt", tip)
			if !hasInitializingWarning(result.Warnings) {
				t.Fatalf("annotate without a parent note = %+v, want initializing warning", result)
			}
			blame, err := Blame(repo, "forge.txt")
			if err != nil {
				t.Fatal(err)
			}
			last := len(blame.Lines) - 1
			for _, line := range blame.Lines[:last] {
				if line.Attribution.Author != model.AuthorUntracked {
					t.Fatalf("fast-forwarded line = %+v, want untracked", line)
				}
			}
			if blame.Lines[last].Attribution.Author != model.AuthorHuman {
				t.Fatalf("local line = %+v, want human", blame.Lines[last])
			}
		})
	}
}

func TestFastForwardStartsFromFetchedNote(t *testing.T) {
	t.Parallel()
	root, repo, base := annotatedRepo(t, "forge.txt")
	// The forge branch annotates an AI commit, like the clone that made it;
	// main then has the commit and its note before the fast-forward.
	git(t, root, "checkout", "-q", "-b", "forge")
	write(t, root, "forge.txt", "base\nforge ai\n")
	captureAI(t, repo, "forge-session", "forge.txt")
	tip := commit(t, root, "forge ai")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	before, found, err := repo.ReadNote(tip)
	if err != nil || !found {
		t.Fatalf("forge note = %t, %v", found, err)
	}
	git(t, root, "checkout", "-q", "main")
	if _, err := HandlePostCheckout(repo, tip, base); err != nil {
		t.Fatal(err)
	}

	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)
	after, found, err := repo.ReadNote(tip)
	if err != nil || !found || !bytes.Equal(after, before) {
		t.Fatalf("forge note changed by fast-forward: found=%t err=%v", found, err)
	}
	state, err := store.New(repo.GitDir).ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.LastAnnotatedCommit != tip {
		t.Fatalf("boundary after fast-forward = %q, want %s", state.LastAnnotatedCommit, tip)
	}

	commitLocalLine(t, root, repo, "forge.txt", tip)
	blame, err := Blame(repo, "forge.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Author{model.AuthorHuman, model.AuthorAI, model.AuthorHuman}
	if len(blame.Lines) != len(want) {
		t.Fatalf("blame lines = %+v", blame.Lines)
	}
	for i, author := range want {
		if blame.Lines[i].Attribution.Author != author {
			t.Fatalf("line %d = %+v, want %s", i+1, blame.Lines[i], author)
		}
	}
}

func TestFastForwardContinuesFromLaterFetchedNote(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := fastForwardRepo(t, 1)
	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)
	// The forge workflow note is fetched after the pull cleared the boundary.
	blob := mustBlob(t, repo, tip, "forge.txt")
	if err := repo.WriteNote(tip, encodeCoverageNote(t, makeCoverageNoteForContent(blob, "forge.txt", 2))); err != nil {
		t.Fatal(err)
	}

	result := commitLocalLine(t, root, repo, "forge.txt", tip)
	if hasInitializingWarning(result.Warnings) {
		t.Fatalf("annotate over a noted parent = %+v, want no initializing warning", result)
	}
	blame, err := Blame(repo, "forge.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 3 {
		t.Fatalf("blame lines = %+v", blame.Lines)
	}
	for i, line := range blame.Lines {
		if line.Attribution.Author != model.AuthorHuman {
			t.Fatalf("line %d = %+v, want human from the fetched note", i+1, line)
		}
	}
}

func TestFastForwardKeepsUncommittedAIEvidence(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := fastForwardRepo(t, 1)
	// An agent edit in progress on a path the fast-forward does not touch.
	write(t, root, "local.txt", "ai\n")
	captureAI(t, repo, "local-session", "local.txt")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)
	records, _ := readCheckpointsAndState(t, repo)
	for _, record := range records {
		if record.BaseCommit != tip {
			t.Fatalf("checkpoint %d base = %s, want fast-forwarded %s", record.Seq, record.BaseCommit, tip)
		}
	}

	result := commitAndAnnotate(t, root, repo, tip, "local ai")
	if result.ParkedCheckpoints != 0 {
		t.Fatalf("annotate parked the agent edit: %+v", result)
	}
	blame, err := Blame(repo, "local.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 1 || blame.Lines[0].Attribution.Author != model.AuthorAI {
		t.Fatalf("local.txt = %+v, want ai", blame.Lines)
	}
}

func TestFastForwardConsumesCheckpointsOnChangedPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		commits int
		rename  string
	}{
		{"edited path", 1, ""},
		// Rename detection reports the edited path only as the rename source.
		{"renamed path", 0, "renamed.txt"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root, repo, base, tip := fastForwardRepo(t, tc.commits)
			path := "forge.txt"
			if tc.rename != "" {
				git(t, root, "checkout", "-q", "forge")
				git(t, root, "mv", path, tc.rename)
				tip = commit(t, root, "rename")
				git(t, root, "checkout", "-q", "main")
				path = tc.rename
			}
			// A reverted agent edit leaves one checkpoint on a path the
			// fast-forward then changes. That checkpoint also opened the lane.
			write(t, root, "forge.txt", "base\nai\n")
			captureAI(t, repo, "reverted-session", "forge.txt")
			write(t, root, "forge.txt", "base\n")
			git(t, root, "merge", "-q", "--ff-only", "forge")
			transaction := runFastForwardHooks(t, repo, base, tip)
			if len(transaction.Warnings) != 1 ||
				!strings.Contains(transaction.Warnings[0], "changed 1 paths") ||
				!strings.Contains(transaction.Warnings[0], "consumed 1 checkpoints") {
				t.Fatalf("fast-forward warnings = %q, want consumed checkpoints", transaction.Warnings)
			}
			records, state := readCheckpointsAndState(t, repo)
			if len(records) != 1 || records[0].BaseCommit != base || !checkpointConsumed(records[0], state) {
				t.Fatalf("checkpoints after fast-forward = %+v, want one consumed on %s", records, base)
			}
			assertNoParkedCheckpoints(t, repo, commitLocalLine(t, root, repo, path, tip))
		})
	}
}

func TestFastForwardMovesOnlyCheckpointsOnUntouchedPaths(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := fastForwardRepo(t, 1)
	// Agent work on local.txt interleaves with an agent edit on forge.txt
	// that is reverted before the fast-forward changes forge.txt.
	write(t, root, "local.txt", "one\n")
	captureAI(t, repo, "local-session", "local.txt")
	write(t, root, "forge.txt", "base\nai\n")
	captureAI(t, repo, "reverted-session", "forge.txt")
	write(t, root, "forge.txt", "base\n")
	write(t, root, "local.txt", "one\ntwo\n")
	captureAI(t, repo, "local-session", "local.txt")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	transaction := runFastForwardHooks(t, repo, base, tip)
	if len(transaction.Warnings) != 1 || !strings.Contains(transaction.Warnings[0], "consumed 1 checkpoints") {
		t.Fatalf("fast-forward warnings = %q, want consumed checkpoints", transaction.Warnings)
	}
	records, state := readCheckpointsAndState(t, repo)
	if len(records) != 3 {
		t.Fatalf("checkpoints after fast-forward = %+v", records)
	}
	for _, record := range records {
		changed := record.Files[0].Path == "forge.txt"
		consumed := checkpointConsumed(record, state)
		if changed && (record.BaseCommit != base || !consumed) {
			t.Fatalf("checkpoint %d on the changed path = %+v, want consumed on %s", record.Seq, record, base)
		}
		if !changed && (record.BaseCommit != tip || consumed) {
			t.Fatalf("checkpoint %d on an untouched path = %+v, want pending on %s", record.Seq, record, tip)
		}
	}

	assertNoParkedCheckpoints(t, repo, commitAndAnnotate(t, root, repo, tip, "local ai"))
	blame, err := Blame(repo, "local.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(blame.Lines) != 2 {
		t.Fatalf("local.txt blame = %+v", blame.Lines)
	}
	for i, line := range blame.Lines {
		if line.Attribution.Author != model.AuthorAI {
			t.Fatalf("local.txt line %d = %+v, want ai", i+1, line)
		}
	}
}

func TestFastForwardKeepsLegacyCheckpointsParked(t *testing.T) {
	t.Parallel()
	root, repo, base, tip := fastForwardRepo(t, 1)
	// A version 1 checkpoint from an older release has no branch lane, so
	// it cannot be split from the agent work that follows it.
	legacy := model.Checkpoint{
		Version:    model.CheckpointVersionV1,
		Kind:       model.CheckpointKindEdit,
		Seq:        1,
		BaseCommit: base,
		TS:         time.Now().UTC().Format(time.RFC3339Nano),
		Type:       model.AuthorHuman,
		Files:      []model.Snapshot{{Path: "forge.txt", Exists: true, Blob: mustBlob(t, repo, base, "forge.txt")}},
	}
	if err := store.New(repo.GitDir).AppendCheckpoint(legacy); err != nil {
		t.Fatal(err)
	}
	write(t, root, "local.txt", "ai\n")
	captureAI(t, repo, "local-session", "local.txt")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	transaction := runFastForwardHooks(t, repo, base, tip)
	if len(transaction.Warnings) != 1 || !strings.Contains(transaction.Warnings[0], "stay parked") {
		t.Fatalf("fast-forward warnings = %q, want parked checkpoints", transaction.Warnings)
	}
	records, _ := readCheckpointsAndState(t, repo)
	if len(records) != 2 {
		t.Fatalf("checkpoints after fast-forward = %+v", records)
	}
	for _, record := range records {
		if record.BaseCommit != base {
			t.Fatalf("checkpoint %d base = %s, want parked on %s", record.Seq, record.BaseCommit, base)
		}
	}
}

// splitFastForwardRepo fast-forwards main over one forge commit with one
// checkpoint on the changed path and one on an untouched path.
func splitFastForwardRepo(t *testing.T) (string, string, string) {
	t.Helper()
	root, repo, base, tip := fastForwardRepo(t, 1)
	write(t, root, "forge.txt", "base\nai\n")
	captureAI(t, repo, "reverted-session", "forge.txt")
	write(t, root, "forge.txt", "base\n")
	write(t, root, "local.txt", "ai\n")
	captureAI(t, repo, "local-session", "local.txt")
	git(t, root, "merge", "-q", "--ff-only", "forge")
	return root, base, tip
}

func TestFastForwardCheckpointMoveFailure(t *testing.T) {
	t.Run("restores state", func(t *testing.T) {
		skipIfUnsupportedPermissions(t)
		root, base, tip := splitFastForwardRepo(t)
		fake := fakeRewriteRepo(t, root, "checkpoint-rewrite-write-error", "")
		dataStore := store.New(fake.GitDir)
		t.Setenv("FAKE_GIT_STORE_DIR", dataStore.Dir)
		defer os.Chmod(dataStore.Dir, 0o700)
		input := base + " " + tip + " refs/heads/main\n"
		_, err := HandleReferenceTransaction(fake, strings.NewReader(input), "committed")
		if err == nil ||
			!strings.Contains(err.Error(), "move checkpoints to fast-forwarded commit") ||
			strings.Contains(err.Error(), "rollback state") {
			t.Fatalf("checkpoint move error = %v", err)
		}
		state, err := dataStore.ReadState()
		if err != nil {
			t.Fatal(err)
		}
		if state.LastAnnotatedCommit != base || state.Pending.BaseCommit != base || len(state.Lanes["refs/heads/main"]) != 0 {
			t.Fatalf("state after failed checkpoint move = %+v, want restored", state)
		}
	})
	t.Run("reports rollback failure", func(t *testing.T) {
		root, base, tip := splitFastForwardRepo(t)
		fake := fakeRewriteRepo(t, root, "checkpoint-rewrite-error", "")
		t.Setenv("FAKE_GIT_CHECKPOINT_PATH", store.New(fake.GitDir).CheckpointPath())
		input := base + " " + tip + " refs/heads/main\n"
		_, err := HandleReferenceTransaction(fake, strings.NewReader(input), "committed")
		if err == nil || !strings.Contains(err.Error(), "rollback state") {
			t.Fatalf("checkpoint move rollback error = %v", err)
		}
	})
}

func TestFastForwardReportsFailures(t *testing.T) {
	cases := []struct {
		name string
		mode string
		// dir returns a store path that is replaced by a directory.
		dir  func(store.Store) string
		want string
	}{
		{"reflog", "reflog-error", nil, "forced reflog failure"},
		{"worktree lock", "", store.Store.LockPath, "open lock"},
		{"state", "", store.Store.StatePath, "read state"},
		{"branch", "branch-error", nil, "forced branch failure"},
		{"checkpoint log", "", store.Store.CheckpointPath, "read checkpoint"},
		{"changed paths", "changes-error", nil, "forced changes failure"},
		{"target note", "notes-read-error", nil, "forced notes read failure"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Two forge commits keep the tip parent off the old tip, so
			// only the fast-forward check reads the reflog.
			root, repo, base, tip := fastForwardRepo(t, 2)
			// A checkpoint on the old tip makes the fast-forward read the
			// changed paths.
			write(t, root, "local.txt", "ai\n")
			captureAI(t, repo, "local-session", "local.txt")
			git(t, root, "merge", "-q", "--ff-only", "forge")
			if tc.mode != "" {
				repo = fakeRewriteRepo(t, root, tc.mode, tip)
			}
			if tc.dir != nil {
				path := tc.dir(store.New(repo.GitDir))
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			update := rewrite.RefUpdate{Ref: "refs/heads/main", Old: base, New: tip}
			if _, err := handleHeadMove(repo, update); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("handleHeadMove() = %v, want error with %q", err, tc.want)
			}
		})
	}
}

func TestFastForwardSkipsForeignCherryPickMarker(t *testing.T) {
	t.Parallel()
	root, repo, base := annotatedRepo(t, "file.txt")
	git(t, root, "checkout", "-q", "-b", "topic")
	write(t, root, "file.txt", "base\nsource\n")
	captureAI(t, repo, "cherry-session", "file.txt")
	source := commit(t, root, "source")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "-b", "forge", base)
	if _, err := HandlePostCheckout(repo, source, base); err != nil {
		t.Fatal(err)
	}
	// Another clone cherry-picks with -x; it maps its own note.
	git(t, root, "cherry-pick", "-x", source)
	tip := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	git(t, root, "checkout", "-q", "main")

	git(t, root, "merge", "-q", "--ff-only", "forge")
	runFastForwardHooks(t, repo, base, tip)
	if _, found, err := repo.ReadNote(tip); err != nil || found {
		t.Fatalf("note on fast-forwarded cherry-pick: found=%t err=%v", found, err)
	}
}
