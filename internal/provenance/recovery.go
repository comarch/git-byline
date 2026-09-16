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
	if err := held.Release(); err != nil {
		return RecoveryReport{}, fmt.Errorf("release recovery snapshot lock: %w", err)
	}
	return previewRecovery(repo, records, state, head, warnings)
}

func previewRecovery(
	repo *gitcmd.Repo,
	records []model.Checkpoint,
	state model.State,
	head string,
	warnings []string,
) (RecoveryReport, error) {
	parent := ""
	if head != "" {
		var err error
		parent, err = repo.Parent(head)
		if err != nil {
			return RecoveryReport{}, err
		}
	}
	report := RecoveryReport{
		Version:  model.StateVersion,
		Head:     head,
		Warnings: warnings,
	}
	type baseClassification struct {
		objectPresent bool
		branches      []string
		droppable     bool
	}
	cache := map[string]baseClassification{}
	scanner := repo.NewBranchScanner()
	for _, record := range records {
		if record.Seq <= state.LastCheckpointSeq ||
			record.BaseCommit == parent ||
			record.BaseCommit == head {
			continue
		}
		classification, ok := cache[record.BaseCommit]
		if !ok {
			if record.BaseCommit == "" {
				classification.droppable = true
			} else {
				branches, exists, err := scanner.BranchesContaining(record.BaseCommit)
				if err != nil {
					return RecoveryReport{}, fmt.Errorf(
						"check checkpoint %d base reachability: %w", record.Seq, err)
				}
				classification.objectPresent = exists
				classification.branches = branches
				classification.droppable = len(branches) == 0
			}
			cache[record.BaseCommit] = classification
		}
		checkpoint := RecoveryCheckpoint{
			Seq:           record.Seq,
			BaseCommit:    record.BaseCommit,
			ObjectPresent: classification.objectPresent,
			Branches:      classification.branches,
			Droppable:     classification.droppable,
		}
		report.Checkpoints = append(report.Checkpoints, checkpoint)
		report.UnrelatedCheckpoints++
		if checkpoint.Droppable {
			report.StrandedCheckpoints++
		} else {
			report.BlockedCheckpoints++
		}
	}
	report.AnnotationPending = head != "" && head != state.LastAnnotatedCommit
	switch {
	case report.BlockedCheckpoints > 0:
		report.RecommendedAction = "annotate or delete the listed branches, then run git-byline recover"
	case report.StrandedCheckpoints > 0:
		report.RecommendedAction = "git-byline recover --drop"
	case report.AnnotationPending:
		report.RecommendedAction = "git-byline annotate"
	}
	return report, nil
}
