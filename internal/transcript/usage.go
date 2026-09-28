package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Usage holds the token counts of one assistant turn. Only aggregate
// numbers leave the transcript; no prompts, responses, or raw content are
// retained.
type Usage struct {
	// MsgID identifies the assistant message the usage belongs to, so one
	// turn counted across several checkpoints is not summed twice.
	MsgID string
	// TokensIn is the non-cached prompt token count of the turn.
	TokensIn uint64
	// TokensOut is the completion token count of the turn.
	TokensOut uint64
	// CacheRead is the prompt token count served from the prompt cache.
	CacheRead uint64
	// CacheWrite is the prompt token count written to the prompt cache.
	CacheWrite uint64
}

type usageEntry struct {
	Message usageMessage `json:"message"`
}

type usageMessage struct {
	ID    string       `json:"id"`
	Usage *usageCounts `json:"usage"`
}

type usageCounts struct {
	InputTokens          uint64 `json:"input_tokens"`
	OutputTokens         uint64 `json:"output_tokens"`
	CacheCreationTokens  uint64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens uint64 `json:"cache_read_input_tokens"`
}

// ResolveUsage returns the usage of the newest assistant turn recorded in a
// session JSONL file. found is false when the transcript names no usage,
// which is honest absence rather than a zero guess. Droid session files
// carry model identifiers but no usage today, so their checkpoints resolve
// no usage until that changes.
func ResolveUsage(transcriptPath string) (Usage, bool, error) {
	if transcriptPath == "" {
		return Usage{}, false, fmt.Errorf("transcript path is empty")
	}
	if !filepath.IsAbs(transcriptPath) {
		return Usage{}, false, fmt.Errorf("transcript path %q is not absolute", transcriptPath)
	}
	file, info, err := openVerifiedSessionFile(transcriptPath)
	if err != nil {
		return Usage{}, false, fmt.Errorf("open transcript %s: %w", transcriptPath, err)
	}
	defer file.Close()
	return resolveUsageFile(file, info.Size(), transcriptPath, transcriptTail)
}

func resolveUsageFile(
	file *os.File,
	size int64,
	transcriptPath string,
	tail func(io.ReaderAt, int64) (io.Reader, error),
) (Usage, bool, error) {
	var reader io.Reader = io.LimitReader(file, maxTranscriptBytes+1)
	if size > tailWindowBytes {
		var err error
		reader, err = tail(file, size)
		if err != nil {
			return Usage{}, false, fmt.Errorf("read transcript %s: %w", transcriptPath, err)
		}
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	var usage Usage
	found := false
	for scanner.Scan() {
		if candidate, ok := lineUsage(scanner.Bytes()); ok {
			usage = candidate
			found = true
		}
	}
	if err := scanner.Err(); err != nil {
		return Usage{}, false, fmt.Errorf("scan transcript %s: %w", transcriptPath, err)
	}
	return usage, found, nil
}

func transcriptTail(file io.ReaderAt, size int64) (io.Reader, error) {
	offset := size - tailWindowBytes
	reader := io.NewSectionReader(file, offset, tailWindowBytes)
	var previous [1]byte
	if _, err := file.ReadAt(previous[:], offset-1); err != nil {
		return nil, err
	}
	if previous[0] == '\n' {
		return reader, nil
	}
	buffered := bufio.NewReader(reader)
	if _, err := buffered.ReadBytes('\n'); err != nil && err != io.EOF {
		return nil, err
	}
	return buffered, nil
}

// lineUsage extracts the usage of one transcript entry. Claude Code
// assistant records carry message.id and message.usage with input_tokens,
// output_tokens, cache_creation_input_tokens, and cache_read_input_tokens.
// An entry with all counts zero names no measurable turn.
func lineUsage(line []byte) (Usage, bool) {
	var entry usageEntry
	if err := json.Unmarshal(line, &entry); err != nil {
		return Usage{}, false
	}
	counts := entry.Message.Usage
	if counts == nil {
		return Usage{}, false
	}
	usage := Usage{
		MsgID:      entry.Message.ID,
		TokensIn:   counts.InputTokens,
		TokensOut:  counts.OutputTokens,
		CacheWrite: counts.CacheCreationTokens,
		CacheRead:  counts.CacheReadInputTokens,
	}
	if usage.TokensIn == 0 && usage.TokensOut == 0 &&
		usage.CacheRead == 0 && usage.CacheWrite == 0 {
		return Usage{}, false
	}
	return usage, true
}
