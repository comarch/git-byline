// Package provenance orchestrates checkpoints, notes, and line attribution.
package provenance

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/preset"
	"github.com/comarch/git-byline/internal/store"
)

const lockTimeout = 10 * time.Second

var errUnsupportedBase = errors.New("unsupported base content or attribution")

const (
	maxBlameCollectionFiles = notes.MaxFiles
	maxBlameCollectionLines = 100_000
	maxBlameCollectionBytes = 16 << 20
	maxShellPaths           = 500
	maxShellSnapshotBytes   = 16 << 20
)

var errShellPathLimit = errors.New("shell event contains more than 500 paths")

// CaptureResult reports a checkpoint write.
type CaptureResult struct {
	Recorded int
	Warnings []string
}

// Capture records one normalized agent event.
func Capture(repo *gitcmd.Repo, event preset.Event, now time.Time) (CaptureResult, error) {
	if event.Type != model.AuthorHuman && event.Type != model.AuthorAI {
		return CaptureResult{}, fmt.Errorf("unsupported event author %q", event.Type)
	}
	kind := event.Kind
	if kind == "" {
		kind = model.CheckpointKindEdit
	}
	switch kind {
	case model.CheckpointKindEdit:
	case model.CheckpointKindShellPre:
		if event.Type != model.AuthorHuman {
			return CaptureResult{}, errors.New("shell_pre event must be human")
		}
	case model.CheckpointKindShellPost:
		if event.Type != model.AuthorAI {
			return CaptureResult{}, errors.New("shell_post event must be ai")
		}
	default:
		return CaptureResult{}, fmt.Errorf("unsupported checkpoint kind %q", kind)
	}
	attribution := model.Attribution{Author: event.Type}
	if event.Type == model.AuthorAI {
		attribution.Agent = event.Agent
		attribution.Model = event.Model
		attribution.Session = event.Session
	}
	if err := model.ValidateAttribution(attribution); err != nil {
		return CaptureResult{}, fmt.Errorf("validate event attribution: %w", err)
	}
	if err := model.ValidateEventID(event.EventID); err != nil {
		return CaptureResult{}, fmt.Errorf("validate event identifier: %w", err)
	}
	dataStore := store.New(repo.GitDir)
	held, err := lock.Acquire(dataStore.LockPath(), lockTimeout)
	if err != nil {
		return CaptureResult{}, err
	}
	defer held.Release()

	records, logWarnings, err := dataStore.ReadCheckpoints()
	if err != nil {
		return CaptureResult{}, err
	}
	warnings := append([]string(nil), logWarnings...)
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
	paths := append([]string(nil), event.Paths...)
	var before model.Checkpoint
	if kind == model.CheckpointKindShellPre {
		var err error
		paths, err = shellDirtyPaths(repo)
		if err != nil {
			if errors.Is(err, errShellPathLimit) {
				warnings = append(warnings, "ignored shell event with more than 500 paths")
				return CaptureResult{Warnings: warnings}, nil
			}
			return CaptureResult{}, err
		}
	} else if kind == model.CheckpointKindShellPost {
		var found bool
		before, found = shellPreForPost(records, event.EventID)
		if !found {
			warnings = append(warnings, "ignored shell_post without a matching shell_pre")
			return CaptureResult{Warnings: warnings}, nil
		}
		if before.BaseCommit != head {
			warnings = append(warnings, "ignored shell_post with a different base commit")
			return CaptureResult{Warnings: warnings}, nil
		}
		var err error
		paths, err = shellPostPaths(repo, before)
		if err != nil {
			if errors.Is(err, errShellPathLimit) {
				warnings = append(warnings, "ignored shell event with more than 500 paths")
				return CaptureResult{Warnings: warnings}, nil
			}
			return CaptureResult{}, err
		}
	}
	snapshots := make([]model.Snapshot, 0, len(paths))
	seen := map[string]bool{}
	var shellBytes int64
	for _, path := range paths {
		normalized, err := repo.NormalizeWorktreePath(path)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("skipped %q: %v", path, err))
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		var snapshot model.Snapshot
		var snapshotBytes int64
		if kind == model.CheckpointKindShellPre || kind == model.CheckpointKindShellPost {
			remaining := int64(maxShellSnapshotBytes) - shellBytes
			snapshot, snapshotBytes, err = repo.SnapshotWorktreeWithLimit(normalized, remaining)
		} else {
			snapshot, err = repo.SnapshotWorktree(normalized)
		}
		if err != nil {
			if (kind == model.CheckpointKindShellPre || kind == model.CheckpointKindShellPost) &&
				errors.Is(err, gitcmd.ErrSnapshotBudget) {
				warnings = append(warnings, "stopped shell snapshot at the 16 MiB aggregate budget")
				break
			}
			warnings = append(warnings, fmt.Sprintf("skipped %q: %v", normalized, err))
			continue
		}
		shellBytes += snapshotBytes
		if kind == model.CheckpointKindShellPost {
			previous, err := shellPreviousSnapshot(repo, head, before, normalized)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skipped %q: %v", normalized, err))
				continue
			}
			if snapshotsEqualWorktree(previous, snapshot) {
				continue
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	if kind == model.CheckpointKindShellPre && len(records) > 0 &&
		records[len(records)-1].Kind == model.CheckpointKindShellPre &&
		records[len(records)-1].EventID == event.EventID &&
		snapshotsEqualCheckpoints(records[len(records)-1].Files, snapshots) {
		return CaptureResult{}, nil
	}
	if len(snapshots) == 0 && kind == model.CheckpointKindEdit {
		return CaptureResult{Warnings: warnings}, nil
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Path < snapshots[j].Path })
	record := model.Checkpoint{
		Version:    model.CheckpointVersion,
		Kind:       kind,
		Seq:        seq,
		BaseCommit: head,
		EventID:    event.EventID,
		TS:         now.UTC().Format(time.RFC3339Nano),
		Type:       event.Type,
		Files:      snapshots,
	}
	if event.Type == model.AuthorAI {
		record.Agent = event.Agent
		record.Model = event.Model
		record.Session = event.Session
	}
	if err := dataStore.CheckCheckpointAppend(record, len(records)+len(logWarnings)); err != nil {
		return CaptureResult{}, err
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

func shellDirtyPaths(repo *gitcmd.Repo) ([]string, error) {
	paths, err := repo.DirtyPaths()
	if err != nil {
		return nil, fmt.Errorf("list shell paths: %w", err)
	}
	if len(paths) > maxShellPaths {
		return nil, errShellPathLimit
	}
	return paths, nil
}

func shellPostPaths(repo *gitcmd.Repo, before model.Checkpoint) ([]string, error) {
	paths, err := repo.DirtyPaths()
	if err != nil {
		return nil, fmt.Errorf("list shell paths: %w", err)
	}
	unique := make(map[string]bool, len(paths)+len(before.Files))
	for _, path := range paths {
		unique[path] = true
	}
	for _, file := range before.Files {
		unique[file.Path] = true
	}
	paths = paths[:0]
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > maxShellPaths {
		return nil, errShellPathLimit
	}
	return paths, nil
}

func shellPreForPost(records []model.Checkpoint, eventID string) (model.Checkpoint, bool) {
	var unpaired []model.Checkpoint
	for _, record := range records {
		switch record.Kind {
		case model.CheckpointKindShellPre:
			unpaired = append(unpaired, record)
		case model.CheckpointKindShellPost:
			index := latestShellPreIndex(unpaired, record.EventID, record.EventID != "")
			if index >= 0 {
				unpaired = append(unpaired[:index], unpaired[index+1:]...)
			}
		}
	}
	index := latestShellPreIndex(unpaired, eventID, eventID != "")
	if index < 0 {
		return model.Checkpoint{}, false
	}
	return unpaired[index], true
}

func latestShellPreIndex(records []model.Checkpoint, eventID string, requireID bool) int {
	for index := len(records) - 1; index >= 0; index-- {
		if !requireID || records[index].EventID == eventID {
			return index
		}
	}
	return -1
}

func shellPreviousSnapshot(repo *gitcmd.Repo, head string, before model.Checkpoint, path string) (model.Snapshot, error) {
	for _, file := range before.Files {
		if file.Path == path {
			return file, nil
		}
	}
	if head == "" {
		return model.Snapshot{Path: path}, nil
	}
	blob, exists, err := repo.BlobID(head, path)
	if err != nil {
		return model.Snapshot{}, err
	}
	return model.Snapshot{Path: path, Exists: exists, Blob: blob}, nil
}

func snapshotsEqualWorktree(left, right model.Snapshot) bool {
	return left.Exists == right.Exists && left.Blob == right.Blob
}

func snapshotsEqualCheckpoints(left, right []model.Snapshot) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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

type sessionMetrics map[string]*model.NoteSession

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
	note := model.Note{
		Version: model.NoteVersion,
		Files:   map[string]model.NoteFile{},
	}
	sessions := sessionMetrics{}
	nextPending := map[string]model.PendingFile{}
	identity, err := commitIdentity(repo, head)
	if err != nil {
		return AnnotateResult{}, err
	}
	// contentFallback covers content no checkpoint explains. On a merge it
	// stays untracked, including for the pending worktree projection: the
	// merge result must not become human attribution on the next commit.
	contentFallback := model.Attribution{Author: model.AuthorHuman, Identity: identity}
	if len(parents) > 1 {
		contentFallback = model.Attribution{Author: model.AuthorUntracked}
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
		transitions, err := transitionsFor(repo, active, sourcePath, path, identity)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("replay %s: %w", path, err)
		}
		replayed, transitionStats, err := engine.ReplayWithStats(initial, transitions)
		if err != nil {
			return AnnotateResult{}, fmt.Errorf("replay %s: %w", path, err)
		}
		addTransitionSessionMetrics(sessions, transitionStats)

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
				committed, err = engine.Project(replayed, content, contentFallback)
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("skipped unsupported path %s: %v", path, err))
				} else {
					ranges, err := committed.Ranges()
					if err != nil {
						return AnnotateResult{}, err
					}
					note.Files[path] = model.NoteFile{Blob: blob, Ranges: ranges}
					addCommittedSessionMetrics(sessions, committed)
				}
			}
		}

		carryTransitions, err := transitionsFor(repo, carry, path, path, identity)
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
		worktree, err := engine.Project(pendingSource, worktreeContent, contentFallback)
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

	note.Sessions = materializeSessionMetrics(sessions)
	data, err := notes.Encode(note)
	if err != nil {
		return AnnotateResult{}, err
	}
	nextState := state
	nextState.LastAnnotatedCommit = head
	nextState.LastCheckpointSeq = lastSeq
	nextState.Pending = model.PendingState{BaseCommit: head, Files: nextPending}
	if err := dataStore.CheckStateWrite(nextState); err != nil {
		return AnnotateResult{}, err
	}
	protected := retainedBlobs(records, state, model.Checkpoint{})
	protected = append(protected, pendingBlobs(nextState)...)
	if err := repo.ProtectBlobs(protected); err != nil {
		return AnnotateResult{}, fmt.Errorf("protect pending snapshots: %w", err)
	}
	if err := repo.WriteNote(head, data); err != nil {
		return AnnotateResult{}, err
	}
	if err := dataStore.WriteState(nextState); err != nil {
		return AnnotateResult{}, err
	}
	if err := repo.ProtectBlobs(pendingBlobs(nextState)); err != nil {
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

// commitIdentity resolves the human identity recorded for lines this commit
// introduces. It comes from the commit author, so anyone can reproduce it
// with git log. An identity that cannot be normalized stays empty.
func commitIdentity(repo *gitcmd.Repo, commit string) (string, error) {
	name, email, err := repo.CommitAuthor(commit)
	if err != nil {
		return "", fmt.Errorf("resolve commit author for %s: %w", commit, err)
	}
	return model.NormalizeIdentity(name, email), nil
}

func transitionsFor(
	repo *gitcmd.Repo,
	records []model.Checkpoint,
	sourcePath,
	targetPath,
	identity string,
) ([]engine.Transition, error) {
	var transitions []engine.Transition
	for _, record := range records {
		if record.Type != model.AuthorHuman && record.Type != model.AuthorAI {
			return nil, fmt.Errorf("checkpoint %d has unsupported author %q", record.Seq, record.Type)
		}
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
		} else {
			attr.Identity = identity
		}
		transitions = append(transitions, engine.Transition{Content: content, Attribution: attr})
	}
	return transitions, nil
}

