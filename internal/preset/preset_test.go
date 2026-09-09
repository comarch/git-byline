package preset

import (
	"bytes"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

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
			name: "qualified droid create", preset: "droid", author: model.AuthorHuman, handled: true, paths: 1,
			payload: `{"tool_name":"functions.Create","tool_input":{"file_path":"a.go"}}`,
		},
		{
			name: "droid patch", preset: "droid", author: model.AuthorAI, handled: true, paths: 2,
			payload: `{"tool_name":"ApplyPatch","tool_input":{"patch":"*** Begin Patch\n*** Update File: a.go\n*** Move to: b.go\n*** End Patch\n"}}`,
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
		})
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
