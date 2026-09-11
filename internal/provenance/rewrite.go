package provenance

import (
	"bytes"
	"crypto/sha256"
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
const stashOwnershipRef = "refs/notes/byline-stash-owner"

const (
	maxRewriteBlobBytes    = 16 << 20
	maxRewriteMatcherCells = 4_000_000
)

var errRewriteBudget = errors.New("rewrite resource budget exceeded")

type rewriteBlobCache struct {
	content map[string][]byte
	sizes   map[string]int64
	used    int64
}

func newRewriteBlobCache() *rewriteBlobCache {
	return &rewriteBlobCache{
		content: make(map[string][]byte),
		sizes:   make(map[string]int64),
	}
}

func (cache *rewriteBlobCache) Read(repo *gitcmd.Repo, oid string) ([]byte, error) {
	if content, ok := cache.content[oid]; ok {
		return content, nil
	}
	size, ok := cache.sizes[oid]
	if !ok {
		var err error
		size, err = repo.BlobSize(oid)
		if err != nil {
			return nil, err
		}
		cache.sizes[oid] = size
	}
	if size > maxRewriteBlobBytes-cache.used {
		return nil, errRewriteBudget
	}
	content, err := repo.ReadBlob(oid)
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > maxRewriteBlobBytes-cache.used {
		return nil, errRewriteBudget
	}
	cache.used += int64(len(content))
	cache.content[oid] = content
	return content, nil
}

// RewriteResult reports history-rewrite work and non-fatal skips.
type RewriteResult struct {
	Mapped   int
	Written  int
	Warnings []string
}

// HandlePostRewrite reprojects attribution notes after rebase or amend.
func HandlePostRewrite(repo *gitcmd.Repo, input io.Reader) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("rewrite repository is nil")
	}
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return RewriteResult{}, err
	}
	mapping, err := rewrite.ParsePostRewriteForLength(input, objectIDLength)
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

	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	result := RewriteResult{Mapped: len(mapping.Pairs)}
	sources := make(map[string]map[string][]engine.Snapshot)
	sessions := make(map[string]map[string]model.NoteSession)
	blocked := make(map[string]bool)
	completed := make(map[string]bool)
	incomplete := make(map[string]bool)
	noteCache := make(map[string]struct {
		note  model.Note
		found bool
	})
	noteBudgetExceeded := make(map[string]bool)
	var noteBytes int64
	blobCache := newRewriteBlobCache()
	readBlob := func(oid string) ([]byte, error) {
		return blobCache.Read(repo, oid)
	}
	for _, pair := range mapping.Pairs {
		if isZero(pair.New) {
			continue
		}
		if noteBudgetExceeded[pair.Old] {
			incomplete[pair.New] = true
			continue
		}
		cached, cachedOK := noteCache[pair.Old]
		if !cachedOK {
			data, found, readErr := repo.ReadNote(pair.Old)
			if readErr != nil {
				return RewriteResult{}, fmt.Errorf("read old attribution note %s: %w", pair.Old, readErr)
			}
			cached.found = found
			if found {
				if int64(len(data)) > maxRewriteBlobBytes-noteBytes {
					noteBudgetExceeded[pair.Old] = true
					incomplete[pair.New] = true
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped rewrite mapping from %s: note budget exceeded", pair.Old))
					continue
				}
				noteBytes += int64(len(data))
				cached.note, err = notes.Decode(data)
				if err != nil {
					return RewriteResult{}, fmt.Errorf("decode old attribution note %s: %w", pair.Old, err)
				}
			}
			noteCache[pair.Old] = cached
		}
		if !cached.found {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("skipped unrelated rewrite mapping from %s", pair.Old))
			continue
		}
		if _, commitErr := repo.Parents(pair.Old); commitErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("skipped rewrite mapping from non-commit %s", pair.Old))
			continue
		}
		note := cached.note
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
			if err := addRewriteSession(sessions[pair.New], key, value); err != nil {
				return RewriteResult{}, fmt.Errorf("aggregate rewritten session %s: %w", key, err)
			}
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
			content, err := readBlob(file.Blob)
			if err != nil {
				if errors.Is(err, errRewriteBudget) {
					incomplete[pair.New] = true
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped %s:%s: %v", pair.Old, path, err))
					continue
				}
				return RewriteResult{}, fmt.Errorf("read source blob %s: %w", file.Blob, err)
			}
			source, err := engine.NewSnapshot(content, file.Ranges)
			if err != nil {
				return RewriteResult{}, fmt.Errorf("decode source snapshot %s:%s: %w", pair.Old, path, err)
			}
			sources[pair.New][targetPath] = append(sources[pair.New][targetPath], source)
		}
	}

	matcherBudget := engine.NewMatcherBudget(maxRewriteMatcherCells)
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
			content, err := readBlob(blob)
			if err != nil {
				if errors.Is(err, errRewriteBudget) {
					incomplete[commit] = true
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped rewritten note %s:%s: %v", commit, path, err))
					continue
				}
				return RewriteResult{}, fmt.Errorf("read rewritten content %s:%s: %w", commit, path, err)
			}
			projected, err := engine.ProjectLayeredWithBudget(
				sources[commit][path],
				content,
				model.Attribution{Author: model.AuthorUntracked},
				matcherBudget,
			)
			if err != nil {
				if errors.Is(err, engine.ErrMatcherBudget) {
					incomplete[commit] = true
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped rewritten note %s:%s: %v", commit, path, err))
					continue
				}
				return RewriteResult{}, fmt.Errorf("project rewritten %s:%s: %w", commit, path, err)
			}
			ranges, err := projected.Ranges()
			if err != nil {
				return RewriteResult{}, fmt.Errorf("range rewritten %s:%s: %w", commit, path, err)
			}
			note.Files[path] = model.NoteFile{Blob: blob, Ranges: ranges}
		}
		if incomplete[commit] {
			continue
		}
		if len(note.Files) == 0 {
			completed[commit] = true
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
		if found {
			if int64(len(existing)) > maxRewriteBlobBytes-noteBytes {
				incomplete[commit] = true
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped rewritten note %s: note budget exceeded", commit))
				continue
			}
			noteBytes += int64(len(existing))
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
		completed[commit] = true
	}

	originalBoundary := state.LastAnnotatedCommit
	originalPendingBase := state.Pending.BaseCommit
	blockedBoundary := false
	boundaryRemapped := false
	if target, ok := mapping.Remap(originalBoundary); ok {
		blockedBoundary = blocked[target]
		switch {
		case isZero(target):
			parent, parentErr := repo.Parent(originalBoundary)
			if parentErr != nil {
				return RewriteResult{}, parentErr
			}
			state.LastAnnotatedCommit = parent
			state.Pending.BaseCommit = parent
		case completed[target] && !blocked[target]:
			state.LastAnnotatedCommit = target
			state.Pending.BaseCommit = target
			boundaryRemapped = true
		}
	}
	if state.LastAnnotatedCommit != "" && !blockedBoundary && !boundaryRemapped {
		remapped, found, err := remapThroughAncestors(repo, state.LastAnnotatedCommit, mapping, completed, blocked)
		if err != nil {
			return RewriteResult{}, err
		}
		if found {
			state.LastAnnotatedCommit = remapped
			state.Pending.BaseCommit = remapped
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
			if completed[head] && !blocked[head] {
				state.LastAnnotatedCommit = head
				state.Pending.BaseCommit = head
			}
		}
	}
	if originalPendingBase != "" && originalPendingBase != originalBoundary {
		if target, ok := mapping.Remap(originalPendingBase); ok {
			switch {
			case isZero(target):
				parent, parentErr := repo.Parent(originalPendingBase)
				if parentErr != nil {
					return RewriteResult{}, parentErr
				}
				state.Pending.BaseCommit = parent
			case completed[target] && !blocked[target]:
				state.Pending.BaseCommit = target
			}
		}
	}
	if err := writeRewriteState(repo, dataStore, state); err != nil {
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
	if !strings.Contains(message, "(cherry picked from commit ") {
		return RewriteResult{}, nil
	}
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return RewriteResult{}, err
	}
	source, err := cherryPickedCommit(message, objectIDLength)
	if err != nil {
		return RewriteResult{Warnings: []string{err.Error()}}, nil
	}
	valid, warning, err := validCherryPickSource(repo, source, head)
	if err != nil {
		return RewriteResult{Warnings: []string{
			fmt.Sprintf("skipped cherry-pick marker: %v", err),
		}}, nil
	}
	if !valid {
		return RewriteResult{Warnings: []string{warning}}, nil
	}
	pairs, err := rewrite.NewMappingForLength(
		[]rewrite.Pair{{Old: source, New: head}},
		objectIDLength,
	)
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
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return rewrite.Mapping{}, false, err
	}
	mapping, err := rewrite.NewMappingForLength(
		[]rewrite.Pair{{Old: previous, New: head}},
		objectIDLength,
	)
	return mapping, true, err
}

