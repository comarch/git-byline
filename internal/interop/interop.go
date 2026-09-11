// Package interop converts git-byline notes to and from external attribution
// formats.
package interop

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/engine"
	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/store"
)

const (
	// GitAINotesRef is the note namespace defined by Git AI Standard v3.
	GitAINotesRef = "refs/notes/ai"

	maxGitAINoteBytes = 16 << 20
	maxImportCommits  = 10_000
	maxGitAIFiles     = 500
	maxGitAIEntries   = 100_000
	maxGitAIString    = 1024

	syntheticHumanAuthor = "git-byline"
)

// GitAILineRange is an inclusive, one-based range in a Git AI note.
type GitAILineRange struct {
	Start int
	End   int
}

// GitAIAttestation associates a Git AI key with line ranges.
type GitAIAttestation struct {
	Key    string
	Ranges []GitAILineRange
}

// GitAIAgentID identifies the tool and conversation recorded by Git AI.
type GitAIAgentID struct {
	Tool  string
	ID    string
	Model string
}

// GitAISession is a Git AI session metadata record.
type GitAISession struct {
	AgentID GitAIAgentID
}

// GitAIHuman is a known-human metadata record.
type GitAIHuman struct {
	Author string
}

// GitAIPrompt is a legacy Git AI prompt metadata record.
type GitAIPrompt struct {
	AgentID        GitAIAgentID
	TotalAdditions int
	TotalDeletions int
	AcceptedLines  int
	OverridenLines int
}

// GitAINote is the parsed form of a Git AI Standard v3 note.
type GitAINote struct {
	SchemaVersion string
	BaseCommit    string
	Files         map[string][]GitAIAttestation
	Sessions      map[string]GitAISession
	Humans        map[string]GitAIHuman
	Prompts       map[string]GitAIPrompt
}

// AgentTraceRecord is the Agent Trace 0.1.0 wire format.
type AgentTraceRecord struct {
	Version   string                        `json:"version"`
	ID        string                        `json:"id"`
	Timestamp string                        `json:"timestamp"`
	VCS       AgentTraceVCS                 `json:"vcs"`
	Tool      AgentTraceTool                `json:"tool"`
	Files     []AgentTraceFile              `json:"files"`
	Metadata  map[string][]TraceAttribution `json:"metadata,omitempty"`
}

// AgentTraceVCS identifies the revision described by a trace.
type AgentTraceVCS struct {
	Type     string `json:"type"`
	Revision string `json:"revision"`
}

// AgentTraceTool identifies the writer.
type AgentTraceTool struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

// AgentTraceFile contains conversations for one repository-relative path.
type AgentTraceFile struct {
	Path          string                   `json:"path"`
	Conversations []AgentTraceConversation `json:"conversations"`
}

// AgentTraceConversation contains one contributor's line ranges.
type AgentTraceConversation struct {
	Contributor AgentTraceContributor `json:"contributor,omitempty"`
	Ranges      []AgentTraceRange     `json:"ranges"`
}

// AgentTraceContributor identifies the source of conversation ranges.
type AgentTraceContributor struct {
	Type    string `json:"type"`
	ModelID string `json:"model_id,omitempty"`
}

// AgentTraceRange is an inclusive, one-based Agent Trace range.
type AgentTraceRange struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// TraceAttribution preserves local agent and session metadata in the
// extensible Agent Trace metadata object.
type TraceAttribution struct {
	File         string `json:"file"`
	Conversation int    `json:"conversation"`
	Agent        string `json:"agent,omitempty"`
	Session      string `json:"session,omitempty"`
}

// ImportResult reports Git AI notes considered by an import.
type ImportResult struct {
	Imported int
	Skipped  int
	Warnings []string
}

// Export returns one supported external representation for commit.
func Export(repo *gitcmd.Repo, format, commit string) ([]byte, error) {
	if repo == nil {
		return nil, errors.New("interop repository is nil")
	}
	switch format {
	case "gitai":
		return ExportGitAI(repo, commit)
	case "agent-trace":
		return ExportAgentTrace(repo, commit)
	default:
		return nil, fmt.Errorf("unsupported export format %q", format)
	}
}

// ExportGitAI writes a deterministic Git AI Standard v3 note payload from the
// byline note attached to commit.
func ExportGitAI(repo *gitcmd.Repo, commit string) ([]byte, error) {
	commit, err := resolveCommit(repo, commit)
	if err != nil {
		return nil, err
	}
	note, checkpoints, err := readByline(repo, commit)
	if err != nil {
		return nil, err
	}
	index := newCheckpointIndex(checkpoints)
	wire := gitAIWireMetadata{
		SchemaVersion: "authorship/3.0.0",
		BaseCommitSHA: commit,
		Prompts:       map[string]gitAIPromptWire{},
		Sessions:      map[string]gitAISessionWire{},
		Humans:        map[string]gitAIHumanWire{},
	}
	var attestations strings.Builder
	paths := sortedNotePaths(note)
	for _, path := range paths {
		file := note.Files[path]
		entries, sessionRecords, humanRecords, err := gitAIEntries(
			path,
			file.Ranges,
			index,
		)
		if err != nil {
			return nil, fmt.Errorf("encode Git AI file %q: %w", path, err)
		}
		if len(entries) == 0 {
			continue
		}
		writeGitAIPath(&attestations, path)
		for _, entry := range entries {
			fmt.Fprintf(&attestations, "  %s %s\n", entry.key, formatLineRanges(entry.ranges))
		}
		for key, value := range sessionRecords {
			if previous, ok := wire.Sessions[key]; ok && !sameSessionWire(previous, value) {
				return nil, fmt.Errorf("session %q has conflicting metadata", key)
			}
			wire.Sessions[key] = value
		}
		for key, value := range humanRecords {
			if previous, ok := wire.Humans[key]; ok && previous != value {
				return nil, fmt.Errorf("human %q has conflicting metadata", key)
			}
			wire.Humans[key] = value
		}
	}
	metadata, err := json.Marshal(wire)
	if err != nil {
		return nil, fmt.Errorf("encode Git AI metadata: %w", err)
	}
	attestations.WriteString("---\n")
	attestations.Write(metadata)
	attestations.WriteByte('\n')
	return []byte(attestations.String()), nil
}

