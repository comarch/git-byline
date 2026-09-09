package preset

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mrwogu/git-byline/internal/model"
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
	} {
		if _, _, err := Parse("agent-v1", "", strings.NewReader(invalid)); err == nil {
			t.Fatalf("Parse accepted %s", invalid)
		}
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
