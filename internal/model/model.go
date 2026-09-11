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
	// NoteVersionV2 is the git note format without human identities.
	NoteVersionV2 = 2
	// NoteVersion is the supported git note format.
	NoteVersion = 3
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
	Author   Author `json:"author"`
	Identity string `json:"identity,omitempty"`
	Agent    string `json:"agent,omitempty"`
	Model    string `json:"model,omitempty"`
	Session  string `json:"session,omitempty"`
	TS       string `json:"ts,omitempty"`
}

// Label renders one attribution as a stable single-token source label.
func (value Attribution) Label() string {
	label := string(value.Author)
	switch value.Author {
	case AuthorHuman:
		if value.Identity != "" {
			label += ":" + value.Identity
		}
	case AuthorAI:
		label += ":" + value.Agent
		if value.Model != "" {
			label += "/" + value.Model
		}
	case AuthorHumanOverride:
		label += ":"
		if value.Identity != "" {
			label += value.Identity + "/"
		}
		label += value.Agent
		if value.Model != "" {
			label += "/" + value.Model
		}
	}
	return label
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

// MaxIdentityBytes bounds one stored human identity token.
const MaxIdentityBytes = 64

var identityPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._+-]*[a-z0-9])?$`)

// NormalizeIdentity derives a stable human identity token from a Git author
// name and email address. It prefers the email local part, because that is
// the part corporate Git identities keep unique, and falls back to the
// author name. Input that cannot be reduced to a safe token yields an empty
// identity, so the line stays honestly anonymous instead of guessed.
func NormalizeIdentity(name, email string) string {
	if local, _, found := strings.Cut(email, "@"); found {
		if token := identityToken(local); token != "" {
			return token
		}
	}
	return identityToken(name)
}

func identityToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9',
			char == '.', char == '_', char == '+', char == '-':
			builder.WriteRune(char)
		case char == ' ' || char == '\t':
			builder.WriteByte('.')
		}
	}
	token := strings.Trim(builder.String(), "._+-")
	if len(token) > MaxIdentityBytes {
		return ""
	}
	if !identityPattern.MatchString(token) {
		return ""
	}
	return token
}

// ValidateIdentity validates one stored human identity token.
func ValidateIdentity(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > MaxIdentityBytes {
		return fmt.Errorf("identity exceeds %d bytes", MaxIdentityBytes)
	}
	if !identityPattern.MatchString(value) {
		return fmt.Errorf("identity %q is not a normalized token", value)
	}
	return nil
}

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
		if value.Author == AuthorUntracked && value.Identity != "" {
			return errors.New("untracked attribution contains an identity")
		}
		if err := ValidateIdentity(value.Identity); err != nil {
			return fmt.Errorf("%s attribution: %w", value.Author, err)
		}
	case AuthorAI, AuthorHumanOverride:
		if value.Agent == "" {
			return fmt.Errorf("%s attribution requires agent", value.Author)
		}
		if value.Author == AuthorAI && value.Identity != "" {
			return errors.New("ai attribution contains a human identity")
		}
		if err := ValidateIdentity(value.Identity); err != nil {
			return fmt.Errorf("%s attribution: %w", value.Author, err)
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