// EncodeGitAI is an alias for ExportGitAI.
func EncodeGitAI(repo *gitcmd.Repo, commit string) ([]byte, error) {
	return ExportGitAI(repo, commit)
}

// ReadGitAINote reads and parses a Git AI note attached to commit.
func ReadGitAINote(repo *gitcmd.Repo, commit string) (GitAINote, bool, error) {
	if repo == nil {
		return GitAINote{}, false, errors.New("interop repository is nil")
	}
	commit, err := resolveCommit(repo, commit)
	if err != nil {
		return GitAINote{}, false, err
	}
	data, found, err := repo.ReadNoteRef(GitAINotesRef, commit)
	if err != nil {
		return GitAINote{}, false, fmt.Errorf("read Git AI note: %w", err)
	}
	if !found {
		return GitAINote{}, false, nil
	}
	note, err := DecodeGitAI(data)
	if err != nil {
		return GitAINote{}, false, err
	}
	return note, true, nil
}

// WriteGitAINote writes the Git AI representation to refs/notes/ai.
func WriteGitAINote(repo *gitcmd.Repo, commit string) error {
	if repo == nil {
		return errors.New("interop repository is nil")
	}
	commit, err := resolveCommit(repo, commit)
	if err != nil {
		return err
	}
	data, err := ExportGitAI(repo, commit)
	if err != nil {
		return err
	}
	if err := repo.WriteNoteRef(GitAINotesRef, commit, data); err != nil {
		return fmt.Errorf("write Git AI note: %w", err)
	}
	return nil
}

// ExportAgentTrace writes a deterministic Agent Trace 0.1.0 record from the
// byline note attached to commit.
func ExportAgentTrace(repo *gitcmd.Repo, commit string) ([]byte, error) {
	commit, err := resolveCommit(repo, commit)
	if err != nil {
		return nil, err
	}
	note, _, err := readByline(repo, commit)
	if err != nil {
		return nil, err
	}
	timestamp, err := repo.CommitTime(commit)
	if err != nil {
		return nil, fmt.Errorf("read trace timestamp: %w", err)
	}
	record := AgentTraceRecord{
		Version:   "0.1.0",
		ID:        traceUUID(commit),
		Timestamp: timestamp,
		VCS:       AgentTraceVCS{Type: "git", Revision: commit},
		Tool:      AgentTraceTool{Name: "git-byline"},
		Files:     make([]AgentTraceFile, 0, len(note.Files)),
	}
	paths := sortedNotePaths(note)
	for _, path := range paths {
		conversations, sources, err := traceConversations(path, note.Files[path].Ranges)
		if err != nil {
			return nil, fmt.Errorf("encode Agent Trace file %q: %w", path, err)
		}
		if len(conversations) == 0 {
			continue
		}
		file := AgentTraceFile{Path: path, Conversations: conversations}
		record.Files = append(record.Files, file)
		for _, source := range sources {
			record.Metadata = appendTraceMetadata(record.Metadata, source)
		}
	}
	if len(record.Metadata) == 0 {
		record.Metadata = nil
	}
	data, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("encode Agent Trace: %w", err)
	}
	return append(data, '\n'), nil
}

// EncodeAgentTrace is an alias for ExportAgentTrace.
func EncodeAgentTrace(repo *gitcmd.Repo, commit string) ([]byte, error) {
	return ExportAgentTrace(repo, commit)
}

