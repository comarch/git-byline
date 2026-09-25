package gitcmd

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/comarch/git-byline/internal/model"
)

// RemoteNotesRef holds the attribution notes that the managed pre-push hook
// fetched from the push remote until merge-notes merges them. It lives under
// refs/worktree/ so concurrent pushes from linked worktrees never share it.
const RemoteNotesRef = "refs/worktree/byline/remote-notes"

const notesRefArg = "--ref=" + bylineNotesRef

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
// Any entry that is not a note blob is an error, so the tree cannot carry
// other files along. It checks this one tree only; CheckNotesHistory checks
// the commits behind it.
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

// fanoutDirectory reports whether path names a notes fanout directory:
// levels of two hex digits that still leave room for a note name.
func fanoutDirectory(path string, length int) bool {
	parts := strings.Split(path, "/")
	if 2*len(parts) >= length {
		return false
	}
	for _, part := range parts {
		if len(part) != 2 || strings.Trim(part, "0123456789abcdef") != "" {
			return false
		}
	}
	return true
}

// CheckNotesHistory checks every object that the notes commit tip brings
// along beyond the notes commit exclude, or its whole history when exclude
// is empty. Only commits, root and fanout trees, and note blobs pass, each
// up to the note size limit. So fetched notes cannot carry other files or
// large blobs in older commits into refs/notes/byline and on to the next
// push. A missing object fails instead of being fetched lazily.
func (repo *Repo) CheckNotesHistory(tip, exclude string) error {
	if !model.ValidObjectID(tip) || (exclude != "" && !model.ValidObjectID(exclude)) {
		return errors.New("invalid notes history object ID")
	}
	args := []string{"rev-list", "--objects", "--missing=print", tip}
	if exclude != "" {
		args = append(args, "--not", exclude)
	}
	out, err := repo.run("list notes history objects", nil, args...)
	if err != nil {
		return err
	}
	ids, paths, err := notesHistoryObjects(out)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	out, err = repo.run("read notes history objects", strings.NewReader(strings.Join(ids, "\n")+"\n"),
		"cat-file", "--batch-check=%(objectname) %(objecttype) %(objectsize)")
	if err != nil {
		return err
	}
	records := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(records) != len(ids) {
		return errors.New("git returned an incomplete notes history object list")
	}
	for index, record := range records {
		if err := checkNotesHistoryObject(record, ids[index], paths[index], len(tip)); err != nil {
			return err
		}
	}
	return nil
}

// notesHistoryObjects splits rev-list --objects --missing=print output into
// object IDs and their paths. rev-list cuts a name at its first newline, so
// every line holds one object and a name with a newline cannot add a line
// of its own.
func notesHistoryObjects(out []byte) ([]string, []string, error) {
	var ids, paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		if missing, found := strings.CutPrefix(line, "?"); found {
			return nil, nil, fmt.Errorf("notes history object %s is missing", missing)
		}
		id, path, _ := strings.Cut(line, " ")
		if !model.ValidObjectID(id) {
			return nil, nil, errors.New("git returned an invalid notes history object ID")
		}
		ids = append(ids, id)
		paths = append(paths, path)
	}
	return ids, paths, nil
}

func checkNotesHistoryObject(record, id, path string, length int) error {
	fields := strings.Split(record, " ")
	if len(fields) != 3 || fields[0] != id {
		return fmt.Errorf("notes history object %s is missing", id)
	}
	kind := fields[1]
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return fmt.Errorf("git returned an invalid size for notes history object %s", id)
	}
	if size > maxNoteBytes {
		return fmt.Errorf("notes history %s %s has %d bytes, more than the %d byte note limit", kind, id, size, maxNoteBytes)
	}
	switch kind {
	case "commit":
		return nil
	case "tree":
		if path == "" || fanoutDirectory(path, length) {
			return nil
		}
	case "blob":
		if _, isNote := noteObject(path, length); isNote {
			return nil
		}
	}
	return fmt.Errorf("notes history %s %s at %q is not part of a notes tree", kind, id, path)
}

// ListNotes maps every object that refs/notes/byline annotates to its note
// blob as git notes list reads the tree, the way every Git command sees it.
func (repo *Repo) ListNotes() (map[string]string, error) {
	out, err := repo.run("list attribution notes", nil, "notes", notesRefArg, "list")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields)%2 != 0 {
		return nil, errors.New("git returned an invalid notes list")
	}
	notes := make(map[string]string, len(fields)/2)
	for index := 0; index < len(fields); index += 2 {
		blob, object := fields[index], fields[index+1]
		if !model.ValidObjectID(blob) || !model.ValidObjectID(object) {
			return nil, errors.New("git returned an invalid notes list entry")
		}
		if _, duplicate := notes[object]; duplicate {
			return nil, fmt.Errorf("git listed two notes for object %s", object)
		}
		notes[object] = blob
	}
	return notes, nil
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
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read notes merge worktree: %w", err)
	}
	// Go 1.24 os.ReadDir on Windows does not reject a regular file.
	if !info.IsDir() {
		return false, fmt.Errorf("notes merge worktree %s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
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
		"notes", notesRefArg, "merge", "--strategy=manual", "--quiet", remote)
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
		"notes", notesRefArg, "merge", "--abort")
	return err
}
