package provenance

import (
	"errors"
	"fmt"
	"maps"
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
// local notes unchanged. The common notes lock keeps annotate and rewrite in
// another worktree from writing a note that the merge result would drop.
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
	if err := repo.CheckNotesHistory(remote, local); err != nil {
		return result, fmt.Errorf("check fetched remote notes: %w", err)
	}
	expected, added, err := remoteAdditions(repo, relation, local, remote)
	if err != nil {
		return result, err
	}
	merged := remote
	if relation == notesBehind {
		err = repo.UpdateRef(attributionNotesRef, remote, local, "git-byline: fast-forward remote attribution notes")
	} else if err = repo.MergeNotes(remote); err == nil {
		merged, err = mergeCommit(repo, local, remote)
	}
	if err == nil {
		err = verifyMergedNotes(repo, local, merged, expected)
	}
	if err != nil {
		return result, err
	}
	result.FastForwarded = relation == notesBehind
	result.Merged = relation == notesDiverged
	result.Added = added
	return result, nil
}

// mergeCommit returns the notes commit that git notes merge wrote. Its
// parents must be local and remote; anything else means another writer
// moved the notes ref during the merge, and a rollback would drop its work.
func mergeCommit(repo *gitcmd.Repo, local, remote string) (string, error) {
	merged, found, err := repo.RefValue(attributionNotesRef)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("attribution notes ref disappeared during the merge")
	}
	parents, err := repo.Parents(merged)
	if err != nil {
		return "", fmt.Errorf("read merged notes parents: %w", err)
	}
	if len(parents) != 2 || parents[0] != local || parents[1] != remote {
		return "", fmt.Errorf("attribution notes moved to %s during the merge; check refs/notes/byline", merged)
	}
	return merged, nil
}

// verifyMergedNotes reads the result back with git notes list, which reads a
// notes tree the way every Git command does. It must hold exactly the local
// notes plus the added ones. A remote tree that NoteBlobs reads differently
// from Git fails here, for example an entry name with a slash that ls-tree
// shows like a fanout path. The notes ref then moves back to local.
func verifyMergedNotes(repo *gitcmd.Repo, local, merged string, expected map[string]string) error {
	listed, err := repo.ListNotes()
	if err == nil && maps.Equal(listed, expected) {
		return nil
	}
	if err == nil {
		err = errors.New("merged attribution notes differ from the checked remote notes")
	}
	var restoreErr error
	if local == "" {
		restoreErr = repo.DeleteRef(attributionNotesRef, merged)
	} else {
		restoreErr = repo.UpdateRef(attributionNotesRef, local, merged, "git-byline: restore local attribution notes")
	}
	if restoreErr != nil {
		return errors.Join(err, fmt.Errorf("restore local attribution notes: %w", restoreErr))
	}
	return fmt.Errorf("%w; local notes are unchanged", err)
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

// remoteAdditions returns the notes that the merge result must hold, the
// local notes plus the remote additions, and the number of additions. It
// returns gitcmd.ErrNotesConflict when the remote side changed or removed a
// note that local notes hold. git notes merge applies such a change without
// a conflict whenever the local side left the note alone, and a fast-forward
// applies every change, but git-byline never replaces an existing note on
// its own. The same change on both sides is fine. The base is the one git
// notes merge uses: local for a fast-forward, else the first merge base, or
// none for unrelated histories.
func remoteAdditions(repo *gitcmd.Repo, relation notesRelation, local, remote string) (map[string]string, int, error) {
	localNotes, err := noteBlobs(repo, local, "local attribution notes")
	if err != nil {
		return nil, 0, err
	}
	baseNotes := localNotes
	if relation == notesDiverged {
		if baseNotes, err = mergeBaseNotes(repo, local, remote); err != nil {
			return nil, 0, err
		}
	}
	remoteNotes, err := noteBlobs(repo, remote, "fetched remote notes")
	if err != nil {
		return nil, 0, err
	}
	added, changed := classifyRemoteNotes(baseNotes, localNotes, remoteNotes)
	if err := changedNotesError(changed); err != nil {
		return nil, 0, err
	}
	expected := maps.Clone(localNotes)
	maps.Copy(expected, added)
	return expected, len(added), nil
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
func classifyRemoteNotes(base, local, remote map[string]string) (map[string]string, []string) {
	added := map[string]string{}
	var changed []string
	for commit, remoteBlob := range remote {
		baseBlob, localBlob := base[commit], local[commit]
		switch {
		case remoteBlob == baseBlob || remoteBlob == localBlob:
		case baseBlob == "" && localBlob == "":
			added[commit] = remoteBlob
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
