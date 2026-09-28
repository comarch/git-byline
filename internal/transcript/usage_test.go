package transcript

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type resolveUsageCase struct {
	name       string
	transcript string
	wantErr    bool
	wantFound  bool
	want       Usage
}

func TestResolveUsage(t *testing.T) {
	t.Parallel()
	claudeTurn := `{"type":"assistant","message":{"id":"msg_a","model":"claude-a","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}}`
	claudeNewer := `{"type":"assistant","message":{"id":"msg_b","model":"claude-a","usage":{"input_tokens":1,"output_tokens":2,"cache_creation_input_tokens":3,"cache_read_input_tokens":4}}}`
	droidEntry := `{"message":{"role":"assistant","modelId":"droid-a"}}`

	tests := []resolveUsageCase{
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
			checkResolveUsage(t, tt)
		})
	}
}

func checkResolveUsage(t *testing.T, test resolveUsageCase) {
	t.Helper()
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if test.transcript != "" {
		if err := os.WriteFile(path, []byte(test.transcript), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := ResolveUsage(path)
	if test.wantErr {
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
	if found != test.wantFound {
		t.Fatalf("ResolveUsage(%q) found = %v, want %v", path, found, test.wantFound)
	}
	if got != test.want {
		t.Fatalf("ResolveUsage(%q) = %+v, want %+v", path, got, test.want)
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

func TestResolveUsageDropsPartialOversizedTailRecord(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	partial := strings.Repeat("x", tailWindowBytes+1024) + "\n"
	latest := `{"message":{"id":"msg_latest","usage":{"input_tokens":9,"output_tokens":8}}}` + "\n"
	if err := os.WriteFile(path, []byte(partial+latest), 0o600); err != nil {
		t.Fatal(err)
	}
	got, found, err := ResolveUsage(path)
	if err != nil {
		t.Fatalf("ResolveUsage error = %v", err)
	}
	if !found || got.MsgID != "msg_latest" || got.TokensIn != 9 || got.TokensOut != 8 {
		t.Fatalf("ResolveUsage = %+v, found %v, want msg_latest 9/8", got, found)
	}
}

func TestTranscriptTailStartsAtRecordBoundary(t *testing.T) {
	t.Parallel()
	data := append([]byte{'\n'}, bytes.Repeat([]byte{'x'}, tailWindowBytes)...)
	reader, err := transcriptTail(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("transcriptTail error = %v", err)
	}
	if _, err := io.ReadAll(reader); err != nil {
		t.Fatalf("read transcript tail: %v", err)
	}
}

func TestTranscriptTailReadErrors(t *testing.T) {
	t.Parallel()
	if _, err := transcriptTail(failingReaderAt{}, tailWindowBytes+1); err == nil {
		t.Fatal("transcriptTail accepted a failed boundary read")
	}
	if _, err := transcriptTail(failingTailReaderAt{}, tailWindowBytes+1); err == nil {
		t.Fatal("transcriptTail accepted a failed tail read")
	}
}

func TestResolveUsageFileTailError(t *testing.T) {
	t.Parallel()
	file, err := os.CreateTemp(t.TempDir(), "session.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	_, _, err = resolveUsageFile(file, tailWindowBytes+1, "session.jsonl",
		func(io.ReaderAt, int64) (io.Reader, error) {
			return nil, errors.New("tail failed")
		})
	if err == nil || !strings.Contains(err.Error(), "read transcript") {
		t.Fatalf("resolveUsageFile error = %v, want read transcript error", err)
	}
}

type failingReaderAt struct{}

func (failingReaderAt) ReadAt([]byte, int64) (int, error) {
	return 0, errors.New("boundary read failed")
}

type failingTailReaderAt struct{}

func (failingTailReaderAt) ReadAt(data []byte, offset int64) (int, error) {
	if offset == 0 {
		data[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("tail read failed")
}

func TestResolveUsageInvalidPath(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "relative.jsonl"} {
		if _, _, err := ResolveUsage(path); err == nil {
			t.Fatalf("ResolveUsage(%q) succeeded, want error", path)
		}
	}
}

func TestResolveUsageRejectsOversizedLine(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", maxLineBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := ResolveUsage(path); err == nil || found {
		t.Fatalf("ResolveUsage(oversized line) = found %t, error %v", found, err)
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
