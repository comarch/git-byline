package provenance

import (
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
)

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
		blocked.RecommendedAction != "annotate or delete the listed branches, then run git-byline recover" {
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
