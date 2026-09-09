// Package preset converts supported agent hook payloads into edit events.
package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mrwogu/git-byline/internal/model"
)

const MaxInputBytes = 1 << 20

// Event is one normalized agent edit.
type Event struct {
	Type    model.Author
	Agent   string
	Model   string
	Session string
	Paths   []string
}

// Parse converts a supported preset payload. handled is false for a valid,
// unrelated hook event.
func Parse(name string, explicit model.Author, input io.Reader) (event Event, handled bool, err error) {
	data, err := readBounded(input)
	if err != nil {
		return Event{}, false, err
	}
	switch name {
	case "droid":
		return parseToolHook("droid", explicit, data, []string{"Edit", "Create", "ApplyPatch"})
	case "claude":
		return parseToolHook("claude", explicit, data, []string{"Write", "Edit", "MultiEdit"})
	case "agent-v1":
		if explicit != "" {
			return Event{}, false, errors.New("agent-v1 takes type from its payload")
		}
		return parseAgentV1(data)
	default:
		return Event{}, false, fmt.Errorf("unknown preset %q", name)
	}
}

func parseToolHook(agent string, explicit model.Author, data []byte, allowed []string) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload struct {
		SessionID string          `json:"session_id"`
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
		Model     string          `json:"model"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode %s hook input: %w", agent, err)
	}
	tool := payload.ToolName
	if index := strings.LastIndex(tool, "."); index >= 0 {
		tool = tool[index+1:]
	}
	if !contains(allowed, tool) {
		return Event{}, false, nil
	}
	if len(payload.ToolInput) == 0 {
		return Event{}, false, errors.New("tool_input is missing")
	}
	var toolInput struct {
		FilePath string `json:"file_path"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal(payload.ToolInput, &toolInput); err != nil {
		return Event{}, false, fmt.Errorf("decode tool_input: %w", err)
	}
	var paths []string
	if tool == "ApplyPatch" {
		var err error
		paths, err = patchPaths(toolInput.Patch)
		if err != nil {
			return Event{}, false, err
		}
	} else {
		if toolInput.FilePath == "" {
			return Event{}, false, errors.New("tool_input.file_path is missing")
		}
		paths = []string{toolInput.FilePath}
	}
	modelName := payload.Model
	if modelName == "" {
		modelName = "unknown"
	}
	event := Event{Type: explicit, Paths: uniquePaths(paths)}
	if explicit == model.AuthorAI {
		event.Agent = agent
		event.Model = modelName
		event.Session = payload.SessionID
		if err := model.ValidateAttribution(model.Attribution{
			Author: event.Type, Agent: event.Agent, Model: event.Model, Session: event.Session,
		}); err != nil {
			return Event{}, false, err
		}
	}
	return event, true, nil
}

func parseAgentV1(data []byte) (Event, bool, error) {
	var payload struct {
		Type            string   `json:"type"`
		AgentName       string   `json:"agent_name"`
		Model           string   `json:"model"`
		ConversationID  string   `json:"conversation_id"`
		EditedFilepaths []string `json:"edited_filepaths"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode agent-v1 input: %w", err)
	}
	var author model.Author
	switch payload.Type {
	case "human":
		author = model.AuthorHuman
	case "ai_agent":
		author = model.AuthorAI
	default:
		return Event{}, false, fmt.Errorf("unsupported agent-v1 type %q", payload.Type)
	}
	if payload.AgentName == "" {
		return Event{}, false, errors.New("agent_name is missing")
	}
	if len(payload.EditedFilepaths) == 0 {
		return Event{}, false, errors.New("edited_filepaths is empty")
	}
	modelName := payload.Model
	if modelName == "" {
		modelName = "unknown"
	}
	event := Event{Type: author, Paths: uniquePaths(payload.EditedFilepaths)}
	if author == model.AuthorAI {
		event.Agent = payload.AgentName
		event.Model = modelName
		event.Session = payload.ConversationID
		if err := model.ValidateAttribution(model.Attribution{
			Author: event.Type, Agent: event.Agent, Model: event.Model, Session: event.Session,
		}); err != nil {
			return Event{}, false, err
		}
	}
	return event, true, nil
}

func patchPaths(patch string) ([]string, error) {
	if patch == "" {
		return nil, errors.New("patch is missing")
	}
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		var path string
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			path = strings.TrimPrefix(line, "*** Add File: ")
		case strings.HasPrefix(line, "*** Update File: "):
			path = strings.TrimPrefix(line, "*** Update File: ")
		case strings.HasPrefix(line, "*** Delete File: "):
			path = strings.TrimPrefix(line, "*** Delete File: ")
		case strings.HasPrefix(line, "*** Move to: "):
			path = strings.TrimPrefix(line, "*** Move to: ")
		default:
			continue
		}
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, errors.New("patch contains an empty file path")
		}
		paths = append(paths, path)
	}
	if len(paths) == 0 {
		return nil, errors.New("patch contains no supported file operation")
	}
	return uniquePaths(paths), nil
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func readBounded(input io.Reader) ([]byte, error) {
	if input == nil {
		return nil, errors.New("hook input is nil")
	}
	data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read hook input: %w", err)
	}
	if len(data) > MaxInputBytes {
		return nil, fmt.Errorf("hook input exceeds %d bytes", MaxInputBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("hook input is empty")
	}
	return data, nil
}