func addTransitionSessionMetrics(sessions sessionMetrics, stats []engine.TransitionStats) {
	for _, value := range stats {
		if value.Attribution.Author == model.AuthorAI && value.Attribution.Session != "" {
			session := ensureSession(sessions, value.Attribution)
			session.Added += value.Added
			session.Deleted += value.Deleted
		}
		for _, attribution := range value.Overridden {
			if attribution.Session != "" {
				ensureSession(sessions, attribution).Overridden++
			}
		}
	}
}

func addCommittedSessionMetrics(sessions sessionMetrics, snapshot engine.Snapshot) {
	for _, attribution := range snapshot.Attributions {
		if attribution.Session == "" {
			continue
		}
		switch attribution.Author {
		case model.AuthorAI:
			ensureSession(sessions, attribution).Accepted++
		}
	}
}

func ensureSession(sessions sessionMetrics, attribution model.Attribution) *model.NoteSession {
	key := model.NoteSessionKey(attribution.Agent, attribution.Session)
	session := sessions[key]
	if session == nil {
		session = &model.NoteSession{}
	}
	if session.Agent == "" {
		session.Agent = attribution.Agent
	}
	if session.Model == "" {
		session.Model = attribution.Model
	}
	if attribution.TS != "" {
		if session.FirstTS == "" || timestampBefore(attribution.TS, session.FirstTS) {
			session.FirstTS = attribution.TS
		}
		if session.LastTS == "" || timestampBefore(session.LastTS, attribution.TS) {
			session.LastTS = attribution.TS
		}
	}
	sessions[key] = session
	return session
}