// DecodeGitAI parses and validates a Git AI Standard v3 note.
func DecodeGitAI(data []byte) (GitAINote, error) {
	if len(data) > maxGitAINoteBytes {
		return GitAINote{}, fmt.Errorf("Git AI note exceeds %d bytes", maxGitAINoteBytes)
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	divider := -1
	for index, line := range lines {
		if line == "---" {
			divider = index
			break
		}
	}
	if divider < 0 {
		return GitAINote{}, errors.New("Git AI note is missing the --- divider")
	}
	metadataText := strings.TrimSpace(strings.Join(lines[divider+1:], "\n"))
	if metadataText == "" {
		return GitAINote{}, errors.New("Git AI note metadata is empty")
	}
	metadata, err := decodeGitAIMetadata([]byte(metadataText))
	if err != nil {
		return GitAINote{}, err
	}
	result := GitAINote{
		SchemaVersion: metadata.SchemaVersion,
		BaseCommit:    metadata.BaseCommitSHA,
		Files:         map[string][]GitAIAttestation{},
		Sessions:      map[string]GitAISession{},
		Humans:        map[string]GitAIHuman{},
		Prompts:       map[string]GitAIPrompt{},
	}
	if metadata.SchemaVersion != "authorship/3.0.0" {
		return GitAINote{}, fmt.Errorf("unsupported Git AI schema version %q", metadata.SchemaVersion)
	}
	if !validObjectID(result.BaseCommit) {
		return GitAINote{}, errors.New("Git AI base_commit_sha is invalid")
	}
	for key, value := range metadata.Sessions {
		if !validSessionID(key) {
			return GitAINote{}, fmt.Errorf("invalid Git AI session %q", key)
		}
		if err := validateGitAIMetadata(value.AgentID, value.HumanAuthor, value.CustomAttributes); err != nil {
			return GitAINote{}, fmt.Errorf("invalid Git AI session %q: %w", key, err)
		}
		result.Sessions[key] = GitAISession{AgentID: GitAIAgentID{
			Tool: value.AgentID.Tool, ID: value.AgentID.ID, Model: value.AgentID.Model,
		}}
	}
	for key, value := range metadata.Humans {
		if !validHumanID(key) || !validGitAIString(value.Author, true) {
			return GitAINote{}, fmt.Errorf("invalid Git AI human %q", key)
		}
		result.Humans[key] = GitAIHuman{Author: value.Author}
	}
	for key, value := range metadata.Prompts {
		if !validLegacyID(key) {
			return GitAINote{}, fmt.Errorf("invalid Git AI prompt %q", key)
		}
		if value.TotalAdditions == nil || value.TotalDeletions == nil ||
			value.AcceptedLines == nil || value.OverridenLines == nil {
			return GitAINote{}, fmt.Errorf("invalid Git AI prompt %q: missing counters", key)
		}
		if *value.TotalAdditions < 0 || *value.TotalDeletions < 0 ||
			*value.AcceptedLines < 0 || *value.OverridenLines < 0 {
			return GitAINote{}, fmt.Errorf("invalid Git AI prompt %q: counters must be nonnegative", key)
		}
		if err := validateGitAIMetadata(value.AgentID, value.HumanAuthor, value.CustomAttributes); err != nil {
			return GitAINote{}, fmt.Errorf("invalid Git AI prompt %q: %w", key, err)
		}
		if !validGitAIString(value.MessagesURL, false) {
			return GitAINote{}, fmt.Errorf("invalid Git AI prompt %q: invalid metadata string", key)
		}
		result.Prompts[key] = GitAIPrompt{
			AgentID: GitAIAgentID{
				Tool: value.AgentID.Tool, ID: value.AgentID.ID, Model: value.AgentID.Model,
			},
			TotalAdditions: *value.TotalAdditions,
			TotalDeletions: *value.TotalDeletions,
			AcceptedLines:  *value.AcceptedLines,
			OverridenLines: *value.OverridenLines,
		}
	}
	var currentPath string
	seenPaths := map[string]bool{}
	attestationCount := 0
	for index, line := range lines[:divider] {
		if line == "" {
			return GitAINote{}, fmt.Errorf("Git AI attestation line %d is empty", index+1)
		}
		if strings.HasPrefix(line, "  ") {
			if len(line) > 2 && line[2] == ' ' {
				return GitAINote{}, fmt.Errorf("Git AI attestation line %d has invalid indentation", index+1)
			}
			if currentPath == "" {
				return GitAINote{}, fmt.Errorf("Git AI attestation line %d has no file", index+1)
			}
			fields := strings.Fields(line)
			if len(fields) != 2 {
				return GitAINote{}, fmt.Errorf("Git AI attestation line %d is invalid", index+1)
			}
			ranges, err := parseLineRanges(fields[1])
			if err != nil {
				return GitAINote{}, fmt.Errorf("Git AI attestation line %d: %w", index+1, err)
			}
			if err := validateGitAIKey(fields[0], result); err != nil {
				return GitAINote{}, fmt.Errorf("Git AI attestation line %d: %w", index+1, err)
			}
			result.Files[currentPath] = append(result.Files[currentPath], GitAIAttestation{
				Key: fields[0], Ranges: ranges,
			})
			attestationCount++
			if attestationCount > maxGitAIEntries {
				return GitAINote{}, fmt.Errorf("Git AI note has more than %d attestations", maxGitAIEntries)
			}
			continue
		}
		if strings.HasPrefix(line, " ") {
			return GitAINote{}, fmt.Errorf("Git AI attestation line %d has invalid indentation", index+1)
		}
		currentPath, err = parseGitAIPath(line)
		if err != nil {
			return GitAINote{}, fmt.Errorf("Git AI attestation line %d: %w", index+1, err)
		}
		if seenPaths[currentPath] {
			return GitAINote{}, fmt.Errorf("Git AI file %q is repeated", currentPath)
		}
		seenPaths[currentPath] = true
		if len(seenPaths) > maxGitAIFiles {
			return GitAINote{}, fmt.Errorf("Git AI note has more than %d files", maxGitAIFiles)
		}
		if _, exists := result.Files[currentPath]; !exists {
			result.Files[currentPath] = nil
		}
	}
	for path, entries := range result.Files {
		if len(entries) == 0 {
			return GitAINote{}, fmt.Errorf("Git AI file %q has no attestations", path)
		}
		if err := validateAttestationRanges(entries); err != nil {
			return GitAINote{}, fmt.Errorf("Git AI file %q: %w", path, err)
		}
	}
	if metadata.Prompts == nil {
		return GitAINote{}, errors.New("Git AI metadata is missing prompts")
	}
	return result, nil
}

// ToBylineNote resolves a parsed Git AI note against commit content.
func (note GitAINote) ToBylineNote(repo *gitcmd.Repo, commit string) (model.Note, error) {
	if repo == nil {
		return model.Note{}, errors.New("interop repository is nil")
	}
	if !validObjectID(commit) || note.BaseCommit != commit {
		return model.Note{}, errors.New("Git AI base commit does not match imported commit")
	}
	result := model.Note{Version: model.NoteVersion, Files: map[string]model.NoteFile{}}
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sortStrings(paths)
	for _, path := range paths {
		blob, exists, err := repo.BlobID(commit, path)
		if err != nil {
			return model.Note{}, fmt.Errorf("read Git AI file %q: %w", path, err)
		}
		if !exists {
			return model.Note{}, fmt.Errorf("Git AI file %q is missing from commit", path)
		}
		content, err := repo.ReadBlob(blob)
		if err != nil {
			return model.Note{}, fmt.Errorf("read Git AI file %q: %w", path, err)
		}
		lines, err := engine.SplitLines(content)
		if err != nil {
			return model.Note{}, fmt.Errorf("read Git AI file %q: %w", path, err)
		}
		owners := make([]model.Attribution, len(lines))
		for index := range owners {
			owners[index] = model.Attribution{Author: model.AuthorUntracked}
		}
		covered := make([]bool, len(lines))
		for _, entry := range note.Files[path] {
			attribution, err := note.attribution(entry.Key)
			if err != nil {
				return model.Note{}, fmt.Errorf("Git AI file %q: %w", path, err)
			}
			for _, value := range entry.Ranges {
				if value.Start > len(lines) || value.End > len(lines) {
					return model.Note{}, fmt.Errorf(
						"Git AI file %q range %d-%d exceeds %d lines",
						path, value.Start, value.End, len(lines),
					)
				}
				for line := value.Start - 1; line < value.End; line++ {
					if covered[line] {
						return model.Note{}, fmt.Errorf("Git AI file %q has overlapping attestations", path)
					}
					covered[line] = true
					owners[line] = attribution
				}
			}
		}
		ranges := rangesFromAttributions(owners)
		if err := model.ValidateRanges(ranges, len(lines)); err != nil {
			return model.Note{}, fmt.Errorf("Git AI file %q: %w", path, err)
		}
		result.Files[path] = model.NoteFile{Blob: blob, Ranges: ranges}
	}
	return result, nil
}

// ImportGitAI imports notes from refs/notes/ai into refs/notes/byline.
func ImportGitAI(repo *gitcmd.Repo, revisionRange string, dryRun bool) (ImportResult, error) {
	if repo == nil {
		return ImportResult{}, errors.New("interop repository is nil")
	}
	commits, err := importCommits(repo, revisionRange)
	if err != nil {
		return ImportResult{}, err
	}
	result := ImportResult{}
	for _, commit := range commits {
		data, found, err := repo.ReadNoteRef(GitAINotesRef, commit)
		if err != nil {
			return result, fmt.Errorf("read Git AI note on %s: %w", commit, err)
		}
		if !found {
			continue
		}
		parsed, err := DecodeGitAI(data)
		if err != nil {
			result.Skipped++
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped Git AI note on %s: %v", commit, err))
			continue
		}
		note, err := parsed.ToBylineNote(repo, commit)
		if err != nil {
			result.Skipped++
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped Git AI note on %s: %v", commit, err))
			continue
		}
		encoded, err := notes.Encode(note)
		if err != nil {
			return result, fmt.Errorf("encode byline note for %s: %w", commit, err)
		}
		existing, exists, err := repo.ReadNoteRef("refs/notes/byline", commit)
		if err != nil {
			return result, fmt.Errorf("read byline note on %s: %w", commit, err)
		}
		if exists {
			if !sameNoteBytes(existing, encoded) {
				result.Skipped++
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("skipped %s: existing byline note is different", commit))
			}
			continue
		}
		if !dryRun {
			if err := repo.WriteNoteRef("refs/notes/byline", commit, encoded); err != nil {
				if isDifferentNoteConflict(err) {
					result.Skipped++
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("skipped %s: existing byline note is different", commit))
					continue
				}
				return result, fmt.Errorf("write byline note on %s: %w", commit, err)
			}
		}
		result.Imported++
	}
	return result, nil
}

