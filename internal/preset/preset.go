// Package preset converts supported agent hook payloads into edit events.
package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/comarch/git-byline/internal/model"
)

const MaxInputBytes = 1 << 20

// Event is one normalized agent event.
type Event struct {
	Kind    string
	Type    model.Author
	Agent   string
	Model   string
	Session string
	EventID string
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
		agent, ok := strings.CutPrefix(name, "portable-")
		if !ok || !slices.Contains(portableAgents, agent) {
			return Event{}, false, fmt.Errorf("unknown preset %q", name)
		}
		return parsePortableHook(agent, explicit, data)
	}
}

var portableAgents = []string{
	"factory",
	"claude",
	"copilot",
	"vscode",
	"cursor",
	"codex",
	"gemini",
	"windsurf",
	"grok",
}

func parseToolHook(agent string, explicit model.Author, data []byte, allowed []string) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload struct {
		SessionID       string          `json:"session_id"`
		SessionIDCamel  string          `json:"sessionId"`
		ConversationID  string          `json:"conversation_id"`
		ToolName        string          `json:"tool_name"`
		ToolNameCamel   string          `json:"toolName"`
		ToolInput       json.RawMessage `json:"tool_input"`
		ToolInputCamel  json.RawMessage `json:"toolInput"`
		Model           string          `json:"model"`
		ModelName       string          `json:"model_name"`
		ModelNameCamel  string          `json:"modelName"`
		ID              string          `json:"id"`
		EventID         string          `json:"event_id"`
		EventIDCamel    string          `json:"eventId"`
		ToolUseID       string          `json:"tool_use_id"`
		ToolUseIDCamel  string          `json:"toolUseId"`
		ToolCallID      string          `json:"tool_call_id"`
		ToolCallIDCamel string          `json:"toolCallId"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode %s hook input: %w", agent, err)
	}
	tool := firstValue(payload.ToolName, payload.ToolNameCamel)
	if index := strings.LastIndex(tool, "."); index >= 0 {
		tool = tool[index+1:]
	}
	eventID := firstValue(
		payload.ID, payload.EventID, payload.EventIDCamel,
		payload.ToolUseID, payload.ToolUseIDCamel,
		payload.ToolCallID, payload.ToolCallIDCamel,
	)
	if isShellOperation(tool) {
		return shellEvent(agent, explicit, firstValue(payload.Model, payload.ModelName, payload.ModelNameCamel),
			firstValue(payload.SessionID, payload.SessionIDCamel, payload.ConversationID), eventID)
	}
	if !slices.Contains(allowed, tool) {
		return Event{}, false, nil
	}
	rawToolInput := payload.ToolInput
	if len(rawToolInput) == 0 {
		rawToolInput = payload.ToolInputCamel
	}
	if len(rawToolInput) == 0 {
		return Event{}, false, errors.New("tool_input is missing")
	}
	var toolInput struct {
		FilePath string `json:"file_path"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal(rawToolInput, &toolInput); err != nil {
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
	modelName := firstValue(payload.Model, payload.ModelName, payload.ModelNameCamel)
	if modelName == "" {
		modelName = "unknown"
	}
	paths = uniquePaths(paths)
	if len(paths) == 0 {
		return Event{}, false, errors.New("hook input contains no usable file path")
	}
	event := Event{Kind: model.CheckpointKindEdit, Type: explicit, Paths: paths, EventID: eventID}
	if explicit == model.AuthorAI {
		event.Agent = agent
		event.Model = modelName
		event.Session = firstValue(payload.SessionID, payload.SessionIDCamel, payload.ConversationID)
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
		ID              string   `json:"id"`
		EventID         string   `json:"event_id"`
		EventIDCamel    string   `json:"eventId"`
		ToolUseID       string   `json:"tool_use_id"`
		ToolUseIDCamel  string   `json:"toolUseId"`
		ToolCallID      string   `json:"tool_call_id"`
		ToolCallIDCamel string   `json:"toolCallId"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode agent-v1 input: %w", err)
	}
	var author model.Author
	kind := model.CheckpointKindEdit
	switch payload.Type {
	case "human":
		author = model.AuthorHuman
	case "ai_agent":
		author = model.AuthorAI
	case model.CheckpointKindShellPre:
		author = model.AuthorHuman
		kind = model.CheckpointKindShellPre
	case model.CheckpointKindShellPost:
		author = model.AuthorAI
		kind = model.CheckpointKindShellPost
	default:
		return Event{}, false, fmt.Errorf("unsupported agent-v1 type %q", payload.Type)
	}
	if payload.AgentName == "" {
		return Event{}, false, errors.New("agent_name is missing")
	}
	if kind == model.CheckpointKindEdit && len(payload.EditedFilepaths) == 0 {
		return Event{}, false, errors.New("edited_filepaths is empty")
	}
	if kind != model.CheckpointKindEdit {
		modelName := payload.Model
		if modelName == "" {
			modelName = "unknown"
		}
		eventID := firstValue(
			payload.ID, payload.EventID, payload.EventIDCamel,
			payload.ToolUseID, payload.ToolUseIDCamel,
			payload.ToolCallID, payload.ToolCallIDCamel,
		)
		event := Event{Kind: kind, Type: author, EventID: eventID}
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
	modelName := payload.Model
	if modelName == "" {
		modelName = "unknown"
	}
	paths := uniquePaths(payload.EditedFilepaths)
	if len(paths) == 0 {
		return Event{}, false, errors.New("edited_filepaths contains no usable path")
	}
	event := Event{
		Kind: kind,
		Type: author,
		EventID: firstValue(
			payload.ID, payload.EventID, payload.EventIDCamel,
			payload.ToolUseID, payload.ToolUseIDCamel,
			payload.ToolCallID, payload.ToolCallIDCamel,
		),
		Paths: paths,
	}
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

func parsePortableHook(agent string, explicit model.Author, data []byte) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	var payload struct {
		SessionID            string          `json:"session_id"`
		SessionIDCamel       string          `json:"sessionId"`
		ConversationID       string          `json:"conversation_id"`
		ConversationIDCamel  string          `json:"conversationId"`
		ThreadID             string          `json:"thread_id"`
		ThreadIDCamel        string          `json:"threadId"`
		Model                string          `json:"model"`
		ModelName            string          `json:"model_name"`
		ModelNameCamel       string          `json:"modelName"`
		HookEventName        string          `json:"hook_event_name"`
		HookEventNameCamel   string          `json:"hookEventName"`
		EventName            string          `json:"event_name"`
		EventNameCamel       string          `json:"eventName"`
		ToolName             string          `json:"tool_name"`
		ToolNameCamel        string          `json:"toolName"`
		ID                   string          `json:"id"`
		EventID              string          `json:"event_id"`
		EventIDCamel         string          `json:"eventId"`
		ToolUseID            string          `json:"tool_use_id"`
		ToolUseIDCamel       string          `json:"toolUseId"`
		ToolCallID           string          `json:"tool_call_id"`
		ToolCallIDCamel      string          `json:"toolCallId"`
		ToolInput            json.RawMessage `json:"tool_input"`
		Arguments            json.RawMessage `json:"arguments"`
		ToolArgs             json.RawMessage `json:"toolArgs"`
		FilePath             string          `json:"file_path"`
		FilePathCamel        string          `json:"filePath"`
		File                 string          `json:"file"`
		Path                 string          `json:"path"`
		FilePaths            []string        `json:"file_paths"`
		FilePathsCamel       []string        `json:"filePaths"`
		EditedFilepaths      []string        `json:"edited_filepaths"`
		EditedFilepathsCamel []string        `json:"editedFilepaths"`
		Files                []string        `json:"files"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Event{}, false, fmt.Errorf("decode %s hook input: %w", agent, err)
	}
	shell := isShellOperation(
		payload.HookEventName,
		payload.HookEventNameCamel,
		payload.EventName,
		payload.EventNameCamel,
		payload.ToolName,
		payload.ToolNameCamel,
	)
	modelName := payload.Model
	if modelName == "" {
		modelName = firstValue(payload.ModelName, payload.ModelNameCamel, "unknown")
	}
	session := firstValue(
		payload.SessionID,
		payload.SessionIDCamel,
		payload.ConversationID,
		payload.ConversationIDCamel,
		payload.ThreadID,
		payload.ThreadIDCamel,
	)
	eventID := firstValue(
		payload.ID, payload.EventID, payload.EventIDCamel,
		payload.ToolUseID, payload.ToolUseIDCamel,
		payload.ToolCallID, payload.ToolCallIDCamel,
	)
	if shell {
		return shellEvent(agent, explicit, modelName, session, eventID)
	}
	if readOnlyOperation(payload.HookEventName) ||
		readOnlyOperation(payload.HookEventNameCamel) ||
		readOnlyOperation(payload.EventName) ||
		readOnlyOperation(payload.EventNameCamel) ||
		readOnlyOperation(firstValue(payload.ToolName, payload.ToolNameCamel)) {
		return Event{}, false, nil
	}
	paths := []string{payload.FilePath, payload.FilePathCamel, payload.File, payload.Path}
	paths = append(paths, payload.FilePaths...)
	paths = append(paths, payload.FilePathsCamel...)
	paths = append(paths, payload.EditedFilepaths...)
	paths = append(paths, payload.EditedFilepathsCamel...)
	paths = append(paths, payload.Files...)
	for _, raw := range []json.RawMessage{payload.ToolInput, payload.Arguments, payload.ToolArgs} {
		if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			continue
		}
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte{'"'}) {
			var encoded string
			if err := json.Unmarshal(raw, &encoded); err != nil {
				return Event{}, false, fmt.Errorf("decode hook argument string: %w", err)
			}
			raw = json.RawMessage(encoded)
		}
		var input struct {
			FilePath             string   `json:"file_path"`
			FilePathCamel        string   `json:"filePath"`
			File                 string   `json:"file"`
			Path                 string   `json:"path"`
			Patch                string   `json:"patch"`
			FilePaths            []string `json:"file_paths"`
			FilePathsCamel       []string `json:"filePaths"`
			EditedFilepaths      []string `json:"edited_filepaths"`
			EditedFilepathsCamel []string `json:"editedFilepaths"`
			Files                []string `json:"files"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return Event{}, false, fmt.Errorf("decode hook arguments: %w", err)
		}
		paths = append(paths, input.FilePath, input.FilePathCamel, input.File, input.Path)
		paths = append(paths, input.FilePaths...)
		paths = append(paths, input.FilePathsCamel...)
		paths = append(paths, input.EditedFilepaths...)
		paths = append(paths, input.EditedFilepathsCamel...)
		paths = append(paths, input.Files...)
		if input.Patch != "" {
			patchFiles, err := patchPaths(input.Patch)
			if err != nil && len(uniquePaths(paths)) == 0 {
				return Event{}, false, err
			}
			if err == nil {
				paths = append(paths, patchFiles...)
			}
		}
	}
	paths = uniquePaths(paths)
	if len(paths) == 0 {
		return Event{}, false, nil
	}
	event := Event{Kind: model.CheckpointKindEdit, Type: explicit, Paths: paths, EventID: eventID}
	if explicit == model.AuthorAI {
		event.Agent = agent
		event.Model = modelName
		event.Session = session
		if err := model.ValidateAttribution(model.Attribution{
			Author: event.Type, Agent: event.Agent, Model: event.Model, Session: event.Session,
		}); err != nil {
			return Event{}, false, err
		}
	}
	return event, true, nil
}

func shellEvent(agent string, explicit model.Author, modelName, session, eventID string) (Event, bool, error) {
	if explicit != model.AuthorHuman && explicit != model.AuthorAI {
		return Event{}, false, errors.New("explicit type must be human or ai")
	}
	if err := model.ValidateEventID(eventID); err != nil {
		return Event{}, false, err
	}
	event := Event{
		Kind:    model.CheckpointKindShellPre,
		Type:    explicit,
		EventID: eventID,
	}
	if explicit == model.AuthorAI {
		if modelName == "" {
			modelName = "unknown"
		}
		event.Kind = model.CheckpointKindShellPost
		event.Agent = agent
		event.Model = modelName
		event.Session = session
		if err := model.ValidateAttribution(model.Attribution{
			Author: event.Type, Agent: event.Agent, Model: event.Model, Session: event.Session,
		}); err != nil {
			return Event{}, false, err
		}
	}
	return event, true, nil
}

func isShellOperation(values ...string) bool {
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.ReplaceAll(value, "-", "_")
		value = strings.ReplaceAll(value, " ", "_")
		if value == "" {
			continue
		}
		switch value {
		case "bash", "shell", "terminal", "command", "run", "exec", "execute",
			"run_command", "execute_command", "shell_command", "terminal_command",
			"run_shell_command", "execute_shell_command", "pre_run_command",
			"post_run_command", "pre_execute_command", "post_execute_command":
			return true
		}
		if strings.Contains(value, "bash") || strings.Contains(value, "command") ||
			strings.Contains(value, "shell") || strings.Contains(value, "terminal") {
			return true
		}
	}
	return false
}

func firstValue(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func readOnlyOperation(value string) bool {
	value = strings.ToLower(value)
	if value == "" {
		return false
	}
	for _, edit := range []string{"write", "edit", "patch", "replace", "create", "delete", "move", "rename"} {
		if strings.Contains(value, edit) {
			return false
		}
	}
	for _, read := range []string{"read", "view", "search", "find", "list", "run", "execute", "mcp"} {
		if strings.Contains(value, read) {
			return true
		}
	}
	return false
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
		case strings.HasPrefix(line, "--- "):
			path = unifiedDiffPath(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			path = unifiedDiffPath(strings.TrimPrefix(line, "+++ "))
		default:
			continue
		}
		if path == "" {
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

func unifiedDiffPath(value string) string {
	value, _, _ = strings.Cut(value, "\t")
	value = strings.TrimSpace(value)
	if value == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(value, "a/") || strings.HasPrefix(value, "b/") {
		value = value[2:]
	}
	return value
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		path = filepath.Clean(path)
		if path == "." {
			continue
		}
		if seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
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
