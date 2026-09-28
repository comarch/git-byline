package model

import (
	"strings"
	"testing"
)

func TestValidateCheckpointUsage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		usage CheckpointUsage
		valid bool
	}{
		{name: "empty", usage: CheckpointUsage{}, valid: true},
		{
			name: "valid",
			usage: CheckpointUsage{
				MsgID:      "msg_a",
				TokensIn:   1,
				TokensOut:  2,
				CacheRead:  3,
				CacheWrite: 4,
			},
			valid: true,
		},
		{name: "max tokens", usage: CheckpointUsage{TokensIn: MaxCheckpointUsageTokens}, valid: true},
		{name: "over max tokens", usage: CheckpointUsage{TokensOut: MaxCheckpointUsageTokens + 1}},
		{name: "msg id too long", usage: CheckpointUsage{MsgID: strings.Repeat("a", MaxCheckpointMsgIDBytes+1)}},
		{name: "msg id reserved separator", usage: CheckpointUsage{MsgID: "a::b"}},
		{name: "msg id control character", usage: CheckpointUsage{MsgID: "a\x01b"}},
		{name: "msg id invalid utf8", usage: CheckpointUsage{MsgID: "a\xffb"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCheckpointUsage(tt.usage)
			if tt.valid && err != nil {
				t.Fatalf("ValidateCheckpointUsage(%+v) = %v", tt.usage, err)
			}
			if !tt.valid && err == nil {
				t.Fatalf("ValidateCheckpointUsage(%+v) succeeded, want error", tt.usage)
			}
		})
	}
}

func TestNoteSessionHasTokenUsage(t *testing.T) {
	t.Parallel()
	if (NoteSession{}).HasTokenUsage() {
		t.Fatal("empty session reports token usage")
	}
	if !(NoteSession{CacheRead: 1}).HasTokenUsage() {
		t.Fatal("nonzero session reports no token usage")
	}
}