func cherryPickMarkers(message string, objectIDLength int) []string {
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
		if rewrite.ValidateFullObjectID(value, objectIDLength, false) == nil {
			result = append(result, value)
		}
		message = message[end+1:]
	}
}

func cherryPickedCommit(message string, objectIDLength int) (string, error) {
	values := cherryPickMarkers(message, objectIDLength)
	if len(values) == 0 {
		return "", errors.New("cherry-pick marker has no valid full source commit")
	}
	return values[len(values)-1], nil
}

func validCherryPickSource(repo *gitcmd.Repo, source, head string) (bool, string, error) {
	if source == head {
		return false, "skipped cherry-pick marker that points to HEAD", nil
	}
	data, found, err := repo.ReadNote(source)
	if err != nil {
		return false, "", err
	}
	if !found {
		return false, fmt.Sprintf("skipped cherry-pick marker without source note on %s", source), nil
	}
	if _, err := notes.Decode(data); err != nil {
		return false, fmt.Sprintf("skipped cherry-pick marker with invalid source note on %s", source), nil
	}
	sourceParent, err := repo.Parent(source)
	if err != nil {
		return false, "", err
	}
	currentParent, err := repo.Parent(head)
	if err != nil {
		return false, "", err
	}
	currentPatch, err := repo.PatchID(head)
	if err != nil || currentPatch == "" {
		return false, "skipped cherry-pick marker without target patch", nil
	}
	if sourceParent == currentParent {
		return true, "", nil
	}
	sourcePatch, err := repo.PatchID(source)
	if err != nil || sourcePatch == "" {
		return false, fmt.Sprintf("skipped cherry-pick marker without source patch on %s", source), nil
	}
	if sourcePatch != currentPatch {
		return false, fmt.Sprintf("skipped cherry-pick marker with unrelated source %s", source), nil
	}
	return true, "", nil
}

