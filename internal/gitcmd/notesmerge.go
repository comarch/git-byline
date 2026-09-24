package gitcmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/comarch/git-byline/internal/model"
)

// RemoteNotesRef holds the attribution notes that the managed pre-push hook
// fetched from the push remote until merge-notes merges them. It lives under
// refs/worktree/ so concurrent pushes from linked worktrees never share it.
const RemoteNotesRef = "refs/worktree/byline/remote-notes"

// ErrNotesConflict reports local and remote notes that differ for the same
// commit. The local notes ref stays unchanged.
var ErrNotesConflict = errors.New("local and remote attribution notes differ for the same commit")

// IsAncestor reports whether ancestor is reachable from descendant.
func (repo *Repo) IsAncestor(ancestor, descendant string) (bool, error) {
	if !model.ValidObjectID(ancestor) || !model.ValidObjectID(descendant) {
		return false, errors.New("invalid ancestry object ID")
	}
	_, err := repo.run("check ancestry", nil, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// UpdateRef moves ref to newValue only while it still holds oldValue. An
// empty oldValue requires that ref does not exist yet.
func (repo *Repo) UpdateRef(ref, newValue, oldValue, reason string) error {
	if err := validateRefName(ref); err != nil {
		return err
	}
	if !model.ValidObjectID(newValue) || (oldValue != "" && !model.ValidObjectID(oldValue)) {
		return errors.New("invalid ref object ID")
	}
	_, err := repo.run("update ref", nil, "update-ref", "-m", reason, ref, newValue, oldValue)
	return err
}

// DeleteRef removes ref only while it still holds oldValue.
func (repo *Repo) DeleteRef(ref, oldValue string) error {
	if err := validateRefName(ref); err != nil {
		return err
	}
	if !model.ValidObjectID(oldValue) {
		return errors.New("invalid ref object ID")
	}
	_, err := repo.run("delete ref", nil, "update-ref", "-d", ref, oldValue)
	return err
}

// NotesMergeInProgress reports an unfinished git notes merge in this
// worktree. It may hold hand-resolved notes, so an automatic merge must
// neither join nor abort it.
func (repo *Repo) NotesMergeInProgress() (bool, error) {
	if _, found, err := repo.RefValue("NOTES_MERGE_PARTIAL"); err != nil || found {
		return found, err
	}
	path, err := repo.GitPath("NOTES_MERGE_WORKTREE")
	if err != nil {
		return false, err
	}
	entries, err := os.ReadDir(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read notes merge worktree: %w", err)
	}
	return len(entries) > 0, nil
}

// MergeNotes merges remote into the attribution notes ref. The explicit
// manual strategy keeps a configured notes.mergeStrategy from picking a side
// on its own. A conflict is aborted and returns ErrNotesConflict; git notes
// merge exits 1 only for conflicts.
func (repo *Repo) MergeNotes(remote string) error {
	if err := validateRevision(remote, "remote notes"); err != nil {
		return err
	}
	_, err := repo.run("merge attribution notes", nil,
		"notes", "--ref="+bylineNotesRef, "merge", "--strategy=manual", "--quiet", remote)
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.ExitCode != 1 {
		return noteWriteError(err)
	}
	if _, err := repo.run("abort attribution notes merge", nil,
		"notes", "--ref="+bylineNotesRef, "merge", "--abort"); err != nil {
		return fmt.Errorf("abort conflicting notes merge: %w", err)
	}
	return ErrNotesConflict
}
