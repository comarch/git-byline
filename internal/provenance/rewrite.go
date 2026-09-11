package provenance

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

const stashNotesRef = "refs/notes/byline-stash"

// RewriteResult reports history-rewrite work and non-fatal skips.
type RewriteResult struct {
	Mapped   int
	Written  int
	Warnings []string
}

// HandlePostRewrite reprojects attribution notes after rebase or amend.
func HandlePostRewrite(repo *gitcmd.Repo, input io.Reader) (RewriteResult, error) {
	mapping, err := rewrite.ParsePostRewrite(input)
	if err != nil {
		return RewriteResult{}, err
	}
	return applyRewriteMapping(repo, mapping)
}

func applyRewriteMapping(repo *gitcmd.Repo, mapping rewrite.Mapping) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("rewrite repository is nil")
	}
	commonLock, err := lock.Acquire(filepath.Join(repo.CommonDir, "byline", "notes.lock"), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer commonLock.Release()
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer held.Release()

	result := RewriteResult{Mapped: len(mapping.Pairs)}
	sources := make(map[string]map[string][]engine.Snapshot)
	sessions := make(map[string]map[string]model.NoteSession)
	blocked := make(map[string]bool)
	for _, pair := range mapping.Pairs {
		if isZero(pair.Old) || isZero(pair.New) {
			continue
		}
		data, found, err := repo.ReadNote(pair.Old)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("read old attribution note %s: %w", pair.Old, err)
		}
		if !found {
			continue
		}
		note, err := notes.Decode(data)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("decode old attribution note %s: %w", pair.Old, err)
		}
		parent, err := repo.Parent(pair.New)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("read rewritten commit parent %s: %w", pair.New, err)
		}
		changes, err := repo.Changes(pair.New, parent)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("read rewritten commit changes %s: %w", pair.New, err)
		}
		if sources[pair.New] == nil {
			sources[pair.New] = make(map[string][]engine.Snapshot)
		}
		if sessions[pair.New] == nil {
			sessions[pair.New] = make(map[string]model.NoteSession)
		}
		for key, value := range note.Sessions {
			sessions[pair.New][key] = value
		}
		paths := make([]string, 0, len(note.Files))
		for path := range note.Files {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			targetPath, found, err := rewrittenPath(repo, pair.New, path, changes)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("map rewritten path %s:%s: %w", pair.Old, path, err)
			}
			if !found {
				continue
			}
			file := note.Files[path]
			content, err := repo.ReadBlob(file.Blob)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("read source blob %s: %w", file.Blob, err)
			}
			source, err := engine.NewSnapshot(content, file.Ranges)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("decode source snapshot %s:%s: %w", pair.Old, path, err)
			}
			sources[pair.New][targetPath] = append(sources[pair.New][targetPath], source)
		}
	}

	newCommits := make([]string, 0, len(sources))
	for commit := range sources {
		newCommits = append(newCommits, commit)
	}
	sort.Strings(newCommits)
	for _, commit := range newCommits {
		note := model.Note{
			Version:  model.NoteVersion,
			Files:    make(map[string]model.NoteFile),
			Sessions: sessions[commit],
		}
		paths := make([]string, 0, len(sources[commit]))
		for path := range sources[commit] {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			blob, exists, err := repo.BlobID(commit, path)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("read rewritten blob %s:%s: %w", commit, path, err)
			}
			if !exists {
				continue
			}
			content, err := repo.ReadBlob(blob)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("read rewritten content %s:%s: %w", commit, path, err)
			}
			projected, err := rewrite.ProjectLayered(
				sources[commit][path],
				content,
				model.Attribution{Author: model.AuthorUntracked},
			)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("project rewritten %s:%s: %w", commit, path, err)
			}
			ranges, err := projected.Ranges()
			if err != nil {
				return RewriteResult{}, fmt.Errorf("range rewritten %s:%s: %w", commit, path, err)
			}
			note.Files[path] = model.NoteFile{Blob: blob, Ranges: ranges}
		}
		if len(note.Files) == 0 {
			continue
		}
		data, err := notes.Encode(note)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("encode rewritten note %s: %w", commit, err)
		}
		existing, found, err := repo.ReadNote(commit)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("read rewritten note %s: %w", commit, err)
		}
		if found && !sameNoteData(existing, data) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped different attribution note on %s", commit))
			blocked[commit] = true
			continue
		}
		if err := repo.WriteNote(commit, data); err != nil {
			return RewriteResult{}, fmt.Errorf("write rewritten note %s: %w", commit, err)
		}
		result.Written++
	}

	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	originalBoundary := state.LastAnnotatedCommit
	state = mapping.RemapState(state)
	blockedBoundary := blocked[state.LastAnnotatedCommit]
	if blockedBoundary {
		state.LastAnnotatedCommit = originalBoundary
		state.Pending.BaseCommit = originalBoundary
	}
	if state.LastAnnotatedCommit != "" && !blockedBoundary {
		remapped, found, err := remapThroughAncestors(repo, state.LastAnnotatedCommit, mapping)
		if err != nil {
			return RewriteResult{}, err
		}
		if found {
			state.LastAnnotatedCommit = remapped
			if state.Pending.BaseCommit != "" {
				state.Pending.BaseCommit = remapped
			}
		}
	}
	head, err := repo.Head()
	if err != nil {
		return RewriteResult{}, err
	}
	if head != "" && state.LastAnnotatedCommit != head {
		parent, parentErr := repo.Parent(head)
		if parentErr != nil {
			return RewriteResult{}, parentErr
		}
		if parent == state.LastAnnotatedCommit {
			if _, found := sources[head]; found {
				state.LastAnnotatedCommit = head
				state.Pending.BaseCommit = head
			}
		}
	}
	if err := dataStore.CheckStateWrite(state); err != nil {
		return RewriteResult{}, fmt.Errorf("validate remapped state: %w", err)
	}
	if err := dataStore.WriteState(state); err != nil {
		return RewriteResult{}, fmt.Errorf("write remapped state: %w", err)
	}
	return result, nil
}