func readByline(repo *gitcmd.Repo, commit string) (model.Note, []model.Checkpoint, error) {
	if repo == nil {
		return model.Note{}, nil, errors.New("interop repository is nil")
	}
	if !validObjectID(commit) {
		return model.Note{}, nil, errors.New("invalid commit object ID")
	}
	data, found, err := repo.ReadNoteRef("refs/notes/byline", commit)
	if err != nil {
		return model.Note{}, nil, fmt.Errorf("read byline note: %w", err)
	}
	if !found {
		return model.Note{}, nil, fmt.Errorf("commit %s has no byline note", commit)
	}
	note, err := notes.Decode(data)
	if err != nil {
		return model.Note{}, nil, fmt.Errorf("decode byline note: %w", err)
	}
	for path, file := range note.Files {
		blob, exists, err := repo.BlobID(commit, path)
		if err != nil {
			return model.Note{}, nil, fmt.Errorf("inspect byline file %q: %w", path, err)
		}
		if !exists || blob != file.Blob {
			return model.Note{}, nil, fmt.Errorf("byline blob for %q does not match commit", path)
		}
		content, err := repo.ReadBlob(blob)
		if err != nil {
			return model.Note{}, nil, fmt.Errorf("read byline file %q: %w", path, err)
		}
		lines, err := engine.SplitLines(content)
		if err != nil {
			return model.Note{}, nil, fmt.Errorf("read byline file %q: %w", path, err)
		}
		if err := model.ValidateRanges(file.Ranges, len(lines)); err != nil {
			return model.Note{}, nil, fmt.Errorf("byline file %q: %w", path, err)
		}
	}
	checkpoints, warnings, err := store.New(repo.GitDir).ReadCheckpoints()
	if err != nil {
		return model.Note{}, nil, fmt.Errorf("read checkpoints: %w", err)
	}
	_ = warnings
	return note, checkpoints, nil
}

