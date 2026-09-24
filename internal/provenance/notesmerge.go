package provenance

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"

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
	// Added counts the notes that came from the remote side.
	Added    int
	Warnings []string
}

// MergeRemoteNotes merges the attribution notes that the managed pre-push
// hook fetched into gitcmd.RemoteNotesRef. Remote notes may only add notes
// for commits that have none: local notes fast-forward when they are behind
// and merge when they diverged. When the remote side changed or removed a
// note that local notes hold, it returns gitcmd.ErrNotesConflict and leaves
// local notes unchanged. The common notes lock keeps annotate in another
// worktree from writing a note that the merge result would drop.
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
	local, err := notesTips(repo, remote)
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
	added, err := remoteAdditions(repo, relation, local, remote)
	if err != nil {
		return result, err
	}
	if relation == notesBehind {
		err = repo.UpdateRef(attributionNotesRef, remote, local, "git-byline: fast-forward remote attribution notes")
		result.FastForwarded = err == nil
	} else {
		err = repo.MergeNotes(remote)
		result.Merged = err == nil
	}
	if err == nil {
		result.Added = added
	}
	return result, err
}

// notesTips returns the local notes commit, or an empty value when there are
// no local notes yet, after checking that both notes refs hold commits. Git
// peels an annotated tag in ancestry checks, but a notes ref that holds one
// breaks every later git notes write.
func notesTips(repo *gitcmd.Repo, remote string) (string, error) {
	if err := requireCommit(repo, remote, "fetched remote notes"); err != nil {
		return "", err
	}
	local, found, err := repo.RefValue(attributionNotesRef)
	if err != nil || !found {
		return "", err
	}
	return local, requireCommit(repo, local, "local attribution notes")
}

func requireCommit(repo *gitcmd.Repo, oid, name string) error {
	kind, err := repo.ObjectType(oid)
	if err != nil {
		return fmt.Errorf("check %s: %w", name, err)
	}
	if kind != "commit" {
		return fmt.Errorf("%s point to a %s, not to a notes commit", name, kind)
	}
	return nil
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

// remoteAdditions counts the notes that the remote side adds and returns
// gitcmd.ErrNotesConflict when it changed or removed a note that local notes
// hold. git notes merge applies such a change without a conflict whenever the
// local side left the note alone, and a fast-forward applies every change,
// but git-byline never replaces an existing note on its own. The same change
// on both sides is fine. The base is the one git notes merge uses: local for
// a fast-forward, else the first merge base, or none for unrelated histories.
func remoteAdditions(repo *gitcmd.Repo, relation notesRelation, local, remote string) (int, error) {
	localNotes, err := noteBlobs(repo, local, "local attribution notes")
	if err != nil {
		return 0, err
	}
	baseNotes := localNotes
	if relation == notesDiverged {
		if baseNotes, err = mergeBaseNotes(repo, local, remote); err != nil {
			return 0, err
		}
	}
	remoteNotes, err := noteBlobs(repo, remote, "fetched remote notes")
	if err != nil {
		return 0, err
	}
	added, changed := classifyRemoteNotes(baseNotes, localNotes, remoteNotes)
	return added, changedNotesError(changed)
}

// mergeBaseNotes reads the notes of the first merge base, the one git notes
// merge picks. Unrelated histories have no base and start from no notes.
func mergeBaseNotes(repo *gitcmd.Repo, local, remote string) (map[string]string, error) {
	base, _, err := repo.FindMergeBase(local, remote)
	if err != nil {
		return nil, err
	}
	return noteBlobs(repo, base, "merge base notes")
}

// classifyRemoteNotes compares the note blob of every commit in base, local
// and remote. A remote note for a commit without a note in base and local
// is added. A remote change or removal of a note that local holds is
// changed, unless local has the same result.
func classifyRemoteNotes(base, local, remote map[string]string) (int, []string) {
	added := 0
	var changed []string
	for commit, remoteBlob := range remote {
		baseBlob, localBlob := base[commit], local[commit]
		switch {
		case remoteBlob == baseBlob || remoteBlob == localBlob:
		case baseBlob == "" && localBlob == "":
			added++
		default:
			changed = append(changed, commit)
		}
	}
	for commit := range base {
		if _, kept := remote[commit]; !kept && local[commit] != "" {
			changed = append(changed, commit)
		}
	}
	return added, changed
}

func noteBlobs(repo *gitcmd.Repo, notesCommit, name string) (map[string]string, error) {
	if notesCommit == "" {
		return map[string]string{}, nil
	}
	notes, err := repo.NoteBlobs(notesCommit)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return notes, nil
}

func changedNotesError(changed []string) error {
	switch len(changed) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("%w for commit %s", gitcmd.ErrNotesConflict, changed[0])
	}
	sort.Strings(changed)
	return fmt.Errorf("%w for commit %s and %d more", gitcmd.ErrNotesConflict, changed[0], len(changed)-1)
}