// HandlePostMerge handles merge and cherry-pick commits after Git updates HEAD.
func HandlePostMerge(repo *gitcmd.Repo) (RewriteResult, error) {
	head, err := repo.Head()
	if err != nil {
		return RewriteResult{}, err
	}
	if head == "" {
		return RewriteResult{}, nil
	}
	parents, err := repo.Parents(head)
	if err != nil {
		return RewriteResult{}, err
	}
	if len(parents) == 0 || len(parents) > 1 {
		return RewriteResult{}, nil
	}
	if previous, found, previousErr := repo.PreviousHead(); previousErr != nil {
		return RewriteResult{}, previousErr
	} else if found {
		if mapped, ok, mapErr := amendMapping(repo, previous, head); mapErr != nil {
			return RewriteResult{}, mapErr
		} else if ok {
			return applyRewriteMapping(repo, mapped)
		}
	}
	message, err := repo.CommitMessage(head)
	if err != nil {
		return RewriteResult{}, err
	}
	if !containsCherryPickMessage(message) {
		source, found, findErr := findCherryPickSource(repo, head)
		if findErr != nil {
			return RewriteResult{}, findErr
		}
		if found {
			pairs, mapErr := rewrite.NewMapping([]rewrite.Pair{{Old: source, New: head}})
			if mapErr != nil {
				return RewriteResult{}, mapErr
			}
			return applyRewriteMapping(repo, pairs)
		}
		return RewriteResult{}, nil
	}
	source, err := cherryPickedCommit(message)
	if err != nil {
		return RewriteResult{}, err
	}
	pairs, err := rewrite.NewMapping([]rewrite.Pair{{Old: source, New: head}})
	if err != nil {
		return RewriteResult{}, err
	}
	return applyRewriteMapping(repo, pairs)
}

func amendMapping(repo *gitcmd.Repo, previous, head string) (rewrite.Mapping, bool, error) {
	previousParent, err := repo.Parent(previous)
	if err != nil {
		return rewrite.Mapping{}, false, err
	}
	headParent, err := repo.Parent(head)
	if err != nil {
		return rewrite.Mapping{}, false, err
	}
	if previousParent == "" || previousParent != headParent {
		return rewrite.Mapping{}, false, nil
	}
	if _, found, err := repo.ReadNote(previous); err != nil {
		return rewrite.Mapping{}, false, err
	} else if !found {
		return rewrite.Mapping{}, false, nil
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{Old: previous, New: head}})
	return mapping, true, err
}

func findCherryPickSource(repo *gitcmd.Repo, head string) (string, bool, error) {
	currentPatch, err := repo.PatchID(head)
	if err != nil {
		return "", false, err
	}
	candidates, err := repo.NoteCommits("refs/notes/byline")
	if err != nil {
		return "", false, err
	}
	for _, candidate := range candidates {
		if candidate == head {
			continue
		}
		patch, patchErr := repo.PatchID(candidate)
		if patchErr != nil {
			continue
		}
		if patch == currentPatch {
			return candidate, true, nil
		}
	}
	return "", false, nil
}

