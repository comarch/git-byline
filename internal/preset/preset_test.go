package preset

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

type presetReadError struct{}

func (presetReadError) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func TestParseToolHooks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		preset  string
		author  model.Author
		payload string
		handled bool
		paths   int
		wantErr bool
	}{
		{
			name: "droid edit", preset: "droid", author: model.AuthorAI, handled: true, paths: 1,
			payload: `{"session_id":"session-1","tool_name":"Edit","model":"test-model","tool_input":{"file_path":"a.go"}}`,
		},
		{
			name: "droid shell pre", preset: "droid", author: model.AuthorHuman, handled: true,
			payload: `{"tool_name":"Bash","model":"test-model"}`,
		},
		{
			name: "claude shell post", preset: "claude", author: model.AuthorAI, handled: true,
			payload: `{"session_id":"session-1","tool_name":"Bash","model":"test-model"}`,
		},
		{
			name: "qualified droid create", preset: "droid", author: model.AuthorHuman, handled: true, paths: 1,
			payload: `{"tool_name":"functions.Create","tool_input":{"file_path":"a.go"}}`,
		},
		{
			name: "droid patch", preset: "droid", author: model.AuthorAI, handled: true, paths: 2,
			payload: `{"tool_name":"ApplyPatch","tool_input":{"patch":"*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** End Patch\n"}}`,
		},
		{
			name: "droid patch in input field", preset: "droid", author: model.AuthorAI, handled: true, paths: 1,
			payload: `{"tool_name":"ApplyPatch","tool_input":{"input":"*** Begin Patch\n*** Update File: a.go\n*** End Patch\n"}}`,
		},
		{
			name: "droid patch without body", preset: "droid", author: model.AuthorAI,
			payload: `{"tool_name":"ApplyPatch","tool_input":{"content":"x"}}`, wantErr: true,
		},
		{
			name: "unified patch", preset: "portable-copilot", author: model.AuthorAI, handled: true, paths: 1,
			payload: `{"toolName":"apply_patch","toolArgs":{"patch":"--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n"}}`,
		},
		{
			name: "claude write", preset: "claude", author: model.AuthorAI, handled: true, paths: 1,
			payload: `{"session_id":"session-1","tool_name":"Write","tool_input":{"file_path":"a.go"}}`,
		},
		{
			name: "unrelated event", preset: "droid", author: model.AuthorAI,
			payload: `{"tool_name":"Read","tool_input":{"file_path":"a.go"}}`,
		},
		{
			name: "missing type", preset: "droid", payload: `{}`, wantErr: true,
		},
		{
			name: "bad patch", preset: "droid", author: model.AuthorAI,
			payload: `{"tool_name":"ApplyPatch","tool_input":{"patch":"bad"}}`, wantErr: true,
		},
		{
			name: "unknown preset", preset: "other", author: model.AuthorAI, payload: `{}`, wantErr: true,
		},
		{
			name: "control in model", preset: "droid", author: model.AuthorAI,
			payload: `{"tool_name":"Edit","model":"bad\nmodel","tool_input":{"file_path":"a.go"}}`, wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event, handled, err := Parse(test.preset, test.author, strings.NewReader(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr = %t", err, test.wantErr)
			}
			if err == nil && handled != test.handled {
				t.Fatalf("handled = %t, want %t", handled, test.handled)
			}
			if err == nil && len(event.Paths) != test.paths {
				t.Fatalf("paths = %v, want %d", event.Paths, test.paths)
			}
			if err == nil && strings.Contains(test.name, "shell") {
				wantKind := model.CheckpointKindShellPre
				if test.author == model.AuthorAI {
					wantKind = model.CheckpointKindShellPost
				}
				if event.Kind != wantKind || len(event.Paths) != 0 {
					t.Fatalf("shell event = %+v, want kind %q without paths", event, wantKind)
				}
			}
		})
	}
}

func TestParseToolHookValidationBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		handled bool
		wantErr bool
	}{
		{
			name:    "invalid JSON",
			payload: `{`,
			wantErr: true,
		},
		{
			name:    "camel case tool input",
			payload: `{"toolName":"Edit","toolInput":{"file_path":"camel.go"}}`,
			handled: true,
		},
		{
			name:    "missing tool input",
			payload: `{"tool_name":"Edit"}`,
			wantErr: true,
		},
		{
			name:    "invalid tool input JSON type",
			payload: `{"tool_name":"Edit","tool_input":"not-an-object"}`,
			wantErr: true,
		},
		{
			name:    "empty file path",
			payload: `{"tool_name":"Edit","tool_input":{"file_path":""}}`,
			wantErr: true,
		},
		{
			name:    "only whitespace file path",
			payload: `{"tool_name":"Edit","tool_input":{"file_path":"   "}}`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, handled, err := Parse("droid", model.AuthorAI, strings.NewReader(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr = %t", err, test.wantErr)
			}
			if err == nil && handled != test.handled {
				t.Fatalf("handled = %t, want %t", handled, test.handled)
			}
		})
	}
}

func TestParseTranscriptPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		preset  string
		author  model.Author
		payload string
		want    string
	}{
		{
			name:    "droid edit",
			preset:  "droid",
			author:  model.AuthorAI,
			payload: `{"tool_name":"Edit","transcript_path":"/tmp/s.jsonl","tool_input":{"file_path":"a.go"}}`,
			want:    "/tmp/s.jsonl",
		},
		{
			name:    "droid shell",
			preset:  "droid",
			author:  model.AuthorAI,
			payload: `{"tool_name":"Bash","transcript_path":"/tmp/s.jsonl"}`,
			want:    "/tmp/s.jsonl",
		},
		{
			name:    "claude write",
			preset:  "claude",
			author:  model.AuthorAI,
			payload: `{"tool_name":"Write","transcript_path":"/tmp/s.jsonl","tool_input":{"file_path":"a.go"}}`,
			want:    "/tmp/s.jsonl",
		},
		{
			name:    "portable claude",
			preset:  "portable-claude",
			author:  model.AuthorAI,
			payload: `{"hook_event_name":"Write","transcript_path":"/tmp/s.jsonl","tool_input":{"file_path":"a.go"}}`,
			want:    "/tmp/s.jsonl",
		},
		{
			name:    "human events keep it too",
			preset:  "droid",
			author:  model.AuthorHuman,
			payload: `{"tool_name":"Edit","transcript_path":"/tmp/s.jsonl","tool_input":{"file_path":"a.go"}}`,
			want:    "/tmp/s.jsonl",
		},
		{
			name:    "absent field stays empty",
			preset:  "droid",
			author:  model.AuthorAI,
			payload: `{"tool_name":"Edit","tool_input":{"file_path":"a.go"}}`,
			want:    "",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event, handled, err := Parse(test.preset, test.author, strings.NewReader(test.payload))
			if err != nil || !handled {
				t.Fatalf("Parse() = %+v, %t, %v", event, handled, err)
			}
			if event.TranscriptPath != test.want {
				t.Fatalf("TranscriptPath = %q, want %q", event.TranscriptPath, test.want)
			}
		})
	}
}

type windsurfCase struct {
	name      string
	author    model.Author
	payload   string
	handled   bool
	wantKind  string
	wantPaths int
}

