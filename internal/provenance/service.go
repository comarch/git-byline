// Package provenance orchestrates checkpoints, notes, and line attribution.
package provenance

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mrwogu/git-byline/internal/engine"
	"github.com/mrwogu/git-byline/internal/gitcmd"
	"github.com/mrwogu/git-byline/internal/lock"
	"github.com/mrwogu/git-byline/internal/model"
	"github.com/mrwogu/git-byline/internal/notes"
	"github.com/mrwogu/git-byline/internal/preset"
	"github.com/mrwogu/git-byline/internal/store"
)

const lockTimeout = 10 * time.Second

var errUnsupportedBase = errors.New("unsupported base content or attribution")

// CaptureResult reports a checkpoint write.
type CaptureResult struct {
	Recorded int
	Warnings []string
}

// Capture snapshots paths from one normalized agent event.
func Capture(repo *gitcmd.Repo, event preset.Event, now time.Time) (CaptureResult, error) {
	attribution := model.Attribution{Author: event.Type}
	if event.Type == model.AuthorAI {
		attribution.Agent = event.Agent
		attribution.Model = event.Model
		attribution.Session = event.Session
	}
	if err := model.ValidateAttribution(attribution); err != nil {
		return CaptureResult{}, fmt.Errorf("validate event attribution: %w", err)
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return CaptureResult{}, err
	}
	defer held.Release()

	records, warnings, err := dataStore.ReadCheckpoints()
	if err != nil {
		return CaptureResult{}, err
	}
	state, err := dataStore.ReadState()
	if err != nil {
		return CaptureResult{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return CaptureResult{}, err
	}
	seq := state.LastCheckpointSeq + 1
	if len(records) > 0 && records[len(records)-1].Seq >= seq {
		seq = records[len(records)-1].Seq + 1
	}
	snapshots := make([]model.Snapshot, 0, len(event.Paths))
	seen := map[string]bool{}
	for _, path := range event.Paths {
		normalized, err := repo.NormalizeWorktreePath(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %q: %v", path, err))
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		snapshot, err := repo.SnapshotWorktree(normalized)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %q: %v", normalized, err))
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	if len(snapshots) == 0 {
		return CaptureResult{Warnings: warnings}, nil
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Path < snapshots[j].Path })
	record := model.Checkpoint{
		Version:    model.CheckpointVersion,
		Kind:       "edit",
		Seq:        seq,
		BaseCommit: head,
		TS:         now.UTC().Format(time.RFC3339Nano),
		Type:       event.Type,
		Files:      snapshots,
	}
	if event.Type == model.AuthorAI {
		record.Agent = event.Agent
		record.Model = event.Model
		record.Session = event.Session
	}
	retained := retainedBlobs(records, state, record)
	if err := repo.ProtectBlobs(retained); err != nil {
		return CaptureResult{}, fmt.Errorf("protect checkpoint blobs: %w", err)
	}
	if err := dataStore.AppendCheckpoint(record); err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Recorded: len(snapshots), Warnings: warnings}, nil
}

func retainedBlobs(records []model.Checkpoint, state model.State, extra model.Checkpoint) []string {
	var result []string
	for _, record := range records {
		if record.Seq <= state.LastCheckpointSeq {
			continue
		}
		for _, file := range record.Files {
			if file.Exists {
				result = append(result, file.Blob)
			}
		}
	}
	for _, file := range state.Pending.Files {
		result = append(result, file.Blob)
	}
	for _, file := range extra.Files {
		if file.Exists {
			result = append(result, file.Blob)
		}
	}
	return result
}

// AnnotateResult reports a completed commit annotation.
type AnnotateResult struct {
	Commit   string
	Files    int
	Noop     bool
	Warnings []string
}

// Annotate writes deterministic attribution for HEAD.
func Annotate(repo *gitcmd.Repo) (AnnotateResult, error) {
	dataStore := store.New(repo.GitDir)
	commonLock, err := lock.Acquire(filepath.Join(repo.CommonDir, "byline", "notes.lock"), lockTimeout)
	if err != nil {
		return AnnotateResult{}, err
	}
	defer commonLock.Release()
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return AnnotateResult{}, err
	}
	defer held.Release()

	head, err := repo.Head()
	if err != nil {
		return AnnotateResult{}, err
	}
	if head == "" {
		return AnnotateResult{}, errors.New("cannot annotate an unborn repository")
	}
	state, err := dataStore.ReadState()
	if err != nil {
		return AnnotateResult{}, err
	}
	records, warnings, err := dataStore.ReadCheckpoints()
	if err != nil {
		return AnnotateResult{}, err
	}
	if state.LastAnnotatedCommit != "" {
		if err := requireAttributionNote(repo, state.LastAnnotatedCommit); err != nil {
			return AnnotateResult{}, err
		}
	}
	if state.LastAnnotatedCommit == head {
		if err := repo.ProtectBlobs(retainedBlobs(records, state, model.Checkpoint{})); err != nil {
			return AnnotateResult{}, fmt.Errorf("compact retained snapshots: %w", err)
		}
		return AnnotateResult{Commit: head, Noop: true, Warnings: warnings}, nil
	}
	parents, err := repo.Parents(head)
	if err != nil {
		return AnnotateResult{}, err
	}
	parent := ""
	if len(parents) > 0 {
		parent = parents[0]
	}
	if state.LastAnnotatedCommit != "" && state.LastAnnotatedCommit != parent {
		return AnnotateResult{}, fmt.Errorf("commit gap or divergent history: last annotated %s, HEAD parent %s", state.LastAnnotatedCommit, parent)
	}
	if state.LastAnnotatedCommit == "" && parent != "" {
		warnings = append(warnings, "initializing attribution on a repository with existing history")
	}

	active, carry, lastSeq, err := selectRecords(records, state.LastCheckpointSeq, parent, head)
	if err != nil {
		return AnnotateResult{}, err
	}
	changes, err := repo.Changes(head, parent)
	if err != nil {
		return AnnotateResult{}, err
	}
	changedByPath := make(map[string]gitcmd.Change, len(changes))
	for _, change := range changes {
		changedByPath[change.Path] = change
	}
	allRecords := make([]model.Checkpoint, 0, len(active)+len(carry))
	allRecords = append(allRecords, active...)
	allRecords = append(allRecords, carry...)
	paths := collectPaths(allRecords, state, changedByPath)
	note := model.Note{Version: model.NoteVersion, Files: map[string]model.NoteFile{}}
	nextPending := map[string]model.PendingFile{}
	mergeFallback := model.Attribution{Author: model.AuthorHuman}
	if len(parents) > 1 {
		mergeFallback = model.Attribution{Author: model.AuthorUntracked}
		warnings = append(warnings, "merge commit uses first-parent attribution; unmatched content is untracked")
	}

	for _, path := range paths {
		change, changed := changedByPath[path]
		sourcePath := path
		if changed && change.OldPath != "" {
			sourcePath = change.OldPath
		}
		initialPath := sourcePath
		if _, ok := state.Pending.Files[path]; ok {
			initialPath = path
		}
		initial, initialWarnings, err := initialSnapshot(repo, state, parent, initialPath)
		warnings = append(warnings, initialWarnings...)
		if err != nil {
			if !errors.Is(err, errUnsupportedBase) {
				return AnnotateResult{}, fmt.Errorf("initialize %s: %w", path, err)
			}
			warnings = append(warnings, fmt.Sprintf("base content for %s is unsupported: %v", path, err))
			initial = engine.Snapshot{}
		}
		transitions, err := transitionsFor(repo, active, sourcePath, path)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("replay %s: %w", path, err)
		}
		replayed, err := engine.Replay(initial, transitions)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("replay %s: %w", path, err)
		}

		committed := replayed
		if changed {
			if change.Status == 'D' {
				committed = engine.Snapshot{}
			} else {
				blob, exists, err := repo.BlobID(head, path)
				if err != nil {
					return AnnotateResult{}, err
				}
				if !exists {
					return AnnotateResult{}, fmt.Errorf("changed path %s is missing from commit", path)
				}
				content, err := repo.ReadBlob(blob)
				if err != nil {
					if errors.Is(err, gitcmd.ErrOutputLimit) {
						warnings = append(warnings, fmt.Sprintf("skipped unsupported path %s: %v", path, err))
						continue
					}
					return AnnotateResult{}, err
				}
				committed, err = engine.Project(replayed, content, mergeFallback)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skipped unsupported path %s: %v", path, err))
				} else {
					ranges, err := committed.Ranges()
					if err != nil {
						return AnnotateResult{}, err
					}
					note.Files[path] = model.NoteFile{Blob: blob, Ranges: ranges}
				}
			}
		}

		carryTransitions, err := transitionsFor(repo, carry, path, path)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("carry pending %s: %w", path, err)
		}
		pendingSource, err := engine.Replay(replayed, carryTransitions)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("carry pending %s: %w", path, err)
		}
		worktreeContent, exists, normalized, readErr := repo.WorktreeFile(path)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("cannot retain pending path %s: %v", path, readErr))
			continue
		}
		if !exists {
			continue
		}
		worktree, err := engine.Project(pendingSource, worktreeContent, model.Attribution{Author: model.AuthorHuman})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot attribute pending path %s: %v", path, err))
			continue
		}
		if changed && snapshotsEqual(worktree, committed) {
			continue
		}
		if !changed && snapshotsEqual(worktree, initial) && len(transitions) == 0 {
			if pending, ok := state.Pending.Files[sourcePath]; ok {
				nextPending[normalized] = pending
			}
			continue
		}
		oid, err := repo.HashBytes(worktreeContent)
		if err != nil {
			return AnnotateResult{}, err
		}
		ranges, err := worktree.Ranges()
		if err != nil {
			return AnnotateResult{}, err
		}
		nextPending[normalized] = model.PendingFile{Blob: oid, Ranges: ranges}
	}

	data, err := notes.Encode(note)
	if err != nil {
		return AnnotateResult{}, err
	}
	if err := repo.WriteNote(head, data); err != nil {
		return AnnotateResult{}, err
	}
	state.LastAnnotatedCommit = head
	state.LastCheckpointSeq = lastSeq
	state.Pending = model.PendingState{BaseCommit: head, Files: nextPending}
	if err := dataStore.WriteState(state); err != nil {
		return AnnotateResult{}, err
	}
	if err := repo.ProtectBlobs(pendingBlobs(state)); err != nil {
		return AnnotateResult{}, fmt.Errorf("compact retained snapshots: %w", err)
	}
	return AnnotateResult{
		Commit:   head,
		Files:    len(note.Files),
		Warnings: warnings,
	}, nil
}

