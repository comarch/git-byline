package provenance

import (
	"fmt"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/store"
)

// RecoveryCheckpoint describes one unrelated checkpoint without changing it.
type RecoveryCheckpoint struct {
	Seq           uint64   `json:"seq"`
	BaseCommit    string   `json:"base_commit,omitempty"`
	BranchRef     string   `json:"branch_ref,omitempty"`
	ObjectPresent bool     `json:"object_present"`
	Branches      []string `json:"branches,omitempty"`
	Droppable     bool     `json:"droppable"`
}

// RecoveryReport previews explicit stranded checkpoint recovery.
type RecoveryReport struct {
	Version              int                  `json:"version"`
	Head                 string               `json:"head,omitempty"`
	AnnotationPending    bool                 `json:"annotation_pending"`
	UnrelatedCheckpoints int                  `json:"unrelated_checkpoints"`
	StrandedCheckpoints  int                  `json:"stranded_checkpoints"`
	BlockedCheckpoints   int                  `json:"blocked_checkpoints"`
	RecommendedAction    string               `json:"recommended_action,omitempty"`
	Checkpoints          []RecoveryCheckpoint `json:"checkpoints,omitempty"`
	Warnings             []string             `json:"warnings,omitempty"`
}

type recoveryBaseClassification struct {
	objectPresent bool
	branches      []string
	droppable     bool
}

// PreviewRecovery classifies unrelated checkpoints without changing state.
func PreviewRecovery(repo *gitcmd.Repo) (RecoveryReport, error) {
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RecoveryReport{}, err
	}
	defer held.Release()
	records, warnings, err := dataStore.ReadCheckpoints()
	if err != nil {
		return RecoveryReport{}, err
	}
	state, err := dataStore.ReadState()
	if err != nil {
		return RecoveryReport{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return RecoveryReport{}, err
	}
	branchRef, _, err := repo.CurrentBranchRef()
	if err != nil {
		return RecoveryReport{}, err
	}
	if err := held.Release(); err != nil {
		return RecoveryReport{}, fmt.Errorf("release recovery snapshot lock: %w", err)
	}
	return previewRecovery(repo, records, state, branchRef, head, warnings)
}

func previewRecovery(
	repo *gitcmd.Repo,
	records []model.Checkpoint,
	state model.State,
	branchRef string,
	head string,
	warnings []string,
) (RecoveryReport, error) {
	parent, err := recoveryParent(repo, head)
	if err != nil {
		return RecoveryReport{}, err
	}
	report := RecoveryReport{
		Version:  model.StateVersion,
		Head:     head,
		Warnings: warnings,
	}
	cache := map[string]recoveryBaseClassification{}
	scanner := repo.NewBranchScanner()
	for _, record := range records {
		if skipRecoveryRecord(record, state, branchRef, parent, head) {
			continue
		}
		classification, err := recoveryClassification(scanner, cache, record)
		if err != nil {
			return RecoveryReport{}, err
		}
		addRecoveryCheckpoint(&report, RecoveryCheckpoint{
			Seq:           record.Seq,
			BaseCommit:    record.BaseCommit,
			BranchRef:     record.BranchRef,
			ObjectPresent: classification.objectPresent,
			Branches:      classification.branches,
			Droppable:     classification.droppable,
		})
	}
	report.AnnotationPending = head != "" && head != state.LastAnnotatedCommit
	report.RecommendedAction = recoveryAction(report)
	return report, nil
}

func recoveryParent(repo *gitcmd.Repo, head string) (string, error) {
	if head == "" {
		return "", nil
	}
	return repo.Parent(head)
}

func skipRecoveryRecord(record model.Checkpoint, state model.State, branchRef, parent, head string) bool {
	return checkpointConsumed(record, state) ||
		recordMatchesContext(record, branchRef, parent) ||
		recordMatchesContext(record, branchRef, head)
}

func recoveryClassification(
	scanner *gitcmd.BranchScanner,
	cache map[string]recoveryBaseClassification,
	record model.Checkpoint,
) (recoveryBaseClassification, error) {
	if classification, ok := cache[record.BaseCommit]; ok {
		return classification, nil
	}
	classification := recoveryBaseClassification{}
	if record.BaseCommit == "" {
		classification.droppable = true
	} else {
		branches, exists, err := scanner.BranchesContaining(record.BaseCommit)
		if err != nil {
			return recoveryBaseClassification{}, fmt.Errorf(
				"check checkpoint %d base reachability: %w", record.Seq, err)
		}
		classification.objectPresent = exists
		classification.branches = branches
		classification.droppable = len(branches) == 0
	}
	cache[record.BaseCommit] = classification
	return classification, nil
}

func addRecoveryCheckpoint(report *RecoveryReport, checkpoint RecoveryCheckpoint) {
	report.Checkpoints = append(report.Checkpoints, checkpoint)
	report.UnrelatedCheckpoints++
	if checkpoint.Droppable {
		report.StrandedCheckpoints++
	} else {
		report.BlockedCheckpoints++
	}
}

func recoveryAction(report RecoveryReport) string {
	switch {
	case report.BlockedCheckpoints > 0:
		return "restore each checkpoint's recorded branch and base to consume it, or delete every listed branch and run git-byline recover"
	case report.StrandedCheckpoints > 0:
		return "git-byline recover --drop"
	case report.AnnotationPending:
		return "git-byline annotate"
	default:
		return ""
	}
}