func containsCherryPickMessage(message string) bool {
	return len(cherryPickMarkers(message)) > 0
}

func cherryPickMarkers(message string) []string {
	const marker = "(cherry picked from commit "
	var result []string
	for {
		start := strings.Index(message, marker)
		if start < 0 {
			return result
		}
		message = message[start+len(marker):]
		end := strings.IndexByte(message, ')')
		if end < 0 {
			return result
		}
		value := message[:end]
		if model.ValidObjectID(value) {
			result = append(result, value)
		}
		message = message[end+1:]
	}
}

func cherryPickedCommit(message string) (string, error) {
	values := cherryPickMarkers(message)
	if len(values) == 0 {
		return "", errors.New("cherry-pick message has no valid source commit")
	}
	return values[len(values)-1], nil
}

func rewrittenPath(repo *gitcmd.Repo, commit, oldPath string, changes []gitcmd.Change) (string, bool, error) {
	if _, exists, err := repo.BlobID(commit, oldPath); err != nil {
		return "", false, err
	} else if exists {
		return oldPath, true, nil
	}
	for _, change := range changes {
		if change.OldPath == oldPath && change.Status != 'D' {
			return change.Path, true, nil
		}
	}
	return "", false, nil
}

func remapThroughAncestors(repo *gitcmd.Repo, commit string, mapping rewrite.Mapping) (string, bool, error) {
	seen := map[string]bool{}
	for commit != "" && !seen[commit] {
		seen[commit] = true
		if target, ok := mapping.Remap(commit); ok {
			return target, true, nil
		}
		parent, err := repo.Parent(commit)
		if err != nil {
			return "", false, fmt.Errorf("walk rewritten state boundary: %w", err)
		}
		commit = parent
	}
	return "", false, nil
}

func sameNoteData(left, right []byte) bool {
	return bytes.Equal(left, right) ||
		bytes.Equal(bytes.TrimSuffix(left, []byte{'\n'}), bytes.TrimSuffix(right, []byte{'\n'}))
}

// HandlePostCheckout reprojects pending worktree attribution after a switch.
func HandlePostCheckout(repo *gitcmd.Repo, oldCommit, newCommit string) (RewriteResult, error) {
	return rebasePending(repo, newCommit)
}

func rebasePending(repo *gitcmd.Repo, newCommit string) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("pending rebase repository is nil")
	}
	if newCommit == "" {
		var err error
		newCommit, err = repo.Head()
		if err != nil {
			return RewriteResult{}, err
		}
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer held.Release()
	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	if newCommit != "" {
		if data, found, readErr := repo.ReadNote(newCommit); readErr != nil {
			return RewriteResult{}, readErr
		} else if found {
			if _, decodeErr := notes.Decode(data); decodeErr != nil {
				return RewriteResult{}, fmt.Errorf("decode checkout note: %w", decodeErr)
			}
			state.LastAnnotatedCommit = newCommit
			state.Pending.BaseCommit = newCommit
		} else {
			state.LastAnnotatedCommit = ""
			state.Pending.BaseCommit = ""
		}
	}
	result := RewriteResult{}
	next := make(map[string]model.PendingFile, len(state.Pending.Files))
	paths := make([]string, 0, len(state.Pending.Files))
	for path := range state.Pending.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		pending := state.Pending.Files[path]
		content, exists, normalized, err := repo.WorktreeFile(path)
		if err != nil || !exists {
			continue
		}
		sourceContent, err := repo.ReadBlob(pending.Blob)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("read pending snapshot %s: %w", path, err)
		}
		source, err := engine.NewSnapshot(sourceContent, pending.Ranges)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("decode pending snapshot %s: %w", path, err)
		}
		projected, err := engine.Project(source, content, model.Attribution{Author: model.AuthorUntracked})
		if err != nil {
			return RewriteResult{}, fmt.Errorf("project pending %s: %w", path, err)
		}
		ranges, err := projected.Ranges()
		if err != nil {
			return RewriteResult{}, err
		}
		blob, err := repo.HashBytes(content)
		if err != nil {
			return RewriteResult{}, err
		}
		next[normalized] = model.PendingFile{Blob: blob, Ranges: ranges}
		result.Mapped++
	}
	state.Pending.Files = next
	if err := dataStore.CheckStateWrite(state); err != nil {
		return RewriteResult{}, fmt.Errorf("validate checkout state: %w", err)
	}
	if err := dataStore.WriteState(state); err != nil {
		return RewriteResult{}, fmt.Errorf("write checkout state: %w", err)
	}
	return result, nil
}