func resolveCommit(repo *gitcmd.Repo, revision string) (string, error) {
	if repo == nil {
		return "", errors.New("interop repository is nil")
	}
	if revision == "" {
		return "", errors.New("commit revision is empty")
	}
	commits, err := repo.RevList("", revision, 1)
	if err != nil {
		return "", fmt.Errorf("resolve commit %q: %w", revision, err)
	}
	if len(commits) != 1 {
		return "", fmt.Errorf("revision %q does not resolve to a commit", revision)
	}
	return commits[0], nil
}

type checkpointIndex struct {
	exact map[string]uint64
	loose map[string]uint64
}

func newCheckpointIndex(records []model.Checkpoint) checkpointIndex {
	index := checkpointIndex{
		exact: map[string]uint64{},
		loose: map[string]uint64{},
	}
	for _, record := range records {
		if record.Type != model.AuthorAI {
			continue
		}
		exactKey := checkpointKey(record.Agent, record.Model, record.Session, record.TS)
		if _, exists := index.exact[exactKey]; !exists {
			index.exact[exactKey] = record.Seq
		}
		looseKey := checkpointKey(record.Agent, record.Model, record.Session, "")
		if _, exists := index.loose[looseKey]; !exists {
			index.loose[looseKey] = record.Seq
		}
	}
	return index
}

func (index checkpointIndex) sequence(attr model.Attribution) (uint64, bool) {
	if value, ok := index.exact[checkpointKey(attr.Agent, attr.Model, attr.Session, attr.TS)]; ok {
		return value, true
	}
	if value, ok := index.loose[checkpointKey(attr.Agent, attr.Model, attr.Session, "")]; ok {
		return value, true
	}
	return 0, false
}

type gitAIEntry struct {
	key    string
	ranges []GitAILineRange
	start  int
}

func gitAIEntries(
	path string,
	ranges []model.Range,
	index checkpointIndex,
) ([]gitAIEntry, map[string]gitAISessionWire, map[string]gitAIHumanWire, error) {
	entries := map[string]*gitAIEntry{}
	sessions := map[string]gitAISessionWire{}
	humans := map[string]gitAIHumanWire{}
	for _, value := range ranges {
		var key string
		switch value.Author {
		case model.AuthorAI:
			sessionID := gitAISessionID(value.Agent, value.Session)
			sequence, matched := index.sequence(value.Attribution)
			if !matched {
				continue
			}
			key = sessionID + "::" + gitAITraceID(sequence)
			modelName := value.Model
			if modelName == "" {
				modelName = "unknown"
			}
			session := gitAISessionWire{
				AgentID: &gitAIAgentIDWire{
					Tool:  value.Agent,
					ID:    value.Session,
					Model: modelName,
				},
			}
			if previous, ok := sessions[sessionID]; ok && !sameSessionWire(previous, session) {
				return nil, nil, nil, fmt.Errorf(
					"file %q has conflicting session metadata for %s", path, sessionID,
				)
			}
			sessions[sessionID] = session
		case model.AuthorHuman, model.AuthorHumanOverride:
			authorID := gitAIHumanID(syntheticHumanAuthor)
			key = authorID
			humans[authorID] = gitAIHumanWire{Author: syntheticHumanAuthor}
		case model.AuthorUntracked:
			continue
		default:
			return nil, nil, nil, fmt.Errorf("unsupported author %q", value.Author)
		}
		entry := entries[key]
		if entry == nil {
			entry = &gitAIEntry{key: key, start: value.Start}
			entries[key] = entry
		}
		entry.ranges = append(entry.ranges, GitAILineRange{Start: value.Start, End: value.End})
		if value.Start < entry.start {
			entry.start = value.Start
		}
	}
	result := make([]gitAIEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, *entry)
	}
	sortGitAIEntries(result)
	return result, sessions, humans, nil
}