func TestParseWindsurfEvents(t *testing.T) {
	t.Parallel()
	tests := []windsurfCase{
		{
			name:     "write post captures the edited path",
			author:   model.AuthorAI,
			payload:  `{"agent_action_name":"post_write_code","trajectory_id":"traj-1","execution_id":"exec-1","timestamp":"2026-09-13T22:00:00Z","model_name":"Claude Sonnet 4","tool_info":{"file_path":"a.go","edits":[{"old_string":"a","new_string":"b"}]}}`,
			handled:  true,
			wantKind: model.CheckpointKindEdit, wantPaths: 1,
		},
		{
			name:     "write pre captures the edited path",
			author:   model.AuthorHuman,
			payload:  `{"agent_action_name":"pre_write_code","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"file_path":"a.go"}}`,
			handled:  true,
			wantKind: model.CheckpointKindEdit, wantPaths: 1,
		},
		{
			name:     "run pre is a shell event",
			author:   model.AuthorHuman,
			payload:  `{"agent_action_name":"pre_run_command","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"command_line":"go test ./...","cwd":"/repo"}}`,
			handled:  true,
			wantKind: model.CheckpointKindShellPre,
		},
		{
			name:     "run post is a shell event",
			author:   model.AuthorAI,
			payload:  `{"agent_action_name":"post_run_command","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"command_line":"go test ./...","cwd":"/repo"}}`,
			handled:  true,
			wantKind: model.CheckpointKindShellPost,
		},
		{
			name:    "read is ignored",
			author:  model.AuthorAI,
			payload: `{"agent_action_name":"pre_read_code","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"file_path":"a.go"}}`,
			handled: false,
		},
		{
			name:    "mcp tool use is ignored",
			author:  model.AuthorAI,
			payload: `{"agent_action_name":"pre_mcp_tool_use","trajectory_id":"traj-1","model_name":"Claude Sonnet 4","tool_info":{"mcp_server_name":"github","mcp_tool_name":"create_issue","mcp_tool_arguments":{"owner":"o","repo":"r"}}}`,
			handled: false,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			runWindsurfCase(t, test)
		})
	}
}

func runWindsurfCase(t *testing.T, test windsurfCase) {
	t.Helper()
	event, handled, err := Parse("portable-windsurf", test.author, strings.NewReader(test.payload))
	if err != nil {
		t.Fatal(err)
	}
	if handled != test.handled {
		t.Fatalf("handled = %t, want %t", handled, test.handled)
	}
	if !handled {
		return
	}
	if event.Kind != test.wantKind || len(event.Paths) != test.wantPaths {
		t.Fatalf("event = %+v, want kind %q with %d paths", event, test.wantKind, test.wantPaths)
	}
	if test.author != model.AuthorAI {
		return
	}
	if event.Agent != "windsurf" || event.Model != "Claude Sonnet 4" || event.Session != "traj-1" {
		t.Fatalf("attribution = %+v", event)
	}
}

func TestParseAgentV1(t *testing.T) {
	t.Parallel()
	payload := `{"type":"ai_agent","agent_name":"other","model":"m","conversation_id":"c","edited_filepaths":["a.go","a.go","b.go"]}`
	event, handled, err := Parse("agent-v1", "", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !handled || event.Type != model.AuthorAI || event.Agent != "other" || len(event.Paths) != 2 {
		t.Fatalf("event = %+v, handled = %t", event, handled)
	}
	if _, _, err := Parse("agent-v1", model.AuthorAI, strings.NewReader(payload)); err == nil {
		t.Fatal("agent-v1 accepted an explicit type")
	}
	for _, invalid := range []string{
		`{"type":"other","agent_name":"a","edited_filepaths":["a"]}`,
		`{"type":"human","edited_filepaths":["a"]}`,
		`{"type":"human","agent_name":"a","edited_filepaths":[]}`,
		`{"type":"human","agent_name":"a","edited_filepaths":[""]}`,
	} {
		if _, _, err := Parse("agent-v1", "", strings.NewReader(invalid)); err == nil {
			t.Fatalf("Parse accepted %s", invalid)
		}
	}
	for _, test := range []struct {
		name string
		kind string
		want model.Author
	}{
		{"shell pre", model.CheckpointKindShellPre, model.AuthorHuman},
		{"shell post", model.CheckpointKindShellPost, model.AuthorAI},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload := `{"type":"` + test.kind + `","agent_name":"agent","model":"model","conversation_id":"session"}`
			event, handled, err := Parse("agent-v1", "", strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			if !handled || event.Kind != test.kind || event.Type != test.want || len(event.Paths) != 0 {
				t.Fatalf("event = %+v, handled = %t", event, handled)
			}
			if test.want == model.AuthorAI &&
				(event.Agent != "agent" || event.Model != "model" || event.Session != "session") {
				t.Fatalf("shell post metadata = %+v", event)
			}
		})
	}
}

