package provenance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

func TestPreviewRecoveryHandlesUnbornSnapshot(t *testing.T) {
	t.Parallel()
	report, err := previewRecovery(nil, nil, model.NewState(), "", "", nil)
	if err != nil || report.Version != model.StateVersion ||
		report.AnnotationPending || report.RecommendedAction != "" {
		t.Fatalf("previewRecovery(unborn) = %+v, %v", report, err)
	}
}

func TestPreviewRecoveryErrorBranches(t *testing.T) {
	t.Run("lock", func(t *testing.T) {
		gitDir := filepath.Join(t.TempDir(), "git-file")
		if err := os.WriteFile(gitDir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewRecovery(&gitcmd.Repo{GitDir: gitDir}); err == nil {
			t.Fatal("PreviewRecovery acquired a lock below a file")
		}
	})
	t.Run("checkpoint read", func(t *testing.T) {
		repo := &gitcmd.Repo{GitDir: t.TempDir()}
		dataStore := store.New(repo.GitDir)
		if err := os.MkdirAll(dataStore.CheckpointPath(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewRecovery(repo); err == nil {
			t.Fatal("PreviewRecovery read a checkpoint directory")
		}
	})
	t.Run("state read", func(t *testing.T) {
		repo := &gitcmd.Repo{GitDir: t.TempDir()}
		dataStore := store.New(repo.GitDir)
		if err := os.MkdirAll(dataStore.StatePath(), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := PreviewRecovery(repo); err == nil {
			t.Fatal("PreviewRecovery read a state directory")
		}
	})
	t.Run("head", func(t *testing.T) {
		repo := &gitcmd.Repo{Root: t.TempDir(), GitDir: t.TempDir()}
		if _, err := PreviewRecovery(repo); err == nil {
			t.Fatal("PreviewRecovery accepted an invalid repository")
		}
	})
	t.Run("branch", func(t *testing.T) {
		root := testRepo(t)
		write(t, root, "file.txt", "content\n")
		commit(t, root, "content")
		repo := fakeRewriteRepo(t, root, "branch-error", "")
		if _, err := PreviewRecovery(repo); err == nil {
			t.Fatal("PreviewRecovery accepted a branch read failure")
		}
	})
}

func TestRecoveryClassificationErrors(t *testing.T) {
	t.Parallel()
	repo := &gitcmd.Repo{}
	if _, err := previewRecovery(
		repo,
		nil,
		model.NewState(),
		"",
		"bad",
		nil,
	); err == nil {
		t.Fatal("previewRecovery accepted an invalid head")
	}
	if _, err := recoveryClassification(
		repo.NewBranchScanner(),
		map[string]recoveryBaseClassification{},
		model.Checkpoint{Seq: 1, BaseCommit: "-bad"},
	); err == nil {
		t.Fatal("recoveryClassification accepted an invalid base")
	}
}

func TestPreviewRecoveryReportsBlockedAndStrandedCheckpoints(t *testing.T) {
	t.Parallel()
	repo, root := setupStrandedBranchEvidence(t, false)
	write(t, root, "file.txt", "base\nmainline\n")
	commit(t, root, "mainline")

	blocked, err := PreviewRecovery(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.AnnotationPending ||
		blocked.UnrelatedCheckpoints != 2 ||
		blocked.StrandedCheckpoints != 0 ||
		blocked.BlockedCheckpoints != 2 ||
		blocked.RecommendedAction != "restore each checkpoint's recorded branch and base to consume it, or delete every listed branch and run git-byline recover" {
		t.Fatalf("blocked preview = %+v", blocked)
	}
	for _, checkpoint := range blocked.Checkpoints {
		if !checkpoint.ObjectPresent || checkpoint.Droppable ||
			strings.Join(checkpoint.Branches, ",") != "refs/heads/feature" {
			t.Fatalf("blocked checkpoint = %+v", checkpoint)
		}
	}

	git(t, root, "branch", "-D", "feature")
	stranded, err := PreviewRecovery(repo)
	if err != nil {
		t.Fatal(err)
	}
	if stranded.UnrelatedCheckpoints != 2 ||
		stranded.StrandedCheckpoints != 2 ||
		stranded.BlockedCheckpoints != 0 ||
		stranded.RecommendedAction != "git-byline recover --drop" {
		t.Fatalf("stranded preview = %+v", stranded)
	}
	for _, checkpoint := range stranded.Checkpoints {
		if !checkpoint.ObjectPresent || !checkpoint.Droppable || len(checkpoint.Branches) != 0 {
			t.Fatalf("stranded checkpoint = %+v", checkpoint)
		}
	}
}

func TestPreviewRecoveryReportsPreRootCheckpoint(t *testing.T) {
	t.Parallel()
	repo := setupPreRootEvidence(t)

	report, err := PreviewRecovery(repo)
	if err != nil {
		t.Fatal(err)
	}
	if report.UnrelatedCheckpoints != 1 ||
		report.StrandedCheckpoints != 1 ||
		len(report.Checkpoints) != 1 ||
		report.Checkpoints[0].BaseCommit != "" ||
		report.Checkpoints[0].ObjectPresent ||
		!report.Checkpoints[0].Droppable {
		t.Fatalf("pre-root preview = %+v", report)
	}
}

func setupPreRootEvidence(t *testing.T) *gitcmd.Repo {
	t.Helper()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	write(t, root, "file.txt", "unborn\n")
	if _, err := Capture(repo, human, now); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "root")
	write(t, root, "file.txt", "unborn\nmainline\n")
	if _, err := Capture(repo, human, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	commit(t, root, "mainline")
	return repo
}

func TestUnrelatedCheckpointDetails(t *testing.T) {
	t.Parallel()
	err := &unrelatedCheckpointError{Seq: 7, Base: strings.Repeat("a", 40)}
	seq, base, ok := UnrelatedCheckpoint(err)
	if !ok || seq != 7 || base != strings.Repeat("a", 40) {
		t.Fatalf("UnrelatedCheckpoint() = %d, %q, %t", seq, base, ok)
	}
	if _, _, ok := UnrelatedCheckpoint(nil); ok {
		t.Fatal("UnrelatedCheckpoint(nil) matched")
	}
}
