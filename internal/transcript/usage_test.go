package transcript

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveUsage(t *testing.T) {
	t.Parallel()
	claudeTurn := `{"type":"assistant","message":{"id":"msg_a","model":"claude-a","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}}`
	claudeNewer := `{"type":"assistant","message":{"id":"msg_b","model":"claude-a","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}}`
	droidEntry := `{"message":{"role":"assistant","modelId":"droid-a"}}`

	tests := []struct {
		name       string
		transcript string
		wantErr    bool
		wantFound  bool
		want       Usage
	}{
		{
			name:       "claude turn",
			transcript: claudeTurn + "\n",
			wantFound:  true,
			want:       Usage{MsgID: "msg_a", TokensIn: 10, TokensOut: 20, CacheWrite: 30, CacheRead: 40},
		},
		{
			name:       "newest turn wins",
			transcript: claudeTurn + "\n" + claudeNewer + "\n",
			wantFound:  true,
			want:       Usage{MsgID: "msg_b", TokensIn: 1, TokensOut: 2, CacheWrite: 3, CacheRead: 4},
		},
		{
			name:       "usage after a newer turn without usage stays",
			transcript: claudeTurn + "\n" + droidEntry + "\n",
			wantFound:  true,
			want:       Usage{MsgID: "msg_a", TokensIn: 10, TokensOut: 20, CacheWrite: 30, CacheRead: 40},
		},
		{
			name:       "garbage lines are skipped",
			transcript: "not json\n" + claudeTurn + "\n{\"type\":\"other\"}\n",
			wantFound:  true,
			want:       Usage{MsgID: "msg_a", TokensIn: 10, TokensOut: 20, CacheWrite: 30, CacheRead: 40},
		},
		{
			name:       "droid transcript without usage resolves none",
			transcript: droidEntry + "\n",
			wantFound:  false,
		},
		{
			name:       "zero usage resolves none",
			transcript: `{"type":"assistant","message":{"id":"msg_z","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n",
			wantFound:  false,
		},
		{
			name:       "entries without usage resolve none",
			transcript: "{\"message\":{\"role\":\"user\"}}\n{\"type\":\"session_start\"}\n",
			wantFound:  false,
		},
		{
			name:      "missing file reports an error",
			wantErr:   true,
			wantFound: false,
		},
		{
			name:       "partial usage fields count",
			transcript: `{"type":"assistant","message":{"id":"msg_p","usage":{"input_tokens":7,"output_tokens":0}}}` + "\n",
			wantFound:  true,
			want:       Usage{MsgID: "msg_p", TokensIn: 7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if tt.transcript != "" {
				if err := os.WriteFile(path, []byte(tt.transcript), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, found, err := ResolveUsage(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveUsage(%q) succeeded, want error", path)
				}
				if found {
					t.Fatalf("ResolveUsage(%q) found usage alongside an error", path)
				}
				return
			}
			if err != nil {
				t.Fatalf("ResolveUsage(%q) error = %v", path, err)
			}
			if found != tt.wantFound {
				t.Fatalf("ResolveUsage(%q) found = %v, want %v", path, found, tt.wantFound)
			}
			if got != tt.want {
				t.Fatalf("ResolveUsage(%q) = %+v, want %+v", path, got, tt.want)
			}
		})
	}
}

func TestResolveUsageWindowKeepsLatestUsage(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var b strings.Builder
	b.WriteString(`{"message":{"id":"msg_old","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n")
	padding := strings.Repeat(`{"message":{"role":"assistant","content":"x"}}`+"\n", 256)
	for b.Len() <= tailWindowBytes {
		b.WriteString(padding)
	}
	b.WriteString(`{"message":{"id":"msg_new","usage":{"input_tokens":9,"output_tokens":8}}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := ResolveUsage(path)
	if err != nil {
		t.Fatalf("ResolveUsage error = %v", err)
	}
	if !found || got.MsgID != "msg_new" || got.TokensIn != 9 || got.TokensOut != 8 {
		t.Fatalf("ResolveUsage = %+v, found %v, want msg_new 9/8", got, found)
	}
}

func TestResolveUsageInvalidPath(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "relative.jsonl"} {
		if _, _, err := ResolveUsage(path); err == nil {
			t.Fatalf("ResolveUsage(%q) succeeded, want error", path)
		}
	}
}

func TestResolveUsageRejectsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(target, []byte(`{"message":{"id":"msg_a","usage":{"input_tokens":1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, found, err := ResolveUsage(link)
	if found {
		t.Fatal("ResolveUsage(symlink) found usage, want none")
	}
	if err == nil {
		t.Fatal("ResolveUsage(symlink) succeeded, want error")
	}
}