type gitAIWireMetadata struct {
	SchemaVersion string                      `json:"schema_version"`
	BaseCommitSHA string                      `json:"base_commit_sha"`
	GitAIVersion  string                      `json:"git_ai_version,omitempty"`
	Prompts       map[string]gitAIPromptWire  `json:"prompts"`
	Sessions      map[string]gitAISessionWire `json:"sessions,omitempty"`
	Humans        map[string]gitAIHumanWire   `json:"humans,omitempty"`
}

type gitAIAgentIDWire struct {
	Tool  string `json:"tool"`
	ID    string `json:"id"`
	Model string `json:"model"`
}

type gitAISessionWire struct {
	AgentID          *gitAIAgentIDWire `json:"agent_id"`
	HumanAuthor      string            `json:"human_author,omitempty"`
	CustomAttributes map[string]string `json:"custom_attributes,omitempty"`
}

type gitAIHumanWire struct {
	Author string `json:"author"`
}

type gitAIPromptWire struct {
	AgentID          *gitAIAgentIDWire `json:"agent_id"`
	HumanAuthor      string            `json:"human_author,omitempty"`
	MessagesURL      string            `json:"messages_url,omitempty"`
	TotalAdditions   *int              `json:"total_additions"`
	TotalDeletions   *int              `json:"total_deletions"`
	AcceptedLines    *int              `json:"accepted_lines"`
	OverridenLines   *int              `json:"overriden_lines"`
	CustomAttributes map[string]string `json:"custom_attributes,omitempty"`
}

func sameSessionWire(left, right gitAISessionWire) bool {
	if left.AgentID == nil || right.AgentID == nil {
		return left.AgentID == nil && right.AgentID == nil
	}
	return *left.AgentID == *right.AgentID &&
		left.HumanAuthor == right.HumanAuthor &&
		sameStringMap(left.CustomAttributes, right.CustomAttributes)
}

func sameStringMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

func decodeGitAIMetadata(data []byte) (gitAIWireMetadata, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var metadata gitAIWireMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return gitAIWireMetadata{}, fmt.Errorf("decode Git AI metadata: %w", err)
	}
	var extra any
	err := decoder.Decode(&extra)
	switch {
	case errors.Is(err, io.EOF):
	case err == nil:
		return gitAIWireMetadata{}, errors.New("multiple JSON values in Git AI metadata")
	default:
		return gitAIWireMetadata{}, fmt.Errorf("decode Git AI metadata tail: %w", err)
	}
	if metadata.SchemaVersion == "" || metadata.BaseCommitSHA == "" {
		return gitAIWireMetadata{}, errors.New("Git AI metadata is missing required fields")
	}
	if !validGitAIString(metadata.GitAIVersion, false) {
		return gitAIWireMetadata{}, errors.New("invalid Git AI metadata string")
	}
	return metadata, nil
}

func validateGitAIMetadata(
	agentID *gitAIAgentIDWire,
	humanAuthor string,
	customAttributes map[string]string,
) error {
	if agentID == nil ||
		!validGitAIString(agentID.Tool, true) ||
		!validGitAIString(agentID.ID, false) ||
		!validGitAIString(agentID.Model, false) ||
		!validGitAIString(humanAuthor, false) {
		return errors.New("invalid metadata string")
	}
	for key, value := range customAttributes {
		if !validGitAIString(key, true) || !validGitAIString(value, false) {
			return errors.New("invalid metadata string")
		}
	}
	return nil
}

func validGitAIString(value string, required bool) bool {
	if required && value == "" {
		return false
	}
	if len(value) > maxGitAIString || !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}

func (note GitAINote) attribution(key string) (model.Attribution, error) {
	switch {
	case strings.HasPrefix(key, "s_"):
		sessionID, _, _ := strings.Cut(key, "::")
		session, ok := note.Sessions[sessionID]
		if !ok {
			return model.Attribution{}, fmt.Errorf("Git AI session %q is missing", sessionID)
		}
		value := model.Attribution{
			Author:  model.AuthorAI,
			Agent:   session.AgentID.Tool,
			Model:   session.AgentID.Model,
			Session: sessionID,
		}
		if err := model.ValidateAttribution(value); err != nil {
			return model.Attribution{}, err
		}
		return value, nil
	case strings.HasPrefix(key, "h_"):
		if _, ok := note.Humans[key]; !ok {
			return model.Attribution{}, fmt.Errorf("Git AI human %q is missing", key)
		}
		return model.Attribution{Author: model.AuthorHuman}, nil
	default:
		prompt, ok := note.Prompts[key]
		if !ok {
			return model.Attribution{}, fmt.Errorf("Git AI prompt %q is missing", key)
		}
		value := model.Attribution{
			Author:  model.AuthorAI,
			Agent:   prompt.AgentID.Tool,
			Model:   prompt.AgentID.Model,
			Session: key,
		}
		if err := model.ValidateAttribution(value); err != nil {
			return model.Attribution{}, err
		}
		return value, nil
	}
}

