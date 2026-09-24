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

// DropCheckpointRecords atomically removes current-version records by
// sequence while preserving unknown versions and a truncated final line.
func (store Store) DropCheckpointRecords(sequences map[uint64]bool) (int, error) {
	if len(sequences) == 0 {
		return 0, nil
	}
	if _, _, err := store.ReadCheckpoints(); err != nil {
		return 0, err
	}
	data, err := readBoundedFile(store.CheckpointPath(), maxCheckpointBytes)
	if err != nil {
		return 0, fmt.Errorf("read checkpoint log for rewrite: %w", err)
	}
	reader := bufio.NewReaderSize(bytes.NewReader(data), checkpointBufferBytes)
	var kept bytes.Buffer
	dropped := 0
	for {
		raw, readErr := readCheckpointLine(reader)
		if len(raw) == 0 && errors.Is(readErr, io.EOF) {
			break
		}
		var header struct {
			Version int    `json:"version"`
			Seq     uint64 `json:"seq"`
		}
		if err := json.Unmarshal(bytes.TrimSuffix(raw, []byte{'\n'}), &header); err != nil {
			kept.Write(raw)
		} else if model.SupportedCheckpointVersion(header.Version) && sequences[header.Seq] {
			dropped++
		} else {
			kept.Write(raw)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	if dropped == 0 {
		return 0, nil
	}
	if err := writeAtomicFile(store.Dir, store.CheckpointPath(), "checkpoints-*.tmp", "checkpoint log", kept.Bytes()); err != nil {
		return 0, err
	}
	return dropped, nil
}

// RewriteCheckpointBases remaps supported checkpoint bases for one branch
// context while preserving unknown versions and a truncated final line.
func (store Store) RewriteCheckpointBases(branchRef string, bases map[string]string) error {
	if len(bases) == 0 {
		return nil
	}
	if err := model.ValidateBranchRef(branchRef); err != nil {
		return err
	}
	if err := validateCheckpointBaseTargets(bases); err != nil {
		return err
	}
	data, err := readBoundedFile(store.CheckpointPath(), maxCheckpointBytes)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read checkpoint log for base rewrite: %w", err)
	}
	rewritten, changed, err := rewriteCheckpointBaseData(data, baseRewrite(branchRef, bases))
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := writeAtomicFile(
		store.Dir,
		store.CheckpointPath(),
		"checkpoints-*.tmp",
		"checkpoint log",
		rewritten,
	); err != nil {
		return err
	}
	return nil
}

func baseRewrite(branchRef string, bases map[string]string) checkpointRewrite {
	return func(record model.Checkpoint) (model.Checkpoint, bool, error) {
		target, mapped := bases[record.BaseCommit]
		if !mapped || !checkpointMatchesRewriteBranch(record, branchRef) {
			return record, false, nil
		}
		if record.Version == model.CheckpointVersionV1 {
			record.Version = model.CheckpointVersion
			record.LaneID = model.LegacyCheckpointLaneID(record.BaseCommit)
		}
		record.BaseCommit = target
		return record, true, nil
	}
}

// CheckpointMove is the new base commit and lane of one checkpoint.
type CheckpointMove struct {
	BaseCommit string
	LaneID     string
}

// MoveCheckpoints rewrites the base commit and lane of the listed records.
// Every listed record must be a current-version record on branchRef with
// a branch lane, so splitting a lane never rewrites a legacy context.
func (store Store) MoveCheckpoints(branchRef string, moves map[uint64]CheckpointMove) error {
	if len(moves) == 0 {
		return nil
	}
	if err := model.ValidateBranchRef(branchRef); err != nil {
		return err
	}
	sequences := make([]uint64, 0, len(moves))
	for seq := range moves {
		sequences = append(sequences, seq)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	for _, seq := range sequences {
		move := moves[seq]
		if !model.ValidObjectID(move.BaseCommit) {
			return fmt.Errorf("move target for checkpoint %d is not a valid object ID", seq)
		}
		if err := model.ValidateCheckpointLaneID(move.LaneID); err != nil || model.IsLegacyCheckpointLaneID(move.LaneID) {
			return fmt.Errorf("move lane for checkpoint %d is not a branch lane", seq)
		}
	}
	data, err := readBoundedFile(store.CheckpointPath(), maxCheckpointBytes)
	if err != nil {
		return fmt.Errorf("read checkpoint log for move: %w", err)
	}
	found := make(map[uint64]bool, len(moves))
	rewritten, changed, err := rewriteCheckpointBaseData(data, func(record model.Checkpoint) (model.Checkpoint, bool, error) {
		move, listed := moves[record.Seq]
		if !listed {
			return record, false, nil
		}
		found[record.Seq] = true
		if record.Version != model.CheckpointVersion ||
			model.IsLegacyCheckpointLaneID(record.LaneID) ||
			record.BranchRef != branchRef {
			return record, false, fmt.Errorf("checkpoint %d is outside branch context %q", record.Seq, branchRef)
		}
		if record.BaseCommit == move.BaseCommit && record.LaneID == move.LaneID {
			return record, false, nil
		}
		record.BaseCommit = move.BaseCommit
		record.LaneID = move.LaneID
		return record, true, nil
	})
	if err != nil {
		return err
	}
	for _, seq := range sequences {
		if !found[seq] {
			return fmt.Errorf("checkpoint %d is missing from the checkpoint log", seq)
		}
	}
	if !changed {
		return nil
	}
	return writeAtomicFile(store.Dir, store.CheckpointPath(), "checkpoints-*.tmp", "checkpoint log", rewritten)
}

func validateCheckpointBaseTargets(bases map[string]string) error {
	invalidSource := ""
	for source, target := range bases {
		if target == "" || model.ValidObjectID(target) {
			continue
		}
		if invalidSource == "" || source < invalidSource {
			invalidSource = source
		}
	}
	if invalidSource == "" {
		return nil
	}
	return fmt.Errorf("rewrite target for base %q is not a valid object ID", invalidSource)
}

// checkpointRewrite returns the replacement for one valid supported record
// and whether it changed.
type checkpointRewrite func(model.Checkpoint) (model.Checkpoint, bool, error)

func rewriteCheckpointBaseData(data []byte, rewrite checkpointRewrite) ([]byte, bool, error) {
	reader := bufio.NewReaderSize(bytes.NewReader(data), checkpointBufferBytes)
	var rewritten bytes.Buffer
	changed := false
	line := 0
	var lastSequence uint64
	for {
		raw, readErr := readCheckpointLine(reader)
		if checkpointBaseDataDone(raw, readErr) {
			break
		}
		line++
		if err := validateCheckpointBaseRaw(raw, readErr, line); err != nil {
			return nil, false, err
		}
		next, err := rewriteCheckpointBaseLine(
			raw,
			errors.Is(readErr, io.EOF) && !bytes.HasSuffix(raw, []byte{'\n'}),
			rewrite,
		)
		if err != nil {
			return nil, false, err
		}
		if err := validateCheckpointBaseSequence(next, lastSequence, line); err != nil {
			return nil, false, err
		}
		if next.supported {
			lastSequence = next.sequence
		}
		rewritten.Write(next.data)
		changed = changed || next.changed
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return rewritten.Bytes(), changed, nil
}

func checkpointBaseDataDone(raw []byte, readErr error) bool {
	return len(raw) == 0 && errors.Is(readErr, io.EOF)
}

func validateCheckpointBaseRaw(raw []byte, readErr error, line int) error {
	if errors.Is(readErr, errCheckpointRecordLimit) ||
		len(bytes.TrimSuffix(raw, []byte{'\n'})) > maxRecordBytes {
		return fmt.Errorf("checkpoint line %d exceeds %d bytes", line, maxRecordBytes)
	}
	if line > maxCheckpointRecords {
		return fmt.Errorf("checkpoint log exceeds %d records", maxCheckpointRecords)
	}
	return nil
}

func validateCheckpointBaseSequence(
	line checkpointBaseRewriteLine,
	lastSequence uint64,
	lineNumber int,
) error {
	if line.supported && line.sequence <= lastSequence {
		return fmt.Errorf(
			"checkpoint line %d sequence %d is not increasing",
			lineNumber,
			line.sequence,
		)
	}
	return nil
}

type checkpointBaseRewriteLine struct {
	data      []byte
	changed   bool
	sequence  uint64
	supported bool
}

func rewriteCheckpointBaseLine(
	raw []byte,
	finalTruncated bool,
	rewrite checkpointRewrite,
) (checkpointBaseRewriteLine, error) {
	line := bytes.TrimSuffix(raw, []byte{'\n'})
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(line, &header); err != nil {
		if finalTruncated && isIncompleteJSON(err) {
			return checkpointBaseRewriteLine{data: raw}, nil
		}
		return checkpointBaseRewriteLine{}, fmt.Errorf("decode checkpoint header for base rewrite: %w", err)
	}
	if !model.SupportedCheckpointVersion(header.Version) {
		return checkpointBaseRewriteLine{data: raw}, nil
	}
	var record model.Checkpoint
	if err := decodeStrict(line, &record); err != nil {
		return checkpointBaseRewriteLine{}, fmt.Errorf("decode checkpoint for base rewrite: %w", err)
	}
	if err := validateCheckpoint(record); err != nil {
		return checkpointBaseRewriteLine{}, fmt.Errorf("validate checkpoint for base rewrite: %w", err)
	}
	result := checkpointBaseRewriteLine{
		data:      raw,
		sequence:  record.Seq,
		supported: true,
	}
	next, changed, err := rewrite(record)
	if err != nil {
		return checkpointBaseRewriteLine{}, err
	}
	if !changed {
		return result, nil
	}
	// Checkpoint contains only JSON-safe scalar and slice fields.
	encoded, _ := json.Marshal(next)
	if bytes.HasSuffix(raw, []byte{'\n'}) {
		encoded = append(encoded, '\n')
	}
	result.data = encoded
	result.changed = true
	return result, nil
}

func checkpointMatchesRewriteBranch(record model.Checkpoint, branchRef string) bool {
	return record.Version == model.CheckpointVersionV1 ||
		model.IsLegacyCheckpointLaneID(record.LaneID) ||
		record.BranchRef == branchRef
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
		if !model.SupportedCheckpointVersion(header.Version) {
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
	if !model.SupportedCheckpointVersion(record.Version) {
		return fmt.Errorf("unsupported checkpoint version %d", record.Version)
	}
	if record.Version == model.CheckpointVersionV1 && record.BranchRef != "" {
		return errors.New("version 1 checkpoint contains branch context")
	}
	if record.Version == model.CheckpointVersionV1 && record.LaneID != "" {
		return errors.New("version 1 checkpoint contains a lane ID")
	}
	if record.Version == model.CheckpointVersion {
		if err := model.ValidateCheckpointLaneID(record.LaneID); err != nil {
			return err
		}
		if model.IsLegacyCheckpointLaneID(record.LaneID) && record.BranchRef != "" {
			return errors.New("legacy checkpoint lane contains branch context")
		}
	}
	if record.BaseCommit != "" && !model.ValidObjectID(record.BaseCommit) {
		return errors.New("base commit is not a valid object ID")
	}
	if err := model.ValidateBranchRef(record.BranchRef); err != nil {
		return err
	}
	switch record.Kind {
	case model.CheckpointKindEdit, model.CheckpointKindShellPre, model.CheckpointKindShellPost:
	default:
		return fmt.Errorf("unsupported kind %q", record.Kind)
	}
	if record.Kind == model.CheckpointKindShellPre && record.Type != model.AuthorHuman {
		return errors.New("shell_pre checkpoint must be human")
	}
	if record.Kind == model.CheckpointKindShellPost && record.Type != model.AuthorAI {
		return errors.New("shell_post checkpoint must be ai")
	}
	if record.Kind != model.CheckpointKindEdit && len(record.Files) > 500 {
		return errors.New("shell checkpoint contains more than 500 files")
	}
	if record.Seq == 0 {
		return errors.New("sequence must be positive")
	}
	if err := model.ValidateEventID(record.EventID); err != nil {
		return err
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
	if len(record.Files) == 0 && record.Kind == model.CheckpointKindEdit {
		return errors.New("files are empty")
	}
	seen := map[string]bool{}
	for _, file := range record.Files {
		if file.Path == "" {
			return errors.New("file path is empty")
		}
		if hasGitPathComponent(file.Path) {
			return fmt.Errorf("file path %q is a Git administrative path", file.Path)
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
	if model.SupportedCheckpointVersion(header.Version) {
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

type stateWire struct {
	Version             int                `json:"version"`
	LastAnnotatedCommit string             `json:"last_annotated_commit,omitempty"`
	LastCheckpointSeq   uint64             `json:"last_checkpoint_seq"`
	NotesVersion        int                `json:"notes_version"`
	Pending             model.PendingState `json:"pending"`
	Lanes               json.RawMessage    `json:"lanes,omitempty"`
}

// ReadState reads state or returns a new empty state. Legacy states migrate
// deterministically in memory and persist on the next state write.
func (store Store) ReadState() (model.State, error) {
	state, _, err := store.ReadStateForUpdate()
	return state, err
}

// ReadStateForUpdate also reports whether callers must persist migration
// before writing data that older binaries cannot consume.
func (store Store) ReadStateForUpdate() (model.State, bool, error) {
	data, err := readBoundedFile(store.StatePath(), maxStateBytes)
	if errors.Is(err, os.ErrNotExist) {
		return model.NewState(), true, nil
	}
	if err != nil {
		return model.State{}, false, fmt.Errorf("read state: %w", err)
	}
	var wire stateWire
	if err := decodeStrict(data, &wire); err != nil {
		return model.State{}, false, fmt.Errorf("decode state: %w", err)
	}
	state := model.State{
		Version:             model.StateVersion,
		LastAnnotatedCommit: wire.LastAnnotatedCommit,
		LastCheckpointSeq:   wire.LastCheckpointSeq,
		NotesVersion:        wire.NotesVersion,
		Pending:             wire.Pending,
		Lanes:               map[string]map[string]uint64{},
	}
	migrated := wire.Version != model.StateVersion
	switch wire.Version {
	case model.StateVersionV1:
		legacy, err := decodeLegacyLanes(wire.Lanes)
		if err != nil {
			return model.State{}, false, fmt.Errorf("decode version 1 lanes: %w", err)
		}
		if len(legacy) != 0 {
			return model.State{}, false, errors.New("version 1 state contains lanes")
		}
	case model.StateVersionV2:
		legacy, err := decodeLegacyLanes(wire.Lanes)
		if err != nil {
			return model.State{}, false, fmt.Errorf("decode version 2 lanes: %w", err)
		}
		if len(legacy) > 0 {
			state.Lanes[""] = make(map[string]uint64, len(legacy))
			for base, consumed := range legacy {
				state.Lanes[""][model.LegacyCheckpointLaneID(base)] = consumed
			}
		}
	case model.StateVersion:
		if len(wire.Lanes) > 0 && string(wire.Lanes) != "null" {
			if err := decodeStrict(wire.Lanes, &state.Lanes); err != nil {
				return model.State{}, false, fmt.Errorf("decode state lanes: %w", err)
			}
		}
	default:
		return model.State{}, false, fmt.Errorf("unsupported state version %d", wire.Version)
	}
	switch state.NotesVersion {
	case model.NoteVersionV1, model.NoteVersionV2:
		state.NotesVersion = model.NoteVersion
	case model.NoteVersion:
	default:
		return model.State{}, false, fmt.Errorf("unsupported notes version %d", state.NotesVersion)
	}
	if state.Pending.Files == nil {
		state.Pending.Files = map[string]model.PendingFile{}
	}
	if err := validateState(state); err != nil {
		return model.State{}, false, fmt.Errorf("validate state: %w", err)
	}
	return state, migrated, nil
}

func decodeLegacyLanes(data json.RawMessage) (map[string]uint64, error) {
	lanes := map[string]uint64{}
	if len(data) == 0 || string(data) == "null" {
		return lanes, nil
	}
	if err := decodeStrict(data, &lanes); err != nil {
		return nil, err
	}
	return lanes, nil
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
	return writeAtomicFile(store.Dir, store.StatePath(), "state-*.tmp", "state", data)
}

func writeAtomicFile(dir, path, pattern, kind string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create store directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create %s temp file: %w", kind, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s temp file: %w", kind, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s temp file: %w", kind, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync %s temp file: %w", kind, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s temp file: %w", kind, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", kind, err)
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
	branchRefs := make([]string, 0, len(state.Lanes))
	for branchRef := range state.Lanes {
		branchRefs = append(branchRefs, branchRef)
	}
	sort.Strings(branchRefs)
	for _, branchRef := range branchRefs {
		if err := model.ValidateBranchRef(branchRef); err != nil {
			return fmt.Errorf("lane branch %q: %w", branchRef, err)
		}
		laneIDs := make([]string, 0, len(state.Lanes[branchRef]))
		for laneID := range state.Lanes[branchRef] {
			laneIDs = append(laneIDs, laneID)
		}
		sort.Strings(laneIDs)
		for _, laneID := range laneIDs {
			if err := model.ValidateCheckpointLaneID(laneID); err != nil {
				return fmt.Errorf("lane %q ID %q: %w", branchRef, laneID, err)
			}
			if model.IsLegacyCheckpointLaneID(laneID) && branchRef != "" {
				return fmt.Errorf("legacy lane %q has branch context %q", laneID, branchRef)
			}
			if state.Lanes[branchRef][laneID] == 0 {
				return fmt.Errorf("lane %q ID %q has consumed sequence 0", branchRef, laneID)
			}
		}
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
		if hasGitPathComponent(path) {
			return fmt.Errorf("pending file path %q is a Git administrative path", path)
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

func hasGitPathComponent(path string) bool {
	for _, component := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.EqualFold(component, ".git") {
			return true
		}
	}
	return false
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
