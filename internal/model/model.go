// Package model defines the versioned records stored by git-byline.
package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// CheckpointVersion is the supported checkpoint log format.
	CheckpointVersion = 1
	// StateVersion is the supported state file format.
	StateVersion = 1
	// NoteVersionV1 is the legacy git note format used by persisted state.
	NoteVersionV1 = 1
	// NoteVersion is the supported git note format.
	NoteVersion = 2
	// NoteSessionSeparator separates an agent from a session map key.
	NoteSessionSeparator = "::"
	// MaxTextLines bounds attribution memory for one file.
	MaxTextLines = 1_000_000

	// CheckpointKindEdit records a file edit event.
	CheckpointKindEdit = "edit"
	// CheckpointKindShellPre records the state before a shell event.
	CheckpointKindShellPre = "shell_pre"
	// CheckpointKindShellPost records the changed state after a shell event.
	CheckpointKindShellPost = "shell_post"
)

// Author identifies the source of one or more lines.
type Author string

const (
	AuthorHuman         Author = "human"
	AuthorAI            Author = "ai"
	AuthorUntracked     Author = "untracked"
	AuthorHumanOverride Author = "human-override"
)

// Attribution records line authorship and optional agent metadata.
type Attribution struct {
	Author  Author `json:"author"`
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Session string `json:"session,omitempty"`
	TS      string `json:"ts,omitempty"`
}

// Range is an inclusive, one-based line interval.
type Range struct {
	Start int `json:"start"`
	End   int `json:"end"`
	Attribution
}

// Snapshot identifies one repository-relative file state.
type Snapshot struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Blob   string `json:"blob,omitempty"`
}

// Checkpoint is one append-only edit record.
type Checkpoint struct {
	Version    int        `json:"version"`
	Kind       string     `json:"kind"`
	Seq        uint64     `json:"seq"`
	BaseCommit string     `json:"base_commit,omitempty"`
	EventID    string     `json:"event_id,omitempty"`
	TS         string     `json:"ts"`
	Type       Author     `json:"type"`
	Session    string     `json:"session,omitempty"`
	Agent      string     `json:"agent,omitempty"`
	Model      string     `json:"model,omitempty"`
	Files      []Snapshot `json:"files"`
}

// PendingFile keeps provenance for uncommitted worktree content.
type PendingFile struct {
	Blob   string  `json:"blob"`
	Ranges []Range `json:"ranges"`
}

// PendingState keeps provenance carried across a partial commit.
type PendingState struct {
	BaseCommit string                 `json:"base_commit,omitempty"`
	Files      map[string]PendingFile `json:"files"`
}

// State records the durable replay boundary.
type State struct {
	Version             int          `json:"version"`
	LastAnnotatedCommit string       `json:"last_annotated_commit,omitempty"`
	LastCheckpointSeq   uint64       `json:"last_checkpoint_seq"`
	NotesVersion        int          `json:"notes_version"`
	Pending             PendingState `json:"pending"`
}

// NoteFile stores attribution for one committed blob.
type NoteFile struct {
	Blob   string  `json:"blob"`
	Ranges []Range `json:"ranges"`
}

// NoteSession stores session-level attribution metrics for one note.
type NoteSession struct {
	Agent      string `json:"agent"`
	Model      string `json:"model"`
	FirstTS    string `json:"first_ts"`
	LastTS     string `json:"last_ts"`
	Added      int    `json:"added"`
	Deleted    int    `json:"deleted"`
	Accepted   int    `json:"accepted"`
	Overridden int    `json:"overridden"`
}

// Note is the versioned value stored in refs/notes/byline.
type Note struct {
	Version  int                    `json:"version"`
	Files    map[string]NoteFile    `json:"files"`
	Sessions map[string]NoteSession `json:"sessions"`
}

// NoteSessionKey returns the deterministic key for one agent session.
func NoteSessionKey(agent, session string) string {
	return agent + NoteSessionSeparator + session
}

var objectIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,128}$`)

const maxAttributionValueBytes = 1024

// ValidObjectID reports whether value can be a Git object ID.
func ValidObjectID(value string) bool {
	return objectIDPattern.MatchString(value)
}

// ValidateAttribution validates one attribution value.
func ValidateAttribution(value Attribution) error {
	switch value.Author {
	case AuthorHuman, AuthorUntracked:
		if value.Agent != "" || value.Model != "" || value.Session != "" || value.TS != "" {
			return fmt.Errorf("%s attribution contains agent metadata", value.Author)
		}
	case AuthorAI, AuthorHumanOverride:
		if value.Agent == "" {
			return fmt.Errorf("%s attribution requires agent", value.Author)
		}
		return validateAgentMetadata(value)
	default:
		return fmt.Errorf("unknown author %q", value.Author)
	}
	return nil
}

func validateAgentMetadata(value Attribution) error {
	if strings.Contains(value.Agent, NoteSessionSeparator) {
		return fmt.Errorf("%s attribution agent contains reserved separator", value.Author)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"agent", value.Agent},
		{"model", value.Model},
		{"session", value.Session},
		{"timestamp", value.TS},
	} {
		name, metadata := field.name, field.value
		if len(metadata) > maxAttributionValueBytes {
			return fmt.Errorf("%s attribution %s exceeds %d bytes", value.Author, name, maxAttributionValueBytes)
		}
		if !utf8.ValidString(metadata) {
			return fmt.Errorf("%s attribution %s is not valid UTF-8", value.Author, name)
		}
		for _, char := range metadata {
			if unicode.IsControl(char) {
				return fmt.Errorf("%s attribution %s contains a control character", value.Author, name)
			}
		}
	}
	if value.TS != "" {
		if _, err := time.Parse(time.RFC3339Nano, value.TS); err != nil {
			return fmt.Errorf("%s attribution timestamp is not RFC3339: %w", value.Author, err)
		}
	}
	return nil
}

// ValidateEventID validates an optional hook event identifier.
func ValidateEventID(value string) error {
	if len(value) > maxAttributionValueBytes {
		return fmt.Errorf("event id exceeds %d bytes", maxAttributionValueBytes)
	}
	if !utf8.ValidString(value) {
		return errors.New("event id is not valid UTF-8")
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return errors.New("event id contains a control character")
		}
	}
	return nil
}

// ValidateRanges checks complete, ordered, non-overlapping line coverage.
func ValidateRanges(ranges []Range, lineCount int) error {
	if lineCount == 0 {
		if len(ranges) != 0 {
			return errors.New("empty content must not have ranges")
		}
		return nil
	}
	if len(ranges) == 0 {
		return errors.New("non-empty content requires ranges")
	}
	next := 1
	for i, value := range ranges {
		if err := ValidateAttribution(value.Attribution); err != nil {
			return fmt.Errorf("range %d: %w", i, err)
		}
		if value.Start != next {
			return fmt.Errorf("range %d starts at %d, want %d", i, value.Start, next)
		}
		if value.End < value.Start || value.End > lineCount {
			return fmt.Errorf("range %d has invalid bounds %d-%d for %d lines", i, value.Start, value.End, lineCount)
		}
		next = value.End + 1
	}
	if next != lineCount+1 {
		return fmt.Errorf("ranges end at %d, want %d", next-1, lineCount)
	}
	return nil
}

// NewState returns an initialized empty state.
func NewState() State {
	return State{
		Version:      StateVersion,
		NotesVersion: NoteVersion,
		Pending: PendingState{
			Files: map[string]PendingFile{},
		},
	}
}
