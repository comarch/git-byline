// Package notes encodes and locates versioned attribution notes.
package notes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
)

// Encode returns canonical JSON with a trailing newline.
func Encode(note model.Note) ([]byte, error) {
	if note.Version != model.NoteVersion {
		return nil, fmt.Errorf("unsupported note version %d", note.Version)
	}
	if note.Files == nil {
		note.Files = map[string]model.NoteFile{}
	}
	if err := validateNote(note); err != nil {
		return nil, err
	}
	data, err := json.Marshal(note)
	if err != nil {
		return nil, fmt.Errorf("encode note: %w", err)
	}
	return append(data, '\n'), nil
}

// Decode reads a supported note version.
func Decode(data []byte) (model.Note, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return model.Note{}, fmt.Errorf("decode note header: %w", err)
	}
	if header.Version != model.NoteVersion {
		return model.Note{}, fmt.Errorf("unsupported note version %d", header.Version)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var note model.Note
	if err := decoder.Decode(&note); err != nil {
		return model.Note{}, fmt.Errorf("decode note: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return model.Note{}, errors.New("multiple JSON values in note")
		}
		return model.Note{}, fmt.Errorf("decode note tail: %w", err)
	}
	if note.Files == nil {
		note.Files = map[string]model.NoteFile{}
	}
	if err := validateNote(note); err != nil {
		return model.Note{}, err
	}
	return note, nil
}

func validateNote(note model.Note) error {
	paths := make([]string, 0, len(note.Files))
	for path := range note.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := note.Files[path]
		if path == "" {
			return errors.New("note file path is empty")
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