// HandleReferenceTransaction applies reset, branch, and stash ref updates.
func HandleReferenceTransaction(repo *gitcmd.Repo, input io.Reader, phase string) (RewriteResult, error) {
	updates, err := rewrite.ParseReferenceTransaction(input)
	if err != nil {
		return RewriteResult{}, err
	}
	if phase != "committed" {
		return RewriteResult{}, nil
	}
	var result RewriteResult
	for _, update := range updates {
		switch {
		case update.Ref == "HEAD":
			value, err := handleHeadMove(repo, update)
			if err != nil {
				return RewriteResult{}, err
			}
			result.Mapped += value.Mapped
			result.Written += value.Written
			result.Warnings = append(result.Warnings, value.Warnings...)
		case update.Ref == "refs/stash":
			value, err := handleStashMove(repo, update)
			if err != nil {
				return RewriteResult{}, err
			}
			result.Mapped += value.Mapped
			result.Written += value.Written
			result.Warnings = append(result.Warnings, value.Warnings...)
		}
	}
	return result, nil
}

func handleHeadMove(repo *gitcmd.Repo, update rewrite.RefUpdate) (RewriteResult, error) {
	if update.Old == update.New {
		return RewriteResult{}, nil
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	state, err := dataStore.ReadState()
	if err != nil {
		held.Release()
		return RewriteResult{}, err
	}
	if state.LastAnnotatedCommit != update.Old && len(state.Pending.Files) == 0 {
		held.Release()
		return RewriteResult{}, nil
	}
	dirty, err := repo.DirtyPaths()
	if err != nil {
		held.Release()
		return RewriteResult{}, err
	}
	held.Release()
	if len(dirty) > 0 && update.New != "" && !isZero(update.New) {
		if data, found, readErr := repo.ReadNote(update.Old); readErr != nil {
			return RewriteResult{}, readErr
		} else if found {
			note, decodeErr := notes.Decode(data)
			if decodeErr != nil {
				return RewriteResult{}, fmt.Errorf("decode reset source note: %w", decodeErr)
			}
			pending, projectErr := pendingFromNote(repo, note)
			if projectErr != nil {
				return RewriteResult{}, projectErr
			}
			state.LastAnnotatedCommit = annotationBoundary(repo, update.New)
			state.Pending = model.PendingState{BaseCommit: state.LastAnnotatedCommit, Files: pending}
			held, err = lock.Acquire(dataStore.LockPath(), lockTimeout)
			if err != nil {
				return RewriteResult{}, err
			}
			defer held.Release()
			if err := dataStore.CheckStateWrite(state); err != nil {
				return RewriteResult{}, err
			}
			if err := dataStore.WriteState(state); err != nil {
				return RewriteResult{}, err
			}
			return RewriteResult{Mapped: len(pending)}, nil
		}
		return rebasePending(repo, update.New)
	}
	if update.New == "" || isZero(update.New) {
		state.LastAnnotatedCommit = ""
		state.Pending = model.PendingState{Files: map[string]model.PendingFile{}}
	} else if _, found, readErr := repo.ReadNote(update.New); readErr != nil {
		return RewriteResult{}, readErr
	} else if found {
		state.LastAnnotatedCommit = update.New
		state.Pending = model.PendingState{
			BaseCommit: update.New,
			Files:      map[string]model.PendingFile{},
		}
	} else {
		state.LastAnnotatedCommit = ""
		state.Pending.BaseCommit = ""
		state.Pending.Files = map[string]model.PendingFile{}
	}
	held, err = lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer held.Release()
	if err := dataStore.CheckStateWrite(state); err != nil {
		return RewriteResult{}, err
	}
	if err := dataStore.WriteState(state); err != nil {
		return RewriteResult{}, err
	}
	return RewriteResult{Mapped: 1}, nil
}

func pendingFromNote(repo *gitcmd.Repo, note model.Note) (map[string]model.PendingFile, error) {
	pending := make(map[string]model.PendingFile, len(note.Files))
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := note.Files[path]
		content, exists, normalized, err := repo.WorktreeFile(path)
		if err != nil {
			continue
		}
		if !exists {
			continue
		}
		ranges, err := projectNoteFile(repo, file, content)
		if err != nil {
			return nil, fmt.Errorf("project note %s: %w", path, err)
		}
		blob, err := repo.HashBytes(content)
		if err != nil {
			return nil, err
		}
		pending[normalized] = model.PendingFile{Blob: blob, Ranges: ranges}
	}
	return pending, nil
}