func timestampBefore(left, right string) bool {
	leftTime, leftErr := time.Parse(time.RFC3339Nano, left)
	rightTime, rightErr := time.Parse(time.RFC3339Nano, right)
	if leftErr != nil || rightErr != nil {
		return left < right
	}
	return leftTime.Before(rightTime)
}

func materializeSessionMetrics(sessions sessionMetrics) map[string]model.NoteSession {
	result := make(map[string]model.NoteSession, len(sessions))
	for key, value := range sessions {
		session := *value
		if session.Accepted == 0 && session.Overridden == 0 {
			session.Added = 0
			session.Deleted = 0
		}
		result[key] = session
	}
	return result
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

// BlameCollection is attribution for every file recorded on HEAD.
type BlameCollection struct {
	Commit   string
	Files    []BlameResult
	Warnings []string
}

// BlameHead reads all files from the attribution note attached to HEAD.
func BlameHead(repo *gitcmd.Repo) (BlameCollection, error) {
	head, note, err := headNote(repo)
	if err != nil {
		return BlameCollection{}, err
	}
	if len(note.Files) > maxBlameCollectionFiles {
		return BlameCollection{}, fmt.Errorf(
			"HEAD attribution note has %d files, limit is %d",
			len(note.Files),
			maxBlameCollectionFiles,
		)
	}
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	blobs, err := preflightBlameFiles(repo, head, paths, note)
	if err != nil {
		return BlameCollection{}, err
	}
	result := BlameCollection{Commit: head}
	for index, path := range paths {
		file, err := blameNotedFile(repo, head, path, blobs[index], note.Files[path])
		if err != nil {
			return BlameCollection{}, fmt.Errorf("blame %q: %w", path, err)
		}
		result.Files = append(result.Files, file)
	}
	return result, nil
}

// BlameHeadFile reads one file from the attribution note attached to HEAD.
func BlameHeadFile(repo *gitcmd.Repo, path string) (BlameResult, error) {
	path, err := repo.NormalizeWorktreePath(path)
	if err != nil {
		return BlameResult{}, err
	}
	head, note, err := headNote(repo)
	if err != nil {
		return BlameResult{}, err
	}
	file, found := note.Files[path]
	if !found {
		return BlameResult{}, fmt.Errorf("file %q is not attributed on HEAD", path)
	}
	blobs, err := preflightBlameFiles(repo, head, []string{path}, note)
	if err != nil {
		return BlameResult{}, err
	}
	result, err := blameNotedFile(repo, head, path, blobs[0], file)
	if err != nil {
		return BlameResult{}, fmt.Errorf("blame %q: %w", path, err)
	}
	return result, nil
}

func headNote(repo *gitcmd.Repo) (string, model.Note, error) {
	head, err := repo.Head()
	if err != nil {
		return "", model.Note{}, err
	}
	if head == "" {
		return "", model.Note{}, errors.New("cannot blame an unborn repository")
	}
	data, found, err := repo.ReadNote(head)
	if err != nil {
		return "", model.Note{}, err
	}
	if !found {
		return "", model.Note{}, errors.New("HEAD has no attribution note; run git-byline annotate")
	}
	note, err := notes.Decode(data)
	if err != nil {
		return "", model.Note{}, fmt.Errorf("decode HEAD attribution note: %w", err)
	}
	return head, note, nil
}

func preflightBlameFiles(repo *gitcmd.Repo, head string, paths []string, note model.Note) ([]string, error) {
	totalLines := 0
	var totalBytes int64
	blobs := make([]string, 0, len(paths))
	for _, path := range paths {
		file := note.Files[path]
		blob, exists, err := repo.BlobID(head, path)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", path, err)
		}
		if !exists {
			return nil, fmt.Errorf("attributed path %q is missing from HEAD", path)
		}
		if blob != file.Blob {
			return nil, fmt.Errorf("attribution blob for %q does not match HEAD", path)
		}
		blobs = append(blobs, blob)
		size, err := repo.BlobSize(blob)
		if err != nil {
			return nil, fmt.Errorf("inspect %q: %w", path, err)
		}
		totalBytes += size
		if totalBytes > maxBlameCollectionBytes {
			return nil, fmt.Errorf(
				"HEAD attribution content exceeds %d bytes",
				maxBlameCollectionBytes,
			)
		}
		if len(file.Ranges) > 0 {
			totalLines += file.Ranges[len(file.Ranges)-1].End
		}
		if totalLines > maxBlameCollectionLines {
			return nil, fmt.Errorf(
				"HEAD attribution has more than %d lines",
				maxBlameCollectionLines,
			)
		}
	}
	return blobs, nil
}

func blameNotedFile(repo *gitcmd.Repo, head, path, blob string, file model.NoteFile) (BlameResult, error) {
	normalized, err := repo.NormalizeWorktreePath(path)
	if err != nil {
		return BlameResult{}, err
	}
	if normalized != path {
		return BlameResult{}, fmt.Errorf("attribution path %q is not normalized", path)
	}
	content, err := repo.ReadBlob(blob)
	if err != nil {
		return BlameResult{}, err
	}
	snapshot, err := engine.NewSnapshot(content, file.Ranges)
	if err != nil {
		return BlameResult{}, err
	}
	return renderBlameResult(head, path, blob, snapshot, nil), nil
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
	return renderBlameResult(head, path, blob, snapshot, warnings), nil
}

func renderBlameResult(head, path, blob string, snapshot engine.Snapshot, warnings []string) BlameResult {
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
	}
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