func rewrittenPath(repo *gitcmd.Repo, commit, oldPath string, changes []gitcmd.Change) (string, bool, error) {
	for _, change := range changes {
		if change.OldPath == oldPath && change.Status != 'D' {
			return change.Path, true, nil
		}
	}
	if _, exists, err := repo.BlobID(commit, oldPath); err != nil {
		return "", false, err
	} else if exists {
		return oldPath, true, nil
	}
	return "", false, nil
}

func remapThroughAncestors(
	repo *gitcmd.Repo,
	commit string,
	mapping rewrite.Mapping,
	completed map[string]bool,
	blocked map[string]bool,
) (string, bool, error) {
	seen := map[string]bool{}
	for commit != "" && !seen[commit] {
		seen[commit] = true
		if target, ok := mapping.Remap(commit); ok && !isZero(target) &&
			completed[target] && !blocked[target] {
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

func addRewriteSession(
	sessions map[string]model.NoteSession,
	key string,
	value model.NoteSession,
) error {
	current, found := sessions[key]
	if !found {
		sessions[key] = value
		return nil
	}
	if current.Agent != value.Agent || current.Model != value.Model {
		return fmt.Errorf("session %q has conflicting metadata", key)
	}
	if current.FirstTS == "" || (value.FirstTS != "" && timestampBefore(value.FirstTS, current.FirstTS)) {
		current.FirstTS = value.FirstTS
	}
	if current.LastTS == "" || (value.LastTS != "" && timestampBefore(current.LastTS, value.LastTS)) {
		current.LastTS = value.LastTS
	}
	var ok bool
	if current.Added, ok = addRewriteMetric(current.Added, value.Added); !ok {
		return fmt.Errorf("session %q added counter overflows", key)
	}
	if current.Deleted, ok = addRewriteMetric(current.Deleted, value.Deleted); !ok {
		return fmt.Errorf("session %q deleted counter overflows", key)
	}
	if current.Accepted, ok = addRewriteMetric(current.Accepted, value.Accepted); !ok {
		return fmt.Errorf("session %q accepted counter overflows", key)
	}
	if current.Overridden, ok = addRewriteMetric(current.Overridden, value.Overridden); !ok {
		return fmt.Errorf("session %q overridden counter overflows", key)
	}
	sessions[key] = current
	return nil
}

func addRewriteMetric(left, right int) (int, bool) {
	if right > 0 && left > int(^uint(0)>>1)-right {
		return 0, false
	}
	return left + right, true
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
	return rebasePendingLocked(repo, newCommit, dataStore, state)
}

func rebasePendingLocked(
	repo *gitcmd.Repo,
	newCommit string,
	dataStore store.Store,
	state model.State,
) (RewriteResult, error) {
	result := RewriteResult{}
	if newCommit != "" {
		if data, found, readErr := repo.ReadNote(newCommit); readErr != nil {
			return RewriteResult{}, readErr
		} else if found {
			if _, decodeErr := notes.Decode(data); decodeErr != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("ignored invalid attribution note on %s: %v", newCommit, decodeErr))
				state.LastAnnotatedCommit = ""
				state.Pending.BaseCommit = ""
			} else {
				state.LastAnnotatedCommit = newCommit
				state.Pending.BaseCommit = newCommit
			}
		} else {
			state.LastAnnotatedCommit = ""
			state.Pending.BaseCommit = ""
		}
	}
	next := make(map[string]model.PendingFile, len(state.Pending.Files))
	matcherBudget := engine.NewMatcherBudget(maxRewriteMatcherCells)
	blobCache := newRewriteBlobCache()
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
		sourceContent, err := blobCache.Read(repo, pending.Blob)
		if err != nil {
			if errors.Is(err, errRewriteBudget) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped pending path %s: %v", path, err))
				continue
			}
			return RewriteResult{}, fmt.Errorf("read pending snapshot %s: %w", path, err)
		}
		source, err := engine.NewSnapshot(sourceContent, pending.Ranges)
		if err != nil {
			return RewriteResult{}, fmt.Errorf("decode pending snapshot %s: %w", path, err)
		}
		projected, err := engine.ProjectWithBudget(
			source,
			content,
			model.Attribution{Author: model.AuthorUntracked},
			matcherBudget,
		)
		if err != nil {
			if errors.Is(err, engine.ErrMatcherBudget) {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped pending path %s: %v", path, err))
				continue
			}
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
	if err := writeRewriteState(repo, dataStore, state); err != nil {
		return RewriteResult{}, fmt.Errorf("write checkout state: %w", err)
	}
	return result, nil
}

// HandleReferenceTransaction applies reset, branch, and stash ref updates.
func HandleReferenceTransaction(repo *gitcmd.Repo, input io.Reader, phase string) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("reference transaction repository is nil")
	}
	if phase != "committed" {
		return RewriteResult{}, nil
	}
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return RewriteResult{}, err
	}
	updates, err := rewrite.ParseReferenceTransactionForLength(input, objectIDLength)
	if err != nil {
		return RewriteResult{}, err
	}
	currentBranch, attached, err := repo.CurrentBranchRef()
	if err != nil {
		return RewriteResult{}, err
	}
	var result RewriteResult
	for _, update := range updates {
		switch {
		case update.Ref == "HEAD" || (attached && update.Ref == currentBranch):
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
	defer held.Release()
	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	if state.LastAnnotatedCommit != update.Old && len(state.Pending.Files) == 0 {
		return RewriteResult{}, nil
	}
	dirty, err := repo.DirtyPaths()
	if err != nil {
		return RewriteResult{}, err
	}
	if len(dirty) > 0 && update.New != "" && !isZero(update.New) {
		if data, found, readErr := repo.ReadNote(update.Old); readErr != nil {
			return RewriteResult{}, readErr
		} else if found {
			note, decodeErr := notes.Decode(data)
			if decodeErr != nil {
				result, rebaseErr := rebasePendingLocked(repo, update.New, dataStore, state)
				if rebaseErr != nil {
					return RewriteResult{}, rebaseErr
				}
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("ignored invalid reset source note on %s: %v", update.Old, decodeErr))
				return result, nil
			}
			pending, projectErr := pendingFromNote(
				repo,
				note,
				newRewriteBlobCache(),
				engine.NewMatcherBudget(maxRewriteMatcherCells),
			)
			if projectErr != nil {
				return RewriteResult{}, projectErr
			}
			boundary := annotationBoundary(repo, update.New)
			result := RewriteResult{Mapped: len(pending)}
			if boundary == "" {
				if targetData, targetFound, targetErr := repo.ReadNote(update.New); targetErr != nil {
					return RewriteResult{}, targetErr
				} else if targetFound {
					if _, decodeErr := notes.Decode(targetData); decodeErr != nil {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("ignored invalid reset target note on %s: %v", update.New, decodeErr))
					}
				}
			}
			state.LastAnnotatedCommit = boundary
			state.Pending = model.PendingState{BaseCommit: state.LastAnnotatedCommit, Files: pending}
			if err := writeRewriteState(repo, dataStore, state); err != nil {
				return RewriteResult{}, err
			}
			return result, nil
		}
		return rebasePendingLocked(repo, update.New, dataStore, state)
	}
	result := RewriteResult{Mapped: 1}
	if update.New == "" || isZero(update.New) {
		state.LastAnnotatedCommit = ""
		state.Pending = model.PendingState{Files: map[string]model.PendingFile{}}
	} else if data, found, readErr := repo.ReadNote(update.New); readErr != nil {
		return RewriteResult{}, readErr
	} else if found {
		if _, decodeErr := notes.Decode(data); decodeErr != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("ignored invalid reset target note on %s: %v", update.New, decodeErr))
			state.LastAnnotatedCommit = ""
			state.Pending.BaseCommit = ""
			state.Pending.Files = map[string]model.PendingFile{}
		} else {
			state.LastAnnotatedCommit = update.New
			state.Pending = model.PendingState{
				BaseCommit: update.New,
				Files:      map[string]model.PendingFile{},
			}
		}
	} else {
		state.LastAnnotatedCommit = ""
		state.Pending.BaseCommit = ""
		state.Pending.Files = map[string]model.PendingFile{}
	}
	if err := writeRewriteState(repo, dataStore, state); err != nil {
		return RewriteResult{}, err
	}
	return result, nil
}

