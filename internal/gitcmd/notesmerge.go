package gitcmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/comarch/git-byline/internal/model"
)

// RemoteNotesRef holds the attribution notes that the managed pre-push hook
// fetched from the push remote until merge-notes merges them. It lives under
// refs/worktree/ so concurrent pushes from linked worktrees never share it.
const RemoteNotesRef = "refs/worktree/byline/remote-notes"

// ErrNotesConflict reports local and remote notes that differ for the same
// commit. The local notes ref stays unchanged.
var ErrNotesConflict = errors.New("local and remote attribution notes differ")

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

// FindMergeBase returns the best common ancestor of two commits. found is
// false when the histories are unrelated.
func (repo *Repo) FindMergeBase(first, second string) (string, bool, error) {
	if !model.ValidObjectID(first) || !model.ValidObjectID(second) {
		return "", false, errors.New("invalid merge base object ID")
	}
	base, err := repo.MergeBase(first, second)
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return base, true, nil
}

// ObjectType returns the type of the object that oid names, for example
// commit or tag.
func (repo *Repo) ObjectType(oid string) (string, error) {
	if !model.ValidObjectID(oid) {
		return "", errors.New("invalid object ID")
	}
	out, err := repo.run("read object type", nil, "cat-file", "-t", oid)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// NoteBlobs maps every object that the notes tree of notesCommit annotates
// to its note blob. Fanout directories are joined back into the object ID.
// Any entry that is not a note blob is an error, so a notes history cannot
// carry other files along.
func (repo *Repo) NoteBlobs(notesCommit string) (map[string]string, error) {
	if !model.ValidObjectID(notesCommit) {
		return nil, errors.New("invalid notes commit object ID")
	}
	out, err := repo.run("list notes tree", nil, "ls-tree", "-r", "-z", "--full-tree", notesCommit)
	if err != nil {
		return nil, err
	}
	notes := map[string]string{}
	for _, record := range strings.Split(string(out), "\x00") {
		if record == "" {
			continue
		}
		meta, path, found := strings.Cut(record, "\t")
		fields := strings.Split(meta, " ")
		if !found || len(fields) != 3 {
			return nil, errors.New("git returned an invalid notes tree entry")
		}
		object, isNote := noteObject(path, len(notesCommit))
		if !isNote || fields[0] != "100644" || fields[1] != "blob" || !model.ValidObjectID(fields[2]) {
			return nil, fmt.Errorf("notes tree entry %q is not a note", path)
		}
		if _, duplicate := notes[object]; duplicate {
			return nil, fmt.Errorf("notes tree has two notes for object %s", object)
		}
		notes[object] = fields[2]
	}
	return notes, nil
}

// noteObject returns the object ID that a notes tree path names. Fanout
// directories have two hex digits each, like git notes writes them.
func noteObject(path string, length int) (string, bool) {
	parts := strings.Split(path, "/")
	for _, part := range parts[:len(parts)-1] {
		if len(part) != 2 {
			return "", false
		}
	}
	object := strings.Join(parts, "")
	if len(object) != length || strings.Trim(object, "0123456789abcdef") != "" {
		return "", false
	}
	return object, true
}

// NotesMergeInProgress reports an unfinished git notes merge in this
// worktree. It may hold hand-resolved notes, so an automatic merge must
// neither join nor abort it.
func (repo *Repo) NotesMergeInProgress() (bool, error) {
	// A branch or tag named NOTES_MERGE_PARTIAL also satisfies a plain
	// rev-parse, so only the exact pseudoref name counts.
	out, err := repo.run("read notes merge state", nil,
		"rev-parse", "--verify", "--quiet", "--symbolic-full-name", "NOTES_MERGE_PARTIAL")
	var commandErr *CommandError
	if err != nil && (!errors.As(err, &commandErr) || commandErr.ExitCode != 1) {
		return false, err
	}
	if err == nil && strings.TrimSpace(string(out)) == "NOTES_MERGE_PARTIAL" {
		return true, nil
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

// MergeNotes merges the notes commit remote into the attribution notes ref.
// The explicit manual strategy keeps a configured notes.mergeStrategy from
// picking a side on its own. git notes merge exits 1 only for conflicts;
// they are aborted and return ErrNotesConflict. The caller must check
// NotesMergeInProgress first, so merge state that any other failure leaves
// behind belongs to this call and is aborted too.
func (repo *Repo) MergeNotes(remote string) error {
	if !model.ValidObjectID(remote) {
		return errors.New("invalid remote notes object ID")
	}
	_, err := repo.run("merge attribution notes", nil,
		"notes", "--ref="+bylineNotesRef, "merge", "--strategy=manual", "--quiet", remote)
	if err == nil {
		return nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		if abortErr := repo.abortNotesMerge(); abortErr != nil {
			return fmt.Errorf("abort conflicting notes merge: %w", abortErr)
		}
		return ErrNotesConflict
	}
	err = noteWriteError(err)
	inProgress, stateErr := repo.NotesMergeInProgress()
	if stateErr != nil {
		return errors.Join(err, stateErr)
	}
	if inProgress {
		if abortErr := repo.abortNotesMerge(); abortErr != nil {
			return errors.Join(err, fmt.Errorf("abort failed notes merge: %w", abortErr))
		}
	}
	return err
}

func (repo *Repo) abortNotesMerge() error {
	_, err := repo.run("abort attribution notes merge", nil,
		"notes", "--ref="+bylineNotesRef, "merge", "--abort")
	return err
}
