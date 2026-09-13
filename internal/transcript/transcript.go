// Package transcript reads model identifiers from local agent session files.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// tailWindowBytes is the transcript suffix scanned for the latest model.
	tailWindowBytes = 8 << 20
	// maxTranscriptBytes bounds any single transcript read.
	maxTranscriptBytes = 64 << 20
	// maxLineBytes bounds one transcript JSONL line.
	maxLineBytes = 4 << 20
	// maxSidecarBytes bounds one session settings file read.
	maxSidecarBytes = 1 << 20
)

// ResolveModel returns the model of the session that produced one hook event.
// It scans the latest model in the transcript and falls back to the session
// sidecar settings file next to the transcript. An empty result with a nil
// error means no source named a model.
func ResolveModel(transcriptPath string) (string, error) {
	if transcriptPath == "" {
		return "", errors.New("transcript path is empty")
	}
	if !filepath.IsAbs(transcriptPath) {
		return "", fmt.Errorf("transcript path %q is not absolute", transcriptPath)
	}
	if model, err := fromTranscript(transcriptPath); err == nil && model != "" {
		return model, nil
	}
	if model, err := fromSidecar(transcriptPath); err == nil && model != "" {
		return model, nil
	}
	return "", nil
}

// fromTranscript returns the newest model recorded in a session JSONL file.
// Only the trailing window is scanned because the event's message is always
// near the end of an active session. Lines that fail to decode are skipped,
// including a line truncated by the window start.
func fromTranscript(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("transcript %s is not a regular file", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var reader io.Reader = io.LimitReader(file, maxTranscriptBytes+1)
	if size := info.Size(); size > tailWindowBytes {
		if _, err := file.Seek(size-tailWindowBytes, io.SeekStart); err != nil {
			return "", err
		}
		reader = io.LimitReader(file, tailWindowBytes)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var last string
	for scanner.Scan() {
		if model := lineModel(scanner.Bytes()); model != "" {
			last = model
		}
	}
	return last, nil
}

// lineModel extracts the model of one transcript entry. Droid records carry
// message.modelId and Claude Code records carry message.model.
func lineModel(line []byte) string {
	var entry struct {
		Message struct {
			Model   string `json:"model"`
			ModelID string `json:"modelId"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &entry); err != nil {
		return ""
	}
	for _, value := range []string{entry.Message.ModelID, entry.Message.Model} {
		if value != "" {
			return value
		}
	}
	return ""
}

// fromSidecar returns the session default model from the settings file that
// Droid keeps next to each session transcript. An absent file is an error so
// callers treat it as an unresolved model.
func fromSidecar(transcriptPath string) (string, error) {
	sidecar := strings.TrimSuffix(transcriptPath, filepath.Ext(transcriptPath)) + ".settings.json"
	info, err := os.Lstat(sidecar)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("session settings %s is not a regular file", sidecar)
	}
	file, err := os.Open(sidecar)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var settings struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(io.LimitReader(file, maxSidecarBytes)).Decode(&settings); err != nil {
		return "", err
	}
	return settings.Model, nil
}
