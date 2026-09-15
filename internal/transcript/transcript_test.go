package transcript

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModel(t *testing.T) {
	t.Parallel()
	droidEntry := `{"type":"message","id":"1","message":{"role":"assistant","modelId":"droid-a"}}`
	droidEntryNew := `{"type":"message","id":"2","message":{"role":"assistant","modelId":"droid-b"}}`
	claudeEntry := `{"type":"assistant","message":{"role":"assistant","model":"claude-a"}}`

	tests := []struct {
		name       string
		transcript string
		sidecar    string
		want       string
	}{
		{
			name:       "droid transcript",
			transcript: droidEntry + "\n" + droidEntryNew + "\n",
			want:       "droid-b",
		},
		{
			name:       "claude transcript",
			transcript: claudeEntry + "\n",
			want:       "claude-a",
		},
		{
			name:       "garbage lines are skipped",
			transcript: "not json\n" + claudeEntry + "\n{\"type\":\"other\"}\n",
			want:       "claude-a",
		},
		{
			name:       "entries without a model resolve to empty",
			transcript: "{\"message\":{\"role\":\"user\"}}\n{\"type\":\"session_start\"}\n",
			want:       "",
		},
		{
			name:       "sidecar fallback",
			transcript: "{\"message\":{\"role\":\"user\"}}\n",
			sidecar:    `{"model":"sidecar-model"}`,
			want:       "sidecar-model",
		},
		{
			name:       "transcript wins over sidecar",
			transcript: droidEntry + "\n",
			sidecar:    `{"model":"sidecar-model"}`,
			want:       "droid-a",
		},
		{
			name: "missing files resolve to empty",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "session.jsonl")
			if tt.transcript != "" {
				if err := os.WriteFile(path, []byte(tt.transcript), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.sidecar != "" {
				if err := os.WriteFile(filepath.Join(dir, "session.settings.json"), []byte(tt.sidecar), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := ResolveModel(path)
			if err != nil {
				t.Fatalf("ResolveModel(%q) error = %v", path, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveModel(%q) = %q, want %q", path, got, tt.want)
			}
		})
	}
}

func TestResolveModelWindowKeepsLatestModel(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var b strings.Builder
	b.WriteString(`{"message":{"role":"assistant","modelId":"droid-a"}}` + "\n")
	padding := strings.Repeat("{\"message\":{\"role\":\"assistant\",\"content\":\"x\"}}\n", 256)
	for b.Len() <= tailWindowBytes {
		b.WriteString(padding)
	}
	b.WriteString(`{"message":{"role":"assistant","modelId":"droid-b"}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(path)
	if err != nil {
		t.Fatalf("ResolveModel error = %v", err)
	}
	if got != "droid-b" {
		t.Fatalf("ResolveModel = %q, want droid-b", got)
	}
}

func TestResolveModelOversizedLineFallsBackToSidecar(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	var b strings.Builder
	b.WriteString(`{"message":{"role":"assistant","modelId":"stale-model"}}` + "\n")
	b.WriteString(`{"message":{"role":"assistant","content":"`)
	b.WriteString(strings.Repeat("x", maxLineBytes+64))
	b.WriteString(`"}}` + "\n")
	b.WriteString(`{"message":{"role":"assistant","modelId":"latest-model"}}` + "\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(dir, "session.settings.json")
	if err := os.WriteFile(sidecar, []byte(`{"model":"sidecar-model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(path)
	if err != nil {
		t.Fatalf("ResolveModel error = %v", err)
	}
	if got != "sidecar-model" {
		t.Fatalf("ResolveModel = %q, want sidecar fallback", got)
	}
}

func TestResolveModelInvalidPath(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "relative.jsonl"} {
		if _, err := ResolveModel(path); err == nil {
			t.Fatalf("ResolveModel(%q) succeeded, want error", path)
		}
	}
}

func TestResolveModelRejectsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(target, []byte(`{"message":{"role":"assistant","modelId":"droid-a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(link)
	if err != nil {
		t.Fatalf("ResolveModel(symlink) error = %v", err)
	}
	if got != "" {
		t.Fatalf("ResolveModel(symlink) = %q, want empty", got)
	}
}

func TestOpenVerifiedSessionFileReadsRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, info, err := openVerifiedSessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if !info.Mode().IsRegular() {
		t.Fatalf("opened session file mode = %v, want regular", info.Mode())
	}
	data, err := io.ReadAll(io.LimitReader(file, 64))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "content" {
		t.Fatalf("read %q, want %q", data, "content")
	}
}

func TestResolveModelFollowsSymlinkAncestor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(realDir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"message":{"role":"assistant","modelId":"droid-a"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(filepath.Join(link, "session.jsonl"))
	if err != nil {
		t.Fatalf("ResolveModel(symlinked ancestor) error = %v", err)
	}
	if got != "droid-a" {
		t.Fatalf("ResolveModel(symlinked ancestor) = %q, want droid-a", got)
	}
}

func TestResolveModelRejectsSidecarSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte("{\"message\":{\"role\":\"user\"}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "settings-target.json")
	if err := os.WriteFile(target, []byte(`{"model":"sidecar-model"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "session.settings.json")); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveModel(path)
	if err != nil {
		t.Fatalf("ResolveModel(sidecar symlink) error = %v", err)
	}
	if got != "" {
		t.Fatalf("ResolveModel(sidecar symlink) = %q, want empty", got)
	}
}