func selectRecords(records []model.Checkpoint, consumed uint64, base, head string) ([]model.Checkpoint, []model.Checkpoint, uint64, error) {
	last := consumed
	var active []model.Checkpoint
	var carry []model.Checkpoint
	carryStarted := false
	for _, record := range records {
		if record.Seq <= consumed {
			continue
		}
		switch record.BaseCommit {
		case base:
			if carryStarted {
				return nil, nil, consumed, fmt.Errorf("checkpoint %d for parent appears after a HEAD checkpoint", record.Seq)
			}
			active = append(active, record)
		case head:
			carryStarted = true
			carry = append(carry, record)
		default:
			return nil, nil, consumed, fmt.Errorf("checkpoint %d belongs to unrelated base commit %q", record.Seq, record.BaseCommit)
		}
		last = record.Seq
	}
	return active, carry, last, nil
}

func collectPaths(records []model.Checkpoint, state model.State, changes map[string]gitcmd.Change) []string {
	paths := map[string]bool{}
	for path := range state.Pending.Files {
		paths[path] = true
	}
	for _, record := range records {
		for _, file := range record.Files {
			paths[file.Path] = true
		}
	}
	for path := range changes {
		paths[path] = true
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

func initialSnapshot(repo *gitcmd.Repo, state model.State, parent, path string) (engine.Snapshot, []string, error) {
	if pending, ok := state.Pending.Files[path]; ok {
		content, err := repo.ReadBlob(pending.Blob)
		if err != nil {
			return engine.Snapshot{}, nil, err
		}
		snapshot, err := engine.NewSnapshot(content, pending.Ranges)
		return snapshot, nil, err
	}
	if parent == "" {
		return engine.Snapshot{}, nil, nil
	}
	blob, exists, err := repo.BlobID(parent, path)
	if err != nil {
		return engine.Snapshot{}, nil, err
	}
	if !exists {
		return engine.Snapshot{}, nil, nil
	}
	content, err := repo.ReadBlob(blob)
	if err != nil {
		if errors.Is(err, gitcmd.ErrOutputLimit) {
			return engine.Snapshot{}, nil, fmt.Errorf("%w: %v", errUnsupportedBase, err)
		}
		return engine.Snapshot{}, nil, err
	}
	file, found, warnings, err := notes.FindFile(repo, parent, path, blob)
	if err != nil {
		return engine.Snapshot{}, warnings, err
	}
	if found {
		snapshot, err := engine.NewSnapshot(content, file.Ranges)
		if err != nil {
			return engine.Snapshot{}, warnings, fmt.Errorf("%w: %v", errUnsupportedBase, err)
		}
		return snapshot, warnings, nil
	}
	ranges, err := engine.UniformRanges(content, model.Attribution{Author: model.AuthorUntracked})
	if err != nil {
		return engine.Snapshot{}, warnings, fmt.Errorf("%w: %v", errUnsupportedBase, err)
	}
	snapshot, err := engine.NewSnapshot(content, ranges)
	return snapshot, warnings, err
}

func transitionsFor(repo *gitcmd.Repo, records []model.Checkpoint, sourcePath, targetPath string) ([]engine.Transition, error) {
	var transitions []engine.Transition
	for _, record := range records {
		var selected *model.Snapshot
		for _, file := range record.Files {
			if file.Path == targetPath {
				value := file
				selected = &value
				break
			}
			if file.Path == sourcePath {
				value := file
				selected = &value
			}
		}
		if selected == nil {
			continue
		}
		var content []byte
		var err error
		if selected.Exists {
			content, err = repo.ReadBlob(selected.Blob)
			if err != nil {
				return nil, err
			}
		}
		attr := model.Attribution{Author: record.Type}
		if record.Type == model.AuthorAI {
			attr.Agent = record.Agent
			attr.Model = record.Model
			attr.Session = record.Session
			attr.TS = record.TS
		}
		transitions = append(transitions, engine.Transition{Content: content, Attribution: attr})
	}
	return transitions, nil
}

func snapshotsEqual(left, right engine.Snapshot) bool {
	if len(left.Lines) != len(right.Lines) {
		return false
	}
	for i := range left.Lines {
		if left.Lines[i] != right.Lines[i] || left.Attributions[i] != right.Attributions[i] {
			return false
		}
	}
	return true
}

func pendingBlobs(state model.State) []string {
	result := make([]string, 0, len(state.Pending.Files))
	for _, file := range state.Pending.Files {
		result = append(result, file.Blob)
	}
	return result
}

func requireAttributionNote(repo *gitcmd.Repo, commit string) error {
	data, found, err := repo.ReadNote(commit)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("state marks %s annotated but its attribution note is missing", commit)
	}
	if _, err := notes.Decode(data); err != nil {
		return fmt.Errorf("state marks %s annotated but its attribution note is invalid: %w", commit, err)
	}
	return nil
}

// BlameLine is one rendered file line.
type BlameLine struct {
	Number      int               `json:"number"`
	Content     string            `json:"content"`
	Attribution model.Attribution `json:"attribution"`
}

// BlameResult is attribution for one committed file.
type BlameResult struct {
	Version  int         `json:"version"`
	File     string      `json:"file"`
	Blob     string      `json:"blob"`
	Commit   string      `json:"commit"`
	Lines    []BlameLine `json:"lines"`
	Warnings []string    `json:"warnings,omitempty"`
}

// Blame reads line attribution for path at HEAD.
func Blame(repo *gitcmd.Repo, path string) (BlameResult, error) {
	path, err := repo.NormalizeWorktreePath(path)
	if err != nil {
		return BlameResult{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return BlameResult{}, err
	}
	if head == "" {
		return BlameResult{}, errors.New("cannot blame an unborn repository")
	}
	blob, exists, err := repo.BlobID(head, path)
	if err != nil {
		return BlameResult{}, err
	}
	if !exists {
		return BlameResult{}, fmt.Errorf("path %q does not exist at HEAD", path)
	}
	content, err := repo.ReadBlob(blob)
	if err != nil {
		return BlameResult{}, err
	}
	file, found, warnings, err := notes.FindFile(repo, head, path, blob)
	if err != nil {
		return BlameResult{}, err
	}
	var snapshot engine.Snapshot
	if found {
		snapshot, err = engine.NewSnapshot(content, file.Ranges)
	} else {
		var ranges []model.Range
		ranges, err = engine.UniformRanges(content, model.Attribution{Author: model.AuthorUntracked})
		if err == nil {
			snapshot, err = engine.NewSnapshot(content, ranges)
		}
	}
	if err != nil {
		return BlameResult{}, err
	}
	lines := make([]BlameLine, len(snapshot.Lines))
	for i, line := range snapshot.Lines {
		line = strings.TrimSuffix(line, "\r\n")
		line = strings.TrimSuffix(line, "\n")
		lines[i] = BlameLine{Number: i + 1, Content: line, Attribution: snapshot.Attributions[i]}
	}
	return BlameResult{
		Version:  model.NoteVersion,
		File:     path,
		Blob:     blob,
		Commit:   head,
		Lines:    lines,
		Warnings: warnings,
	}, nil
}

// StatusResult reports repository attribution state.
type StatusResult struct {
	Version             int      `json:"version"`
	Head                string   `json:"head,omitempty"`
	LastAnnotatedCommit string   `json:"last_annotated_commit,omitempty"`
	PendingCheckpoints  int      `json:"pending_checkpoints"`
	PendingFiles        int      `json:"pending_files"`
	RetainedSnapshots   int      `json:"retained_snapshots"`
	Warnings            []string `json:"warnings,omitempty"`
}

// Status reads current state without changing it.
func Status(repo *gitcmd.Repo) (StatusResult, error) {
	dataStore := store.New(repo.GitDir)
	state, err := dataStore.ReadState()
	if err != nil {
		return StatusResult{}, err
	}
	records, warnings, err := dataStore.ReadCheckpoints()
	if err != nil {
		return StatusResult{}, err
	}
	head, err := repo.Head()
	if err != nil {
		return StatusResult{}, err
	}
	retained, err := repo.ProtectedBlobCount()
	if err != nil {
		return StatusResult{}, err
	}
	pending := 0
	for _, record := range records {
		if record.Seq > state.LastCheckpointSeq {
			pending++
		}
	}
	if head != "" && head != state.LastAnnotatedCommit {
		warnings = append(warnings, "HEAD is not annotated")
	}
	if state.LastAnnotatedCommit != "" {
		if err := requireAttributionNote(repo, state.LastAnnotatedCommit); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return StatusResult{
		Version:             model.StateVersion,
		Head:                head,
		LastAnnotatedCommit: state.LastAnnotatedCommit,
		PendingCheckpoints:  pending,
		PendingFiles:        len(state.Pending.Files),
		RetainedSnapshots:   retained,
		Warnings:            warnings,
	}, nil
}