func validateGitAIKey(key string, note GitAINote) error {
	switch {
	case strings.HasPrefix(key, "s_"):
		parts := strings.Split(key, "::")
		if len(parts) != 2 || !validSessionID(parts[0]) || !validTraceID(parts[1]) {
			return fmt.Errorf("invalid Git AI session key %q", key)
		}
		if _, ok := note.Sessions[parts[0]]; !ok {
			return fmt.Errorf("Git AI session %q is missing", parts[0])
		}
	case strings.HasPrefix(key, "h_"):
		if !validHumanID(key) {
			return fmt.Errorf("invalid Git AI human key %q", key)
		}
		if _, ok := note.Humans[key]; !ok {
			return fmt.Errorf("Git AI human %q is missing", key)
		}
	default:
		if !validLegacyID(key) {
			return fmt.Errorf("invalid Git AI legacy key %q", key)
		}
		if _, ok := note.Prompts[key]; !ok {
			return fmt.Errorf("Git AI prompt %q is missing", key)
		}
	}
	return nil
}

func validateAttestationRanges(entries []GitAIAttestation) error {
	type rangeValue struct {
		start int
		end   int
	}
	var values []rangeValue
	for _, entry := range entries {
		for _, value := range entry.Ranges {
			values = append(values, rangeValue{start: value.Start, end: value.End})
		}
	}
	if len(values) > maxGitAIEntries {
		return fmt.Errorf("more than %d attestation ranges", maxGitAIEntries)
	}
	sort.Slice(values, func(left, right int) bool {
		if values[left].start == values[right].start {
			return values[left].end < values[right].end
		}
		return values[left].start < values[right].start
	})
	for i := 1; i < len(values); i++ {
		if values[i].start <= values[i-1].end {
			return errors.New("attestation ranges overlap")
		}
	}
	return nil
}

func parseGitAIPath(line string) (string, error) {
	if line == "" {
		return "", errors.New("file path is empty")
	}
	if line[0] == '"' {
		path, err := strconv.Unquote(line)
		if err != nil {
			return "", fmt.Errorf("invalid quoted path: %w", err)
		}
		if path == "" {
			return "", errors.New("file path is empty")
		}
		line = path
	} else if strings.ContainsAny(line, " \t") {
		return "", errors.New("paths containing whitespace must be quoted")
	}
	normalized, err := gitcmd.NormalizePath(line)
	if err != nil {
		return "", fmt.Errorf("invalid file path: %w", err)
	}
	return normalized, nil
}

func parseLineRanges(value string) ([]GitAILineRange, error) {
	if value == "" {
		return nil, errors.New("line range is empty")
	}
	parts := strings.Split(value, ",")
	result := make([]GitAILineRange, 0, len(parts))
	previousEnd := 0
	for _, part := range parts {
		if part == "" {
			return nil, errors.New("line range contains an empty item")
		}
		start, end := 0, 0
		if strings.Contains(part, "-") {
			values := strings.Split(part, "-")
			if len(values) != 2 {
				return nil, fmt.Errorf("invalid line range %q", part)
			}
			var err error
			start, err = strconv.Atoi(values[0])
			if err != nil {
				return nil, fmt.Errorf("invalid line range %q", part)
			}
			end, err = strconv.Atoi(values[1])
			if err != nil {
				return nil, fmt.Errorf("invalid line range %q", part)
			}
		} else {
			var err error
			start, err = strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("invalid line range %q", part)
			}
			end = start
		}
		if start < 1 || end < start || start <= previousEnd {
			return nil, fmt.Errorf("line ranges are not positive and ordered in %q", value)
		}
		result = append(result, GitAILineRange{Start: start, End: end})
		previousEnd = end
	}
	return result, nil
}

func rangesFromAttributions(values []model.Attribution) []model.Range {
	if len(values) == 0 {
		return nil
	}
	result := make([]model.Range, 0)
	start := 0
	for index := 1; index <= len(values); index++ {
		if index < len(values) && values[index] == values[start] {
			continue
		}
		result = append(result, model.Range{
			Start:       start + 1,
			End:         index,
			Attribution: values[start],
		})
		start = index
	}
	return result
}

func formatLineRanges(values []GitAILineRange) string {
	var builder strings.Builder
	for index, value := range values {
		if index > 0 {
			builder.WriteByte(',')
		}
		if value.Start == value.End {
			builder.WriteString(strconv.Itoa(value.Start))
		} else {
			fmt.Fprintf(&builder, "%d-%d", value.Start, value.End)
		}
	}
	return builder.String()
}

func writeGitAIPath(builder *strings.Builder, path string) {
	if strings.ContainsAny(path, " \t\n") || strings.HasPrefix(path, `"`) {
		builder.WriteString(strconv.Quote(path))
	} else {
		builder.WriteString(path)
	}
	builder.WriteByte('\n')
}

