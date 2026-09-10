// Package notes encodes and locates versioned attribution notes.
package notes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

const (
	// MaxEncodedBytes bounds note parsing before allocating the complete model.
	MaxEncodedBytes = 16 << 20
	// MaxFiles bounds file entries in one attribution note.
	MaxFiles = 500
)

type noteSession = model.NoteSession

// Encode returns canonical JSON with a trailing newline.
func Encode(note model.Note) ([]byte, error) {
	if note.Version != model.NoteVersion {
		return nil, fmt.Errorf("unsupported note version %d", note.Version)
	}
	if note.Files == nil {
		note.Files = map[string]model.NoteFile{}
	}
	if note.Sessions == nil {
		note.Sessions = map[string]model.NoteSession{}
	}
	if len(note.Files) > MaxFiles {
		return nil, fmt.Errorf("note has more than %d files", MaxFiles)
	}
	if err := validateNote(note); err != nil {
		return nil, err
	}
	data, err := json.Marshal(note)
	if err != nil {
		return nil, fmt.Errorf("encode note: %w", err)
	}
	if len(data)+1 > MaxEncodedBytes {
		return nil, fmt.Errorf("note exceeds %d bytes", MaxEncodedBytes)
	}
	return append(data, '\n'), nil
}

// Decode reads a supported note version.
func Decode(data []byte) (model.Note, error) {
	if len(data) > MaxEncodedBytes {
		return model.Note{}, fmt.Errorf("note exceeds %d bytes", MaxEncodedBytes)
	}
	if err := CheckFileCount(data, MaxFiles); err != nil {
		return model.Note{}, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return model.Note{}, fmt.Errorf("decode note header: %w", err)
	}
	if header.Version != 1 && header.Version != 2 {
		return model.Note{}, fmt.Errorf("unsupported note version %d", header.Version)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var wire struct {
		Version  int                       `json:"version"`
		Files    map[string]model.NoteFile `json:"files"`
		Sessions map[string]*noteSession   `json:"sessions"`
	}
	if header.Version == 1 {
		var legacy struct {
			Version int                       `json:"version"`
			Files   map[string]model.NoteFile `json:"files"`
		}
		if err := decoder.Decode(&legacy); err != nil {
			return model.Note{}, fmt.Errorf("decode note: %w", err)
		}
		wire.Version = legacy.Version
		wire.Files = legacy.Files
	} else if err := decoder.Decode(&wire); err != nil {
		return model.Note{}, fmt.Errorf("decode note: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return model.Note{}, errors.New("multiple JSON values in note")
		}
		return model.Note{}, fmt.Errorf("decode note tail: %w", err)
	}
	if wire.Version != header.Version {
		return model.Note{}, fmt.Errorf("note version changed during decode from %d to %d", header.Version, wire.Version)
	}
	for name, session := range wire.Sessions {
		if session == nil {
			return model.Note{}, fmt.Errorf("decode note session %q: session must be an object", name)
		}
	}
	note := model.Note{Version: wire.Version, Files: wire.Files}
	if wire.Version == 2 {
		note.Sessions = make(map[string]model.NoteSession, len(wire.Sessions))
		for name, session := range wire.Sessions {
			if err := validateSession(name, *session); err != nil {
				return model.Note{}, err
			}
			note.Sessions[name] = *session
		}
	}
	if note.Files == nil {
		note.Files = map[string]model.NoteFile{}
	}
	if err := validateNote(note); err != nil {
		return model.Note{}, err
	}
	return note, nil
}

// CheckFileCount rejects notes with more than limit file entries before the
// complete map is decoded.
func CheckFileCount(data []byte, limit int) error {
	if limit < 0 {
		return errors.New("note file limit is negative")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode note start: %w", err)
	}
	if token != json.Delim('{') {
		return errors.New("note must be a JSON object")
	}
	count := 0
	for decoder.More() {
		nameToken, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode note field: %w", err)
		}
		name, ok := nameToken.(string)
		if !ok {
			return errors.New("note field name is not a string")
		}
		if name != "files" {
			var skipped json.RawMessage
			if err := decoder.Decode(&skipped); err != nil {
				return fmt.Errorf("decode note field %q: %w", name, err)
			}
			continue
		}
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("decode note files: %w", err)
		}
		if token != json.Delim('{') {
			return errors.New("note files must be a JSON object")
		}
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return fmt.Errorf("decode note file name: %w", err)
			}
			count++
			if count > limit {
				return fmt.Errorf("note has more than %d files", limit)
			}
			var skipped json.RawMessage
			if err := decoder.Decode(&skipped); err != nil {
				return fmt.Errorf("decode note file: %w", err)
			}
		}
		if _, err := decoder.Token(); err != nil {
			return fmt.Errorf("decode note files end: %w", err)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return fmt.Errorf("decode note end: %w", err)
	}
	return nil
}