func writeRewriteState(repo *gitcmd.Repo, dataStore store.Store, state model.State) error {
	if err := dataStore.CheckStateWrite(state); err != nil {
		return err
	}
	records, _, err := dataStore.ReadCheckpoints()
	if err != nil {
		return err
	}
	protected := retainedBlobs(records, state, model.Checkpoint{})
	if err := repo.ProtectBlobs(protected); err != nil {
		return fmt.Errorf("protect pending snapshots: %w", err)
	}
	if err := dataStore.WriteState(state); err != nil {
		return err
	}
	if err := repo.ProtectBlobs(retainedBlobs(records, state, model.Checkpoint{})); err != nil {
		return fmt.Errorf("compact pending snapshots: %w", err)
	}
	return nil
}

func pendingFromNote(
	repo *gitcmd.Repo,
	note model.Note,
	blobCache *rewriteBlobCache,
	matcherBudget *engine.MatcherBudget,
) (map[string]model.PendingFile, error) {
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
		ranges, err := projectNoteFileWithBudgetAndCache(
			repo,
			file,
			content,
			matcherBudget,
			blobCache,
		)
		if err != nil {
			if errors.Is(err, errRewriteBudget) || errors.Is(err, engine.ErrMatcherBudget) {
				continue
			}
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
		return handleStashDrop(repo, update.Old)
	}
	if update.Old != "" && !isZero(update.Old) {
		if _, oldFound, err := repo.ReadNoteRef(stashNotesRef, update.Old); err != nil {
			return RewriteResult{}, err
		} else if oldFound {
			if _, newFound, err := repo.ReadNoteRef(stashNotesRef, update.New); err != nil {
				return RewriteResult{}, err
			} else if newFound {
				return handleStashDrop(repo, update.Old)
			}
		}
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
	state, err := dataStore.ReadState()
	if err != nil {
		return RewriteResult{}, err
	}
	if len(state.Pending.Files) == 0 {
		return RewriteResult{}, nil
	}
	paths, err := repo.StashPaths(update.New)
	if err != nil {
		return RewriteResult{}, err
	}
	stashed := make(map[string]bool, len(paths))
	for _, path := range paths {
		stashed[path] = true
	}
	files := make(map[string]model.NoteFile)
	remaining := make(map[string]model.PendingFile, len(state.Pending.Files))
	for path, pending := range state.Pending.Files {
		if stashed[path] {
			files[path] = model.NoteFile{Blob: pending.Blob, Ranges: pending.Ranges}
			continue
		}
		remaining[path] = pending
	}
	if len(files) == 0 {
		return RewriteResult{}, nil
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
	if err := writeStashOwnership(repo, update.New, data); err != nil {
		return RewriteResult{}, err
	}
	state.Pending.Files = remaining
	if err := writeRewriteState(repo, dataStore, state); err != nil {
		return RewriteResult{}, err
	}
	return RewriteResult{Written: 1}, nil
}

func handleStashDrop(repo *gitcmd.Repo, stash string) (RewriteResult, error) {
	if stash == "" || isZero(stash) {
		return RewriteResult{}, nil
	}
	paths, err := repo.StashPaths(stash)
	if err != nil {
		return RewriteResult{}, err
	}
	applied, err := repo.StashApplied(stash, paths)
	if err != nil {
		return RewriteResult{}, err
	}
	if applied {
		return HandleStashApply(repo, stash, false)
	}
	commonLock, err := lock.Acquire(filepath.Join(repo.CommonDir, "byline", "notes.lock"), lockTimeout)
	if err != nil {
		return RewriteResult{}, err
	}
	defer commonLock.Release()
	if _, exists, refErr := repo.RefValue("refs/stash"); refErr != nil {
		return RewriteResult{}, refErr
	} else if !exists {
		return deleteAllDroppedStashNotesLocked(repo)
	}
	return deleteDroppedStashNote(repo, stash)
}

// HandleStashApply restores stash attribution to pending state.
func HandleStashApply(repo *gitcmd.Repo, stashCommit string, keep bool) (RewriteResult, error) {
	if repo == nil {
		return RewriteResult{}, errors.New("stash repository is nil")
	}
	objectIDLength, err := repo.ObjectIDLength()
	if err != nil {
		return RewriteResult{}, err
	}
	if err := rewrite.ValidateFullObjectID(stashCommit, objectIDLength, false); err != nil {
		return RewriteResult{}, fmt.Errorf("invalid stash commit: %w", err)
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
	return handleStashApplyLocked(repo, stashCommit, keep, dataStore)
}

func handleStashApplyLocked(
	repo *gitcmd.Repo,
	stashCommit string,
	keep bool,
	dataStore store.Store,
) (RewriteResult, error) {
	data, found, err := repo.ReadNoteRef(stashNotesRef, stashCommit)
	if err != nil {
		return RewriteResult{}, err
	}
	if !found {
		return RewriteResult{}, nil
	}
	owned, err := stashNoteOwned(repo, stashCommit, data)
	if err != nil {
		return RewriteResult{}, err
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
	matcherBudget := engine.NewMatcherBudget(maxRewriteMatcherCells)
	blobCache := newRewriteBlobCache()
	result := RewriteResult{}
	projectionIncomplete := false
	for path, file := range note.Files {
		content, exists, normalized, readErr := repo.WorktreeFile(path)
		if readErr != nil || !exists {
			continue
		}
		projected, projectErr := projectNoteFileWithBudgetAndCache(
			repo,
			file,
			content,
			matcherBudget,
			blobCache,
		)
		if projectErr != nil {
			if errors.Is(projectErr, errRewriteBudget) ||
				errors.Is(projectErr, engine.ErrMatcherBudget) {
				projectionIncomplete = true
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped stash path %s: %v", path, projectErr))
				continue
			}
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
	if state.Pending.Files == nil {
		state.Pending.Files = map[string]model.PendingFile{}
	}
	for path, value := range pending {
		if _, exists := state.Pending.Files[path]; exists {
			continue
		}
		state.Pending.Files[path] = value
	}
	state.Pending.BaseCommit = boundary
	result.Mapped = len(pending)
	if err := writeRewriteState(repo, dataStore, state); err != nil {
		return RewriteResult{}, err
	}
	if !keep && !projectionIncomplete {
		removed, err := removeStashNote(repo, stashCommit, data, owned)
		if err != nil {
			return RewriteResult{}, err
		}
		if !removed {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("skipped removing changed stash attribution note on %s", stashCommit))
		}
	} else if !keep {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("kept stash attribution note on %s after incomplete projection", stashCommit))
	}
	return result, nil
}

func projectNoteFileWithBudgetAndCache(
	repo *gitcmd.Repo,
	file model.NoteFile,
	content []byte,
	budget *engine.MatcherBudget,
	blobCache *rewriteBlobCache,
) ([]model.Range, error) {
	var sourceContent []byte
	var err error
	if blobCache == nil {
		sourceContent, err = repo.ReadBlob(file.Blob)
	} else {
		sourceContent, err = blobCache.Read(repo, file.Blob)
	}
	if err != nil {
		return nil, err
	}
	source, err := engine.NewSnapshot(sourceContent, file.Ranges)
	if err != nil {
		return nil, err
	}
	projected, err := engine.ProjectWithBudget(
		source,
		content,
		model.Attribution{Author: model.AuthorUntracked},
		budget,
	)
	if err != nil {
		return nil, err
	}
	return projected.Ranges()
}

func removeStashNote(repo *gitcmd.Repo, commit string, expected []byte, owned bool) (bool, error) {
	if !owned {
		return false, nil
	}
	removed, err := repo.DeleteNoteRefIfEqual(stashNotesRef, commit, expected)
	if err != nil {
		return false, fmt.Errorf("remove stash attribution: %w", err)
	}
	if removed {
		if err := removeStashOwnership(repo, commit, expected); err != nil {
			return false, err
		}
	}
	return removed, nil
}

func deleteDroppedStashNote(repo *gitcmd.Repo, commit string) (RewriteResult, error) {
	data, found, err := repo.ReadNoteRef(stashNotesRef, commit)
	if err != nil {
		return RewriteResult{}, err
	}
	if !found {
		return RewriteResult{}, nil
	}
	owned, err := stashNoteOwned(repo, commit, data)
	if err != nil {
		return RewriteResult{}, err
	}
	if !owned {
		return RewriteResult{Warnings: []string{
			fmt.Sprintf("skipped removing unowned stash attribution note on %s", commit),
		}}, nil
	}
	note, err := notes.Decode(data)
	if err != nil {
		return RewriteResult{Warnings: []string{
			fmt.Sprintf("skipped removing invalid stash attribution note on %s", commit),
		}}, nil
	}
	canonical, err := notes.Encode(note)
	if err != nil || !bytes.Equal(canonical, data) {
		return RewriteResult{Warnings: []string{
			fmt.Sprintf("skipped removing changed stash attribution note on %s", commit),
		}}, nil
	}
	removed, err := repo.DeleteNoteRefIfEqual(stashNotesRef, commit, data)
	if err != nil {
		return RewriteResult{}, fmt.Errorf("remove dropped stash attribution: %w", err)
	}
	if !removed {
		return RewriteResult{Warnings: []string{
			fmt.Sprintf("skipped removing changed stash attribution note on %s", commit),
		}}, nil
	}
	if err := removeStashOwnership(repo, commit, data); err != nil {
		return RewriteResult{}, err
	}
	return RewriteResult{Written: 1}, nil
}

func writeStashOwnership(repo *gitcmd.Repo, commit string, data []byte) error {
	digest := sha256.Sum256(data)
	ownership := []byte(fmt.Sprintf("%x\n", digest[:]))
	if err := repo.WriteNoteRef(stashOwnershipRef, commit, ownership); err != nil {
		return fmt.Errorf("write stash attribution ownership: %w", err)
	}
	return nil
}

func stashNoteOwned(repo *gitcmd.Repo, commit string, data []byte) (bool, error) {
	ownership, found, err := repo.ReadNoteRef(stashOwnershipRef, commit)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	digest := sha256.Sum256(data)
	expected := []byte(fmt.Sprintf("%x\n", digest[:]))
	return bytes.Equal(ownership, expected), nil
}

func removeStashOwnership(repo *gitcmd.Repo, commit string, data []byte) error {
	digest := sha256.Sum256(data)
	expected := []byte(fmt.Sprintf("%x\n", digest[:]))
	_, err := repo.DeleteNoteRefIfEqual(stashOwnershipRef, commit, expected)
	if err != nil {
		return fmt.Errorf("remove stash attribution ownership: %w", err)
	}
	return nil
}

func deleteAllDroppedStashNotesLocked(repo *gitcmd.Repo) (RewriteResult, error) {
	commits, err := repo.NoteCommits(stashNotesRef)
	if err != nil {
		return RewriteResult{}, err
	}
	var result RewriteResult
	for _, commit := range commits {
		value, removeErr := deleteDroppedStashNote(repo, commit)
		if removeErr != nil {
			return RewriteResult{}, removeErr
		}
		result.Written += value.Written
		result.Warnings = append(result.Warnings, value.Warnings...)
	}
	return result, nil
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