func sortedNotePaths(note model.Note) []string {
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sortStrings(paths)
	return paths
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func sortGitAIEntries(values []gitAIEntry) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0; j-- {
			left, right := values[j-1], values[j]
			if left.start < right.start || (left.start == right.start && left.key <= right.key) {
				break
			}
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}

func checkpointKey(agent, modelName, session, timestamp string) string {
	return agent + "\x00" + modelName + "\x00" + session + "\x00" + timestamp
}

func gitAISessionID(tool, conversation string) string {
	sum := sha256.Sum256([]byte(tool + ":" + conversation))
	return "s_" + hex.EncodeToString(sum[:])[:14]
}

func gitAIHumanID(author string) string {
	sum := sha256.Sum256([]byte(author))
	return "h_" + hex.EncodeToString(sum[:])[:14]
}

func gitAITraceID(sequence uint64) string {
	sum := sha256.Sum256([]byte("git-byline:checkpoint:" + strconv.FormatUint(sequence, 10)))
	return "t_" + hex.EncodeToString(sum[:])[:14]
}

func traceUUID(commit string) string {
	sum := sha256.Sum256([]byte("git-byline:agent-trace:" + commit))
	value := sum[:16]
	value[6] = (value[6] & 0x0f) | 0x50
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(value[0:4]),
		hex.EncodeToString(value[4:6]),
		hex.EncodeToString(value[6:8]),
		hex.EncodeToString(value[8:10]),
		hex.EncodeToString(value[10:16]),
	)
}

func validObjectID(value string) bool {
	return model.ValidObjectID(value) && isHex(value)
}

func isHex(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') ||
			(char >= 'A' && char <= 'F')) {
			return false
		}
	}
	return true
}

func validSessionID(value string) bool {
	return len(value) == 16 && strings.HasPrefix(value, "s_") && isHex(value[2:])
}

func validTraceID(value string) bool {
	return len(value) == 16 && strings.HasPrefix(value, "t_") && isHex(value[2:])
}

func validHumanID(value string) bool {
	return len(value) == 16 && strings.HasPrefix(value, "h_") && isHex(value[2:])
}

func validLegacyID(value string) bool {
	return (len(value) == 16 || len(value) == 7) && isHex(value)
}

func sameNoteBytes(left, right []byte) bool {
	return bytes.Equal(bytes.TrimSuffix(left, []byte{'\n'}), bytes.TrimSuffix(right, []byte{'\n'}))
}

func isDifferentNoteConflict(err error) bool {
	return err != nil && err.Error() == "commit already has a different attribution note"
}

func importCommits(repo *gitcmd.Repo, revisionRange string) ([]string, error) {
	if revisionRange == "" {
		head, err := repo.Head()
		if err != nil {
			return nil, fmt.Errorf("read HEAD for import: %w", err)
		}
		if head == "" {
			return nil, nil
		}
		return []string{head}, nil
	}
	commits, err := repo.RevList("", revisionRange, maxImportCommits+1)
	if err != nil {
		return nil, fmt.Errorf("list import commits: %w", err)
	}
	if len(commits) > maxImportCommits {
		return nil, fmt.Errorf("import range exceeds %d commits", maxImportCommits)
	}
	return commits, nil
}

func appendTraceMetadata(
	metadata map[string][]TraceAttribution,
	source TraceAttribution,
) map[string][]TraceAttribution {
	if metadata == nil {
		metadata = map[string][]TraceAttribution{}
	}
	metadata["comarch.git-byline"] = append(metadata["comarch.git-byline"], source)
	return metadata
}

type traceGroup struct {
	key         string
	agent       string
	session     string
	contributor AgentTraceContributor
	ranges      []AgentTraceRange
	start       int
}

func traceConversations(path string, ranges []model.Range) ([]AgentTraceConversation, []TraceAttribution, error) {
	groups := map[string]*traceGroup{}
	for _, value := range ranges {
		contributor := AgentTraceContributor{}
		switch value.Author {
		case model.AuthorAI:
			contributor.Type = "ai"
			contributor.ModelID = value.Model
		case model.AuthorHuman, model.AuthorHumanOverride:
			contributor.Type = "human"
		case model.AuthorUntracked:
			contributor.Type = "unknown"
		default:
			return nil, nil, fmt.Errorf("unsupported author %q", value.Author)
		}
		key := string(contributor.Type) + "\x00" + contributor.ModelID + "\x00" +
			value.Agent + "\x00" + value.Session
		group := groups[key]
		if group == nil {
			group = &traceGroup{
				key: key, agent: value.Agent, session: value.Session,
				contributor: contributor, start: value.Start,
			}
			groups[key] = group
		}
		group.ranges = append(group.ranges, AgentTraceRange{
			StartLine: value.Start, EndLine: value.End,
		})
	}
	groupsList := make([]traceGroup, 0, len(groups))
	for _, group := range groups {
		groupsList = append(groupsList, *group)
	}
	for i := 1; i < len(groupsList); i++ {
		for j := i; j > 0 && groupsList[j].key < groupsList[j-1].key; j-- {
			groupsList[j], groupsList[j-1] = groupsList[j-1], groupsList[j]
		}
	}
	conversations := make([]AgentTraceConversation, 0, len(groupsList))
	sources := make([]TraceAttribution, 0, len(groupsList))
	for index, group := range groupsList {
		conversations = append(conversations, AgentTraceConversation{
			Contributor: group.contributor,
			Ranges:      group.ranges,
		})
		if group.agent != "" || group.session != "" {
			sources = append(sources, TraceAttribution{
				File: path, Conversation: index, Agent: group.agent, Session: group.session,
			})
		}
	}
	return conversations, sources, nil
}