func annotationBoundary(repo *gitcmd.Repo, commit string) string {
	if commit == "" || isZero(commit) {
		return ""
	}
	data, found, err := repo.ReadNote(commit)
	if err == nil && found {
		if _, decodeErr := notes.Decode(data); decodeErr == nil {
			return commit
		}
	}
	return ""
}

func handleStashMove(repo *gitcmd.Repo, update rewrite.RefUpdate) (RewriteResult, error) {
	if update.New == "" || isZero(update.New) {
		if update.Old == "" || isZero(update.Old) {
			return RewriteResult{}, nil
		}
		paths, err := repo.DirtyPaths()
		if err != nil {
			return RewriteResult{}, err
		}
		if len(paths) == 0 {
			return RewriteResult{}, repo.DeleteNoteRef(stashNotesRef, update.Old)
		}
		return HandleStashApply(repo, update.Old, false)
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer held.Release()
	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	if len(state.Pending.Files) == 0 {
		return RewriteResult{}, nil
	}
	files := make(map[string]model.NoteFile, len(state.Pending.Files))
	for path, pending := range state.Pending.Files {
		files[path] = model.NoteFile{Blob: pending.Blob, Ranges: pending.Ranges}
	}
	data, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files:   files,
	})
	if err != nil {
		return RewriteResult{}, fmt.Errorf("encode stash attribution: %w", err)
	}
	if err := repo.WriteNoteRef(stashNotesRef, update.New, data); err != nil {
		return RewriteResult{}, fmt.Errorf("write stash attribution: %w", err)
	}
	return RewriteResult{Written: 1}, nil
}

// HandleStashApply restores stash attribution to pending state.
func HandleStashApply(repo *gitcmd.Repo, stashCommit string, keep bool) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("stash repository is nil")
	}
	if !model.ValidObjectID(stashCommit) {
		return RewriteResult{}, errors.New("invalid stash commit")
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer held.Release()
	data, found, err := repo.ReadNoteRef(stashNotesRef, stashCommit)
	if err != nil {
		return RewriteResult{}, err
	}
	if !found {
		return RewriteResult{}, nil
	}
	note, err := notes.Decode(data)
	if err != nil {
		return RewriteResult{}, fmt.Errorf("decode stash attribution: %w", err)
	}
	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return RewriteResult{}, err
	}
	pending := make(map[string]model.PendingFile, len(note.Files))
	for path, file := range note.Files {
		content, exists, normalized, readErr := repo.WorktreeFile(path)
		if readErr != nil || !exists {
			continue
		}
		projected, projectErr := projectNoteFile(repo, file, content)
		if projectErr != nil {
			return RewriteResult{}, fmt.Errorf("project stash %s: %w", path, projectErr)
		}
		blob, hashErr := repo.HashBytes(content)
		if hashErr != nil {
			return RewriteResult{}, hashErr
		}
		pending[normalized] = model.PendingFile{Blob: blob, Ranges: projected}
	}
	boundary := annotationBoundary(repo, head)
	state.LastAnnotatedCommit = boundary
	state.Pending = model.PendingState{BaseCommit: boundary, Files: pending}
	if err := dataStore.CheckStateWrite(state); err != nil {
		return RewriteResult{}, err
	}
	if err := dataStore.WriteState(state); err != nil {
		return RewriteResult{}, err
	}
	if !keep {
		if err := removeStashNote(repo, stashCommit); err != nil {
			return RewriteResult{}, err
		}
	}
	return RewriteResult{Mapped: len(pending)}, nil
}

func projectNoteFile(repo *gitcmd.Repo, file model.NoteFile, content []byte) ([]model.Range, error) {
	sourceContent, err := repo.ReadBlob(file.Blob)
	if err != nil {
		return nil, err
	}
	source, err := engine.NewSnapshot(sourceContent, file.Ranges)
	if err != nil {
		return nil, err
	}
	projected, err := engine.Project(source, content, model.Attribution{Author: model.AuthorUntracked})
	if err != nil {
		return nil, err
	}
	return projected.Ranges()
}

func removeStashNote(repo *gitcmd.Repo, commit string) error {
	err := repo.DeleteNoteRef(stashNotesRef, commit)
	if err != nil {
		return fmt.Errorf("remove stash attribution: %w", err)
	}
	return nil
}

func isZero(value string) bool {
	if value == "" {
		return true
	}
	for _, char := range value {
		if char != '0' {
			return false
		}
	}
	return true
}

// StashNoteRef exposes the stash note namespace for inspection and tests.
func StashNoteRef() string {
	return stashNotesRef
}
