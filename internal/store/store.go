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
	"strings"
	"time"

	"github.com/comarch/git-byline/internal/model"
)

const (
	maxRecordBytes        = 8 << 20
	maxCheckpointBytes    = 64 << 20
	maxCheckpointRecords  = 100_000
	maxStateBytes         = 64 << 20
	checkpointBufferBytes = 64 << 10
)

var errCheckpointRecordLimit = errors.New("checkpoint record exceeds supported limit")

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
	file, err := os.Open(store.CheckpointPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read checkpoint log: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("stat checkpoint log: %w", err)
	}
	if info.Size() > maxCheckpointBytes {
		return nil, nil, fmt.Errorf("checkpoint log exceeds %d bytes", maxCheckpointBytes)
	}
	limited := &io.LimitedReader{R: file, N: maxCheckpointBytes + 1}
	reader := bufio.NewReaderSize(limited, checkpointBufferBytes)
	var records []model.Checkpoint
	var warnings []string
	line := 0
	for {
		raw, readErr := readCheckpointLine(reader)
		if len(raw) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		line++
		if line > maxCheckpointRecords {
			return nil, nil, fmt.Errorf("checkpoint log exceeds %d records", maxCheckpointRecords)
		}
		if errors.Is(readErr, errCheckpointRecordLimit) || len(bytes.TrimSuffix(raw, []byte{'\n'})) > maxRecordBytes {
			return nil, nil, fmt.Errorf("checkpoint line %d exceeds %d bytes", line, maxRecordBytes)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, nil, fmt.Errorf("read checkpoint line %d: %w", line, readErr)
		}
		finalTruncated := errors.Is(readErr, io.EOF) && !bytes.HasSuffix(raw, []byte{'\n'})
		raw = bytes.TrimSuffix(raw, []byte{'\n'})
		if len(bytes.TrimSpace(raw)) == 0 {
			return nil, nil, fmt.Errorf("checkpoint line %d is empty", line)
		}
		var header struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(raw, &header); err != nil {
			if finalTruncated && isIncompleteJSON(err) {
				warnings = append(warnings, fmt.Sprintf("ignored truncated final checkpoint line %d", line))
				break
			}
			return nil, nil, fmt.Errorf("decode checkpoint header line %d: %w", line, err)
		}
		if header.Version != model.CheckpointVersion {
			warnings = append(warnings, fmt.Sprintf("skipped checkpoint line %d with version %d", line, header.Version))
			continue
		}
		var record model.Checkpoint
		if err := decodeStrict(raw, &record); err != nil {
			if finalTruncated && isIncompleteJSON(err) {
				warnings = append(warnings, fmt.Sprintf("ignored truncated final checkpoint line %d", line))
				break
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
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if limited.N <= 0 {
		return nil, nil, fmt.Errorf("checkpoint log exceeds %d bytes", maxCheckpointBytes)
	}
	return records, warnings, nil
}

func readCheckpointLine(reader *bufio.Reader) ([]byte, error) {
	var result []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(result)+len(part) > maxRecordBytes+1 {
			return nil, errCheckpointRecordLimit
		}
		result = append(result, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return result, err
	}
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
	records, warnings, err := store.ReadCheckpoints()
	if err != nil {
		return err
	}
	data, err := store.checkCheckpointAppend(record, len(records)+len(warnings))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(store.Dir, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	_, _, _, err = inspectCheckpointTail(store.CheckpointPath(), true)
	if err != nil {
		return err
	}
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

// CheckCheckpointAppend validates an append without changing the log.
// existingRecords includes decoded and warned lines from ReadCheckpoints.
func (store Store) CheckCheckpointAppend(record model.Checkpoint, existingRecords int) error {
	_, err := store.checkCheckpointAppend(record, existingRecords)
	return err
}

func (store Store) checkCheckpointAppend(record model.Checkpoint, existingRecords int) ([]byte, error) {
	if err := validateCheckpoint(record); err != nil {
		return nil, fmt.Errorf("validate checkpoint: %w", err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode checkpoint: %w", err)
	}
	if len(data) > maxRecordBytes {
		return nil, fmt.Errorf("checkpoint record exceeds %d bytes", maxRecordBytes)
	}
	data = append(data, '\n')
	if existingRecords < 0 {
		return nil, errors.New("checkpoint record count is negative")
	}
	prefix, keptSize, droppedRecord, err := inspectCheckpointTail(store.CheckpointPath(), false)
	if err != nil {
		return nil, err
	}
	if droppedRecord && existingRecords > 0 {
		existingRecords--
	}
	if existingRecords >= maxCheckpointRecords {
		return nil, fmt.Errorf("checkpoint log would exceed %d records", maxCheckpointRecords)
	}
	data = append(prefix, data...)
	if keptSize+int64(len(data)) > maxCheckpointBytes {
		return nil, fmt.Errorf("checkpoint log would exceed %d bytes", maxCheckpointBytes)
	}
	return data, nil
}

func inspectCheckpointTail(path string, repair bool) ([]byte, int64, bool, error) {
	flag := os.O_RDONLY
	if repair {
		flag = os.O_RDWR
	}
	file, err := os.OpenFile(path, flag, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, fmt.Errorf("read checkpoint tail: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, false, fmt.Errorf("stat checkpoint log: %w", err)
	}
	if info.Size() > maxCheckpointBytes {
		return nil, 0, false, fmt.Errorf("checkpoint log exceeds %d bytes", maxCheckpointBytes)
	}
	if info.Size() == 0 {
		return nil, 0, false, nil
	}
	last := []byte{0}
	if _, err := file.ReadAt(last, info.Size()-1); err != nil {
		return nil, 0, false, fmt.Errorf("read checkpoint tail byte: %w", err)
	}
	if last[0] == '\n' {
		return nil, info.Size(), false, nil
	}
	readSize := info.Size()
	if readSize > maxRecordBytes+1 {
		readSize = maxRecordBytes + 1
	}
	data := make([]byte, int(readSize))
	if _, err := file.ReadAt(data, info.Size()-readSize); err != nil {
		return nil, 0, false, fmt.Errorf("read checkpoint tail: %w", err)
	}
	lastNewline := bytes.LastIndexByte(data, '\n')
	if lastNewline < 0 && info.Size() > readSize {
		return nil, 0, false, fmt.Errorf("checkpoint tail exceeds %d bytes", maxRecordBytes)
	}
	start := info.Size() - readSize + int64(lastNewline+1)
	tail := data[lastNewline+1:]
	if len(tail) > maxRecordBytes {
		return nil, 0, false, fmt.Errorf("checkpoint tail exceeds %d bytes", maxRecordBytes)
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(tail, &header); err != nil {
		if !isIncompleteJSON(err) {
			return nil, 0, false, fmt.Errorf("decode checkpoint tail header: %w", err)
		}
		if repair {
			if err := file.Truncate(start); err != nil {
				return nil, 0, false, fmt.Errorf("truncate checkpoint tail: %w", err)
			}
		}
		return nil, start, true, nil
	}
	if header.Version == model.CheckpointVersion {
		var record model.Checkpoint
		if err := decodeStrict(tail, &record); err != nil {
			return nil, 0, false, fmt.Errorf("decode checkpoint tail: %w", err)
		}
		if err := validateCheckpoint(record); err != nil {
			return nil, 0, false, fmt.Errorf("validate checkpoint tail: %w", err)
		}
	}
	return []byte{'\n'}, info.Size(), false, nil
}

func isIncompleteJSON(err error) bool {
	var syntax *json.SyntaxError
	return errors.As(err, &syntax) && strings.Contains(syntax.Error(), "unexpected end of JSON input")
}

// ReadState reads state or returns a new empty state.
func (store Store) ReadState() (model.State, error) {
	data, err := readBoundedFile(store.StatePath(), maxStateBytes)
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

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	reader := &io.LimitedReader{R: file, N: limit + 1}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if reader.N <= 0 {
		return nil, fmt.Errorf("file exceeds %d bytes", limit)
	}
	return data, nil
}

// WriteState atomically replaces state.
func (store Store) WriteState(state model.State) error {
	data, err := encodeState(state)
	if err != nil {
		return err
	}
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

// CheckStateWrite validates a state write without changing the state file.
func (store Store) CheckStateWrite(state model.State) error {
	_, err := encodeState(state)
	return err
}

func encodeState(state model.State) ([]byte, error) {
	if state.Pending.Files == nil {
		state.Pending.Files = map[string]model.PendingFile{}
	}
	if err := validateState(state); err != nil {
		return nil, fmt.Errorf("validate state: %w", err)
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode state: %w", err)
	}
	data = append(data, '\n')
	if len(data) > maxStateBytes {
		return nil, fmt.Errorf("state exceeds %d bytes", maxStateBytes)
	}
	return data, nil
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
