// Package store persists checkpoint and state records in the Git directory.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/comarch/git-byline/internal/model"
)

const maxRecordBytes = 8 << 20

// Store addresses worktree-specific git-byline data.
type Store struct {
	Dir string
}

// New creates a store rooted in the worktree-specific Git directory.
func New(gitDir string) Store {
	return Store{Dir: filepath.Join(gitDir, "byline")}
}

// CheckpointPath returns the append-only log path.
func (store Store) CheckpointPath() string {
	return filepath.Join(store.Dir, "checkpoints.jsonl")
}

// StatePath returns the state file path.
func (store Store) StatePath() string {
	return filepath.Join(store.Dir, "state.json")
}

// LockPath returns the worktree operation lock path.
func (store Store) LockPath() string {
	return filepath.Join(store.Dir, "operation.lock")
}

// ReadCheckpoints reads supported records and skips unknown versions.
func (store Store) ReadCheckpoints() ([]model.Checkpoint, []string, error) {
	data, err := os.ReadFile(store.CheckpointPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read checkpoint log: %w", err)
	}
	hasFinalNewline := len(data) == 0 || data[len(data)-1] == '\n'
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), maxRecordBytes)
	var records []model.Checkpoint
	var warnings []string
	line := 0
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, nil, fmt.Errorf("checkpoint line %d is empty", line)
		}
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			if !hasFinalNewline && scannerAtEnd(scanner, data, line) {
				warnings = append(warnings, fmt.Sprintf("ignored truncated final checkpoint line %d", line))
				continue
			}
			return nil, nil, fmt.Errorf("decode checkpoint header line %d: %w", line, err)
		}
		if header.Version != model.CheckpointVersion {
			warnings = append(warnings, fmt.Sprintf("skipped checkpoint line %d with version %d", line, header.Version))
			continue
		}
		var record model.Checkpoint
		if err := decodeStrict(raw, &record); err != nil {
			if !hasFinalNewline && scannerAtEnd(scanner, data, line) {
				warnings = append(warnings, fmt.Sprintf("ignored truncated final checkpoint line %d", line))
				continue
			}
			return nil, nil, fmt.Errorf("decode checkpoint line %d: %w", line, err)
		}
		if err := validateCheckpoint(record); err != nil {
			return nil, nil, fmt.Errorf("checkpoint line %d: %w", line, err)
		}
		if len(records) > 0 && record.Seq <= records[len(records)-1].Seq {
			return nil, nil, fmt.Errorf("checkpoint line %d sequence %d is not increasing", line, record.Seq)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan checkpoint log: %w", err)
	}
	return records, warnings, nil
}

func scannerAtEnd(_ *bufio.Scanner, data []byte, line int) bool {
	return line == bytes.Count(data, []byte{'\n'})+1
}

func validateCheckpoint(record model.Checkpoint) error {
	if record.Kind != "edit" {
		return fmt.Errorf("unsupported kind %q", record.Kind)
	}
	if record.Seq == 0 {
		return errors.New("sequence must be positive")
	}
	attr := model.Attribution{
		Author:  record.Type,
		Agent:   record.Agent,
		Model:   record.Model,
		Session: record.Session,
		TS:      record.TS,
	}
	if record.Type != model.AuthorAI {
		attr.TS = ""
	}
	if err := model.ValidateAttribution(attr); err != nil {
		return err
	}
	if record.TS == "" {
		return errors.New("timestamp is empty")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.TS); err != nil {
		return fmt.Errorf("timestamp is not RFC3339: %w", err)
	}
	if len(record.Files) == 0 {
		return errors.New("files are empty")
	}
	seen := map[string]bool{}
	for _, file := range record.Files {
		if file.Path == "" {
			return errors.New("file path is empty")
		}
		if seen[file.Path] {
			return fmt.Errorf("duplicate file path %q", file.Path)
		}
		seen[file.Path] = true
		if file.Exists && !model.ValidObjectID(file.Blob) {
			return fmt.Errorf("file %q has invalid blob", file.Path)
		}
		if !file.Exists && file.Blob != "" {
			return fmt.Errorf("missing file %q has a blob", file.Path)
		}
	}
	return nil
}