func validateNote(note model.Note) error {
	if note.Version != 2 && len(note.Sessions) > 0 {
		return errors.New("note sessions require version 2")
	}
	for name, session := range note.Sessions {
		if err := validateSession(name, session); err != nil {
			return err
		}
	}
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := note.Files[path]
		normalized, err := gitcmd.NormalizePath(path)
		if err != nil {
			return fmt.Errorf("note file path %q is invalid: %w", path, err)
		}
		if normalized != path {
			return fmt.Errorf("note file path %q is not normalized", path)
		}
		if !model.ValidObjectID(file.Blob) {
			return fmt.Errorf("note file %q has invalid blob", path)
		}
		lineCount := 0
		if len(file.Ranges) > 0 {
			lineCount = file.Ranges[len(file.Ranges)-1].End
		}
		if err := model.ValidateRanges(file.Ranges, lineCount); err != nil {
			return fmt.Errorf("note file %q: %w", path, err)
		}
	}
	return nil
}

func validateSession(name string, session model.NoteSession) error {
	if name == "" {
		return errors.New("note session name must not be empty")
	}
	if len(name) > 1024 || !utf8.ValidString(name) {
		return fmt.Errorf("note session %q has invalid name", name)
	}
	for _, char := range name {
		if unicode.IsControl(char) {
			return fmt.Errorf("note session %q has invalid name", name)
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"agent", session.Agent},
		{"model", session.Model},
		{"first_ts", session.FirstTS},
		{"last_ts", session.LastTS},
	} {
		fieldName, value := field.name, field.value
		if len(value) > 1024 {
			return fmt.Errorf("note session %q %s exceeds 1024 bytes", name, fieldName)
		}
		if !utf8.ValidString(value) {
			return fmt.Errorf("note session %q %s is not valid UTF-8", name, fieldName)
		}
		for _, char := range value {
			if unicode.IsControl(char) {
				return fmt.Errorf("note session %q %s contains a control character", name, fieldName)
			}
		}
	}
	for _, field := range []struct {
		name  string
		value int
	}{
		{"added", session.Added},
		{"deleted", session.Deleted},
		{"accepted", session.Accepted},
		{"overridden", session.Overridden},
	} {
		if field.value < 0 {
			return fmt.Errorf("note session %q %s is negative", name, field.name)
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"first_ts", session.FirstTS},
		{"last_ts", session.LastTS},
	} {
		if field.value == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, field.value); err != nil {
			return fmt.Errorf("note session %q %s is not RFC3339: %w", name, field.name, err)
		}
	}
	return nil
}

// FindFile walks first-parent history to find attribution for the exact blob.
func FindFile(repo *gitcmd.Repo, start, path, blob string) (model.NoteFile, bool, []string, error) {
	if start == "" {
		return model.NoteFile{}, false, nil, nil
	}
	history, err := repo.FirstParentHistory(start)
	if err != nil {
		return model.NoteFile{}, false, nil, err
	}
	var warnings []string
	for _, commit := range history {
		data, ok, err := repo.ReadNote(commit)
		if err != nil {
			return model.NoteFile{}, false, warnings, err
		}
		if !ok {
			continue
		}
		note, err := Decode(data)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("ignored attribution note on %s: %v", commit, err))
			continue
		}
		file, ok := note.Files[path]
		if !ok {
			continue
		}
		if file.Blob != blob {
			return model.NoteFile{}, false, append(warnings, fmt.Sprintf("attribution blob mismatch for %s", path)), nil
		}
		return file, true, warnings, nil
	}
	return model.NoteFile{}, false, warnings, nil
}
