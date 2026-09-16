package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/provenance"
)

func TestRecoverPreviewAndStatusExplainBlockedAttribution(t *testing.T) {
	t.Parallel()
	root, feature := setupAppStrandedRecovery(t)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	code, stdout, stderr, err := appRun(root, now, nil, "annotate")
	if code != ExitFailure || err == nil || stdout != "" ||
		!strings.Contains(stderr, "attribution pending for ") ||
		!strings.Contains(stderr, "checkpoint 1 belongs to base "+feature) ||
		!strings.Contains(stderr, "run: git-byline recover") {
		t.Fatalf("annotate failure = %d, %q, %q, %v", code, stdout, stderr, err)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "recover")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "Blocked checkpoints: 1") ||
		!strings.Contains(stdout, "Branch: refs/heads/feature") ||
		!strings.Contains(stdout, "Recommended action: annotate or delete the listed branches") ||
		!strings.Contains(stdout, "No checkpoints changed.") {
		t.Fatalf("recover preview = %d, %q, %q, %v", code, stdout, stderr, err)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "status")
	if code != ExitSuccess || err != nil ||
		!strings.Contains(stdout, "Annotation pending: true") ||
		!strings.Contains(stdout, "Unrelated checkpoints: 1") ||
		!strings.Contains(stdout, "Blocked checkpoints: 1") ||
		!strings.Contains(stdout, "Recommended action: git-byline recover") {
		t.Fatalf("status = %d, %q, %q, %v", code, stdout, stderr, err)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "recover", "--drop")
	if code != ExitFailure || err == nil || stdout != "" ||
		!strings.Contains(stderr, "refs/heads/feature") {
		t.Fatalf("blocked drop = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	report := readRecoveryPreview(t, root)
	if report.UnrelatedCheckpoints != 1 || report.BlockedCheckpoints != 1 {
		t.Fatalf("preview after blocked drop = %+v", report)
	}
}

func TestRecoverDropAnnotatesAfterBranchDeletion(t *testing.T) {
	t.Parallel()
	root, _ := setupAppStrandedRecovery(t)
	appGit(t, root, "branch", "-D", "feature")
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	code, stdout, stderr, err := appRun(root, now, nil, "recover", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("recover JSON preview = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var preview recoverCommandResult
	if err := json.Unmarshal([]byte(stdout), &preview); err != nil {
		t.Fatal(err)
	}
	if !preview.DryRun ||
		preview.Preview.StrandedCheckpoints != 1 ||
		preview.Preview.RecommendedAction != "git-byline recover --drop" ||
		len(preview.Preview.Checkpoints) != 1 ||
		!preview.Preview.Checkpoints[0].ObjectPresent ||
		!preview.Preview.Checkpoints[0].Droppable ||
		preview.Annotation != nil {
		t.Fatalf("recover JSON preview = %+v", preview)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "recover", "--drop", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("recover JSON drop = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var dropped recoverCommandResult
	if err := json.Unmarshal([]byte(stdout), &dropped); err != nil {
		t.Fatal(err)
	}
	if dropped.DryRun ||
		dropped.Annotation == nil ||
		dropped.Annotation.DroppedCheckpoints != 1 ||
		dropped.Annotation.Commit == "" {
		t.Fatalf("recover JSON drop = %+v", dropped)
	}

	code, stdout, stderr, err = appRun(root, now, nil, "status", "--json")
	if code != ExitSuccess || err != nil || stderr != "" {
		t.Fatalf("status JSON = %d, %q, %q, %v", code, stdout, stderr, err)
	}
	var status provenance.StatusResult
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatal(err)
	}
	if status.AnnotationPending ||
		status.UnrelatedCheckpoints != 0 ||
		status.StrandedCheckpoints != 0 ||
		status.BlockedCheckpoints != 0 ||
		status.RecommendedAction != "" {
		t.Fatalf("status after recovery = %+v", status)
	}
}

func TestRecoverUsage(t *testing.T) {
	t.Parallel()
	root := appRepo(t)
	for _, args := range [][]string{
		{"recover", "extra"},
		{"recover", "--unknown"},
	} {
		code, _, _, err := appRun(root, time.Time{}, nil, args...)
		if code != ExitUsage || err == nil {
			t.Fatalf("Run(%v) = %d, %v, want usage error", args, code, err)
		}
	}
	code, stdout, stderr, err := appRun(root, time.Time{}, nil, "recover", "--help")
	if code != ExitSuccess || err != nil || stderr != "" ||
		!strings.Contains(stdout, "Usage: git-byline recover") {
		t.Fatalf("recover help = %d, %q, %q, %v", code, stdout, stderr, err)
	}
}

func TestRecoverOperationalEdges(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	// Outside a repository, discovery fails before a preview is built.
	if code, _, _, err := appRun(t.TempDir(), now, nil, "recover"); code != ExitFailure || err == nil {
		t.Fatalf("recover outside repository = %d, %v", code, err)
	}

	// Malformed state turns the preview itself into an operational error.
	broken := appRepo(t)
	statePath := filepath.Join(broken, ".git", "byline", "state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _, err := appRun(broken, now, nil, "recover"); code != ExitFailure || err == nil {
		t.Fatalf("recover with malformed state = %d, %v", code, err)
	}

	// A pending checkpoint recorded before the first commit keeps an
	// empty base, and the preview reports it as such.
	preRoot := appRepo(t)
	appWrite(t, preRoot, "file.txt", "unborn\n")
	repo, err := gitcmd.Discover(preRoot)
	if err != nil {
		t.Fatal(err)
	}
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	if _, err := provenance.Capture(repo, human, now); err != nil {
		t.Fatal(err)
	}
	appCommit(t, preRoot, "root")
	appWrite(t, preRoot, "file.txt", "unborn\nmainline\n")
	if _, err := provenance.Capture(repo, human, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	appCommit(t, preRoot, "mainline")
	code, stdout, _, err := appRun(preRoot, now, nil, "recover")
	if code != ExitSuccess || err != nil ||
		!strings.Contains(stdout, "(before first commit)") {
		t.Fatalf("recover pre-root preview = %d, %q, %v", code, stdout, err)
	}

	// A plain drop prints the annotate result without JSON once the
	// stranded branch is gone.
	root, _ := setupAppStrandedRecovery(t)
	appGit(t, root, "branch", "-D", "feature")
	code, stdout, _, err = appRun(root, now, nil, "recover", "--drop")
	if code != ExitSuccess || err != nil || !strings.Contains(stdout, "annotated ") {
		t.Fatalf("plain recover drop = %d, %q, %v", code, stdout, err)
	}

	// Failing stdout writers surface as operational errors in both the
	// preview and the dropped JSON reports.
	failing := func(root string, args ...string) (int, error) {
		env := &Env{
			Stdin:  strings.NewReader(""),
			Stdout: coverageErrorWriter{err: errors.New("stdout unavailable")},
			Stderr: io.Discard,
			Dir:    root,
			Now:    func() time.Time { return now },
		}
		return Run(args, env)
	}
	if code, err := failing(appRepo(t), "recover", "--json"); code != ExitFailure || err == nil {
		t.Fatalf("recover JSON preview with failing stdout = %d, %v", code, err)
	}
	if code, err := failing(root, "recover", "--drop", "--json"); code != ExitFailure || err == nil {
		t.Fatalf("recover JSON drop with failing stdout = %d, %v", code, err)
	}

	// A droppable checkpoint among blocked ones exercises the skip in
	// the refusal listing.
	root, _ = setupAppStrandedRecovery(t)
	appGit(t, root, "checkout", "-b", "temp")
	appWrite(t, root, "file.txt", "base\ntemp\n")
	appCommit(t, root, "temp")
	repo, err = gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provenance.Capture(repo, preset.Event{
		Type:  model.AuthorHuman,
		Paths: []string{"file.txt"},
	}, now); err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "checkout", "main")
	appGit(t, root, "branch", "-D", "temp")
	code, _, stderr, err := appRun(root, now, nil, "recover", "--drop")
	if code != ExitFailure || err == nil || !strings.Contains(stderr, "refs/heads/feature") {
		t.Fatalf("mixed blocked drop = %d, %q, %v", code, stderr, err)
	}
}

func setupAppStrandedRecovery(t *testing.T) (string, string) {
	t.Helper()
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provenance.Annotate(repo); err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "checkout", "-b", "feature")
	appWrite(t, root, "file.txt", "base\nfeature\n")
	appCommit(t, root, "feature")
	feature := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	if _, err := provenance.Capture(
		repo,
		human,
		time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	); err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "checkout", "main")
	appWrite(t, root, "file.txt", "base\nmainline\n")
	appCommit(t, root, "mainline")
	return root, feature
}

func readRecoveryPreview(t *testing.T, root string) provenance.RecoveryReport {
	t.Helper()
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	report, err := provenance.PreviewRecovery(repo)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestRecoverDropReportsAnnotateFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	root := appRepo(t)
	appWrite(t, root, "file.txt", "base\n")
	appCommit(t, root, "base")
	appGit(t, root, "checkout", "-b", "temp")
	appWrite(t, root, "file.txt", "base\ntemp\n")
	appCommit(t, root, "temp")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	human := preset.Event{Type: model.AuthorHuman, Paths: []string{"file.txt"}}
	if _, err := provenance.Capture(repo, human, now); err != nil {
		t.Fatal(err)
	}
	appGit(t, root, "checkout", "main")
	appGit(t, root, "branch", "-D", "temp")

	// A different attribution note already sits on the head commit, so
	// dropping the stranded checkpoint still leaves the retry
	// annotation refusing to overwrite it.
	head := strings.TrimSpace(appGit(t, root, "rev-parse", "HEAD"))
	appGit(t, root, "notes", "--ref=byline", "add", "-f", "-m", `{"version":1,"files":{}}`, head)

	code, _, _, err := appRun(root, now, nil, "recover", "--drop")
	if code != ExitFailure || err == nil {
		t.Fatalf("recover drop with conflicting note = %d, %v", code, err)
	}
}