// AppendCheckpoint durably appends one record.
func (store Store) AppendCheckpoint(record model.Checkpoint) error {
	if err := validateCheckpoint(record); err != nil {
		return fmt.Errorf("validate checkpoint: %w", err)
	}
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode checkpoint: %w", err)
	}
	data = append(data, '\n')
	prefix, err := repairCheckpointTail(store.CheckpointPath())
	if err != nil {
		return err
	}
	data = append(prefix, data...)
	file, err := os.OpenFile(store.CheckpointPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open checkpoint log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("set checkpoint log permissions: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("append checkpoint: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync checkpoint log: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close checkpoint log: %w", err)
	}
	return nil
}

func repairCheckpointTail(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read checkpoint tail: %w", err)
	}
	if len(data) == 0 || data[len(data)-1] == '\n' {
		return nil, nil
	}
	start := bytes.LastIndexByte(data, '\n') + 1
	tail := data[start:]
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(tail, &header); err != nil {
		if err := os.Truncate(path, int64(start)); err != nil {
			return nil, fmt.Errorf("truncate checkpoint tail: %w", err)
		}
		return nil, nil
	}
	if header.Version == model.CheckpointVersion {
		var record model.Checkpoint
		if err := decodeStrict(tail, &record); err != nil {
			return nil, fmt.Errorf("decode checkpoint tail: %w", err)
		}
		if err := validateCheckpoint(record); err != nil {
			return nil, fmt.Errorf("validate checkpoint tail: %w", err)
		}
	}
	return []byte{'\n'}, nil
}

// ReadState reads state or returns a new empty state.
func (store Store) ReadState() (model.State, error) {
	data, err := os.ReadFile(store.StatePath())
	if errors.Is(err, os.ErrNotExist) {
		return model.NewState(), nil
	}
	if err != nil {
		return model.State{}, fmt.Errorf("read state: %w", err)
	}
	var state model.State
	if err := decodeStrict(data, &state); err != nil {
		return model.State{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Version != model.StateVersion {
		return model.State{}, fmt.Errorf("unsupported state version %d", state.Version)
	}
	if state.NotesVersion != model.NoteVersion {
		return model.State{}, fmt.Errorf("unsupported notes version %d", state.NotesVersion)
	}
	if state.Pending.Files == nil {
		state.Pending.Files = map[string]model.PendingFile{}
	}
	if err := validateState(state); err != nil {
		return model.State{}, fmt.Errorf("validate state: %w", err)
	}
	return state, nil
}

// WriteState atomically replaces state.
func (store Store) WriteState(state model.State) error {
	if state.Pending.Files == nil {
		state.Pending.Files = map[string]model.PendingFile{}
	}
	if err := validateState(state); err != nil {
		return fmt.Errorf("validate state: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	tmp, err := os.CreateTemp(store.Dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("create state temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod state temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write state temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync state temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state temp file: %w", err)
	}
	if err := os.Rename(tmpName, store.StatePath()); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func validateState(state model.State) error {
	if state.Version != model.StateVersion || state.NotesVersion != model.NoteVersion {
		return errors.New("invalid state version")
	}
	if state.LastAnnotatedCommit != "" && !model.ValidObjectID(state.LastAnnotatedCommit) {
		return errors.New("last annotated commit is invalid")
	}
	if state.Pending.BaseCommit != "" && !model.ValidObjectID(state.Pending.BaseCommit) {
		return errors.New("pending base commit is invalid")
	}
	if state.Pending.BaseCommit != state.LastAnnotatedCommit {
		return errors.New("pending base commit differs from last annotated commit")
	}
	paths := make([]string, 0, len(state.Pending.Files))
	for path := range state.Pending.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := state.Pending.Files[path]
		if path == "" {
			return errors.New("pending file path is empty")
		}
		if !model.ValidObjectID(file.Blob) {
			return fmt.Errorf("pending file %q has invalid blob", path)
		}
		lineCount := 0
		if len(file.Ranges) > 0 {
			lineCount = file.Ranges[len(file.Ranges)-1].End
		}
		if err := model.ValidateRanges(file.Ranges, lineCount); err != nil {
			return fmt.Errorf("pending file %q: %w", path, err)
		}
	}
	return nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}