func TestParseAgentV1ValidationBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		wantErr bool
		check   func(*testing.T, Event)
	}{
		{
			name:    "invalid JSON",
			payload: `{`,
			wantErr: true,
		},
		{
			name:    "shell post defaults model",
			payload: `{"type":"shell_post","agent_name":"agent","conversation_id":"session"}`,
			check: func(t *testing.T, event Event) {
				if event.Kind != model.CheckpointKindShellPost || event.Model != FallbackModel {
					t.Fatalf("event = %+v", event)
				}
			},
		},
		{
			name:    "shell post rejects reserved agent separator",
			payload: `{"type":"shell_post","agent_name":"bad::agent","conversation_id":"session"}`,
			wantErr: true,
		},
		{
			name:    "edit rejects reserved agent separator",
			payload: `{"type":"ai_agent","agent_name":"bad::agent","edited_filepaths":["a.go"]}`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event, handled, err := Parse("agent-v1", "", strings.NewReader(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr = %t", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if err != nil || !handled {
				t.Fatalf("Parse() = %+v, %t, %v", event, handled, err)
			}
			test.check(t, event)
		})
	}
}

func TestParsePortableHooks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
		paths   []string
	}{
		{"factory", `{"session_id":"s","model":"m","tool_input":{"file_path":"a.go"}}`, []string{"a.go"}},
		{"vscode", `{"thread_id":"t","toolName":"replace_string_in_file","tool_input":{"filePath":"b.go"}}`, []string{"b.go"}},
		{"cursor", `{"file_path":"c.go"}`, []string{"c.go"}},
		{"codex", `{"tool_name":"write_file","arguments":{"file_path":"d.go"}}`, []string{"d.go"}},
		{"gemini", `{"file":"e.go"}`, []string{"e.go"}},
		{"windsurf", `{"hook_event_name":"pre_write_code","path":"f.go"}`, []string{"f.go"}},
		{"grok", `{"file_paths":["g.go","h.go"]}`, []string{"g.go", "h.go"}},
		{"copilot", `{"sessionId":"s","toolName":"apply_patch","toolArgs":"{\"filePath\":\"i.go\"}"}`, []string{"i.go"}},
		{"claude", `{"conversation_id":"c","edited_filepaths":["j.go"]}`, []string{"j.go"}},
		{"factory", `{"tool_name":"ApplyPatch","model":"m","tool_input":{"input":"*** Begin Patch\n*** Update File: k.go\n*** End Patch\n"}}`, []string{"k.go"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			event, handled, err := Parse("portable-"+test.name, model.AuthorAI, strings.NewReader(test.payload))
			if err != nil {
				t.Fatal(err)
			}
			if !handled || event.Agent != test.name || event.Model == "" {
				t.Fatalf("event = %+v, handled = %t", event, handled)
			}
			if strings.Join(event.Paths, ",") != strings.Join(test.paths, ",") {
				t.Fatalf("paths = %v, want %v", event.Paths, test.paths)
			}
		})
	}
	t.Run("patch-like input outside ApplyPatch is not a patch body", func(t *testing.T) {
		t.Parallel()
		payload := `{"toolName":"write_notes","toolArgs":{"input":"*** Begin Patch\n*** Update File: stray.go\n*** End Patch\n"}}`
		event, handled, err := Parse("portable-factory", model.AuthorAI, strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		if handled || len(event.Paths) != 0 {
			t.Fatalf("event = %+v, handled = %t", event, handled)
		}
	})
	t.Run("applypatch spellings use input as patch body", func(t *testing.T) {
		t.Parallel()
		for tool, path := range map[string]string{
			"apply_patch":        "snake.go",
			"factory.ApplyPatch": "qualified.go",
		} {
			tool, path := tool, path
			t.Run(tool, func(t *testing.T) {
				t.Parallel()
				payload := `{"toolName":"` + tool +
					`","toolArgs":{"input":"*** Begin Patch\n*** Update File: ` + path + `\n*** End Patch\n"}}`
				event, handled, err := Parse("portable-factory", model.AuthorAI, strings.NewReader(payload))
				if err != nil {
					t.Fatal(err)
				}
				if !handled || len(event.Paths) != 1 || event.Paths[0] != path {
					t.Fatalf("event = %+v, handled = %t", event, handled)
				}
			})
		}
	})
	t.Run("applypatch without body fails", func(t *testing.T) {
		t.Parallel()
		payload := `{"tool_name":"ApplyPatch","tool_input":{"foo":"bar"}}`
		event, handled, err := Parse("portable-factory", model.AuthorAI, strings.NewReader(payload))
		if err == nil || handled || len(event.Paths) != 0 {
			t.Fatalf("event = %+v, handled = %t, error = %v", event, handled, err)
		}
	})
	t.Run("factory applypatch validates body with a path", func(t *testing.T) {
		t.Parallel()
		for _, payload := range []string{
			`{"tool_name":"ApplyPatch","tool_input":{"filePath":"a.go"}}`,
			`{"tool_name":"ApplyPatch","tool_input":{"filePath":"a.go","input":"bad"}}`,
		} {
			event, handled, err := Parse("portable-factory", model.AuthorAI, strings.NewReader(payload))
			if err == nil || handled || len(event.Paths) != 0 {
				t.Fatalf("payload = %s, event = %+v, handled = %t, error = %v", payload, event, handled, err)
			}
		}
	})
	for _, agent := range portableAgents {
		agent := agent
		t.Run("shell-"+agent, func(t *testing.T) {
			t.Parallel()
			payload := `{"toolName":"run_command","model":"model","conversationId":"session"}`
			event, handled, err := Parse("portable-"+agent, model.AuthorAI, strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			if !handled || event.Kind != model.CheckpointKindShellPost || len(event.Paths) != 0 ||
				event.Agent != agent || event.Model != "model" || event.Session != "session" {
				t.Fatalf("shell event = %+v, handled = %t", event, handled)
			}
		})
	}
	for _, agent := range []string{"vscode", "windsurf"} {
		agent := agent
		t.Run("path-only-"+agent, func(t *testing.T) {
			t.Parallel()
			event, handled, err := Parse("portable-"+agent, model.AuthorAI,
				strings.NewReader(`{"path":"path-only.go"}`))
			if err != nil {
				t.Fatal(err)
			}
			if !handled || event.Kind != model.CheckpointKindEdit ||
				len(event.Paths) != 1 || event.Paths[0] != "path-only.go" {
				t.Fatalf("path-only event = %+v, handled = %t", event, handled)
			}
		})
	}
	if _, _, err := Parse("portable-other", model.AuthorAI, strings.NewReader(`{"file":"a"}`)); err == nil {
		t.Fatal("Parse accepted an unknown portable agent")
	}
	if _, handled, err := Parse("portable-factory", model.AuthorAI, strings.NewReader(`{"files":[""]}`)); err != nil || handled {
		t.Fatalf("empty portable paths = handled %t, error %v", handled, err)
	}
	if _, handled, err := Parse("portable-windsurf", model.AuthorAI,
		strings.NewReader(`{"hook_event_name":"post_read_code","file_path":"a.go"}`)); err != nil || handled {
		t.Fatalf("read-only portable event = handled %t, error %v", handled, err)
	}
}

func TestParsePortableValidationBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		author  model.Author
		payload string
		wantErr bool
	}{
		{
			name:    "encoded argument is malformed JSON",
			payload: `{"toolArgs":"\""}`,
			wantErr: true,
		},
		{
			name:    "encoded argument is not an object",
			payload: `{"toolArgs":"not-json"}`,
			wantErr: true,
		},
		{
			name:    "non-factory ApplyPatch requires body",
			payload: `{"toolName":"apply_patch"}`,
			wantErr: true,
		},
		{
			name:    "invalid model metadata",
			payload: `{"model":"bad\nmodel","file_path":"a.go"}`,
			wantErr: true,
		},
		{
			name:    "invalid explicit type",
			author:  model.Author("other"),
			payload: `{"file_path":"a.go"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			payload: `{`,
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			author := test.author
			if author == "" {
				author = model.AuthorAI
			}
			_, _, err := Parse("portable-copilot", author, strings.NewReader(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("Parse() error = %v, wantErr = %t", err, test.wantErr)
			}
		})
	}
}

func TestOperationHelperFallbacks(t *testing.T) {
	t.Parallel()
	if !isShellOperation("custom terminal runner") {
		t.Fatal("isShellOperation missed a shell substring")
	}
	if readOnlyOperation("noop") {
		t.Fatal("readOnlyOperation classified an unrelated operation as read-only")
	}
}

func TestShellEventIdentifier(t *testing.T) {
	t.Parallel()
	event, handled, err := Parse("droid", model.AuthorAI,
		strings.NewReader(`{"tool_name":"Bash","tool_call_id":"call-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !handled || event.EventID != "call-1" || event.Kind != model.CheckpointKindShellPost {
		t.Fatalf("shell event = %+v, handled = %t", event, handled)
	}
}

func TestShellEventValidationBranches(t *testing.T) {
	t.Parallel()
	if _, _, err := shellEvent("agent", model.Author("other"), "", "", ""); err == nil {
		t.Fatal("shellEvent accepted an invalid author")
	}
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid event identifier",
			payload: `{"tool_name":"Bash","event_id":"bad\nid"}`,
		},
		{
			name:    "invalid model metadata",
			payload: `{"tool_name":"Bash","model":"bad\nmodel"}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := Parse("droid", model.AuthorAI, strings.NewReader(test.payload))
			if err == nil {
				t.Fatalf("Parse accepted %s", test.name)
			}
		})
	}
}

func TestParseInputErrors(t *testing.T) {
	t.Parallel()
	if _, _, err := Parse("droid", model.AuthorAI, nil); err == nil {
		t.Fatal("Parse accepted nil input")
	}
	if _, _, err := Parse("droid", model.AuthorAI, strings.NewReader(" ")); err == nil {
		t.Fatal("Parse accepted empty input")
	}
	large := bytes.Repeat([]byte{'x'}, MaxInputBytes+1)
	if _, _, err := Parse("droid", model.AuthorAI, bytes.NewReader(large)); err == nil {
		t.Fatal("Parse accepted oversized input")
	}
}

func TestPatchPathBranches(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		patch   string
		paths   []string
		wantErr bool
	}{
		{
			name:  "add and delete operations",
			patch: "*** Add File: add.go\n*** Delete File: remove.go",
			paths: []string{"add.go", "remove.go"},
		},
		{
			name:  "dev null and prefixed unified path",
			patch: "--- /dev/null\n+++ b/new.go",
			paths: []string{"new.go"},
		},
		{
			name:    "empty operation path",
			patch:   "*** Add File:    ",
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			paths, err := patchPaths(test.patch)
			if (err != nil) != test.wantErr {
				t.Fatalf("patchPaths() error = %v, wantErr = %t", err, test.wantErr)
			}
			if err == nil && strings.Join(paths, ",") != strings.Join(test.paths, ",") {
				t.Fatalf("paths = %v, want %v", paths, test.paths)
			}
		})
	}
}

func TestPathAndReaderHelperBranches(t *testing.T) {
	t.Parallel()
	paths := uniquePaths([]string{" ", ".", "./a", "a", "b"})
	if strings.Join(paths, ",") != "a,b" {
		t.Fatalf("uniquePaths() = %v", paths)
	}
	if _, err := readBounded(presetReadError{}); err == nil {
		t.Fatal("readBounded accepted a failing reader")
	}
}

func FuzzParseDroid(f *testing.F) {
	f.Add([]byte(`{"tool_name":"Edit","tool_input":{"file_path":"a.go"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = Parse("droid", model.AuthorAI, bytes.NewReader(data))
	})
}

func FuzzParsePortable(f *testing.F) {
	f.Add([]byte(`{"toolName":"apply_patch","toolArgs":{"filePath":"a.go"}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = Parse("portable-copilot", model.AuthorAI, bytes.NewReader(data))
	})
}
