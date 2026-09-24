package provenance

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
)

const attributionNotesRef = "refs/notes/byline"

type notesRelation int

const (
	notesUpToDate notesRelation = iota
	notesBehind
	notesDiverged
)

var errNotesMergeInProgress = errors.New(
	"a git notes merge is in progress in this worktree; finish it with " +
		"'git notes merge --commit' or 'git notes merge --abort'")

// NotesMergeResult reports how fetched remote notes changed local notes.
type NotesMergeResult struct {
	FastForwarded bool
	Merged        bool
	Warnings      []string
}

// MergeRemoteNotes merges the attribution notes that the managed pre-push
// hook fetched into gitcmd.RemoteNotesRef. Local notes fast-forward when they
// are behind and merge when they diverged. When both sides changed the note
// of one commit, it returns gitcmd.ErrNotesConflict and leaves local notes
// unchanged. The common notes lock keeps annotate in another worktree from
// writing a note that the merge result would drop.
func MergeRemoteNotes(repo *gitcmd.Repo) (result NotesMergeResult, err error) {
	commonLock, err := lock.Acquire(filepath.Join(repo.CommonDir, "byline", "notes.lock"), lockTimeout)
	if err != nil {
		return result, err
	}
	defer commonLock.Release()
	remote, found, err := repo.RefValue(gitcmd.RemoteNotesRef)
	if err != nil || !found {
		return result, err
	}
	defer func() {
		if deleteErr := repo.DeleteRef(gitcmd.RemoteNotesRef, remote); deleteErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("could not remove %s: %v", gitcmd.RemoteNotesRef, deleteErr))
		}
	}()
	local, _, err := repo.RefValue(attributionNotesRef)
	if err != nil {
		return result, err
	}
	relation, err := compareNotes(repo, local, remote)
	if err != nil || relation == notesUpToDate {
		return result, err
	}
	inProgress, err := repo.NotesMergeInProgress()
	if err != nil {
		return result, err
	}
	if inProgress {
		return result, errNotesMergeInProgress
	}
	if relation == notesBehind {
		err = repo.UpdateRef(attributionNotesRef, remote, local, "git-byline: fast-forward remote attribution notes")
		result.FastForwarded = err == nil
		return result, err
	}
	err = repo.MergeNotes(gitcmd.RemoteNotesRef)
	result.Merged = err == nil
	return result, err
}

// compareNotes classifies local notes against fetched remote notes. An
// empty local value means the local notes ref does not exist yet.
func compareNotes(repo *gitcmd.Repo, local, remote string) (notesRelation, error) {
	if local == "" {
		return notesBehind, nil
	}
	if local == remote {
		return notesUpToDate, nil
	}
	containsRemote, err := repo.IsAncestor(remote, local)
	if err != nil || containsRemote {
		return notesUpToDate, err
	}
	behind, err := repo.IsAncestor(local, remote)
	if err != nil || behind {
		return notesBehind, err
	}
	return notesDiverged, nil
}
