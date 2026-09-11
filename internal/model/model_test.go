package model

import (
	"strings"
	"testing"
)

func TestSessionKeyAndEventID(t *testing.T) {
	t.Parallel()
	if got := NoteSessionKey("droid", "session"); got != "droid::session" {
		t.Fatalf("NoteSessionKey() = %q", got)
	}
	for _, value := range []string{"", "event-1", "unicode"} {
		if err := ValidateEventID(value); err != nil {
			t.Fatalf("ValidateEventID(%q) = %v", value, err)
		}
	}
	for _, value := range []string{"bad\nid", "bad\x00id"} {
		if err := ValidateEventID(value); err == nil {
			t.Fatalf("ValidateEventID(%q) accepted invalid input", value)
		}
	}
}

func TestValidateAttribution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value Attribution
		valid bool
	}{
		{"human", Attribution{Author: AuthorHuman}, true},
		{"untracked", Attribution{Author: AuthorUntracked}, true},
		{"ai", Attribution{Author: AuthorAI, Agent: "droid"}, true},
		{"human override", Attribution{Author: AuthorHumanOverride, Agent: "droid"}, true},
		{"ai missing agent", Attribution{Author: AuthorAI}, false},
		{"human override missing agent", Attribution{Author: AuthorHumanOverride}, false},
		{"ai control character", Attribution{Author: AuthorAI, Agent: "droid\nother"}, false},
		{"ai session separator in agent", Attribution{Author: AuthorAI, Agent: "droid::other"}, false},
		{"ai oversized model", Attribution{Author: AuthorAI, Agent: "droid", Model: string(make([]byte, maxAttributionValueBytes+1))}, false},
		{"ai invalid timestamp", Attribution{Author: AuthorAI, Agent: "droid", TS: "invalid"}, false},
		{"human metadata", Attribution{Author: AuthorHuman, Agent: "droid"}, false},
		{"human override timestamp", Attribution{
			Author: AuthorHumanOverride, Agent: "droid", TS: "2026-01-02T03:04:05Z",
		}, true},
		{"unknown", Attribution{Author: "other"}, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateAttribution(test.value)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateAttribution(%+v) error = %v, valid = %t", test.value, err, test.valid)
			}
		})
	}
}

func TestCheckpointKinds(t *testing.T) {
	t.Parallel()
	if CheckpointKindEdit == CheckpointKindShellPre ||
		CheckpointKindEdit == CheckpointKindShellPost ||
		CheckpointKindShellPre == CheckpointKindShellPost {
		t.Fatal("checkpoint kinds are not unique")
	}
}

func TestValidateRanges(t *testing.T) {
	t.Parallel()
	human := Attribution{Author: AuthorHuman}
	override := Attribution{Author: AuthorHumanOverride, Agent: "droid", Model: "model"}
	tests := []struct {
		name      string
		ranges    []Range
		lineCount int
		valid     bool
	}{
		{"empty", nil, 0, true},
		{"complete", []Range{{Start: 1, End: 2, Attribution: human}}, 2, true},
		{"two ranges", []Range{
			{Start: 1, End: 1, Attribution: human},
			{Start: 2, End: 2, Attribution: Attribution{Author: AuthorUntracked}},
		}, 2, true},
		{"human override", []Range{{Start: 1, End: 1, Attribution: override}}, 1, true},
		{"range on empty", []Range{{Start: 1, End: 1, Attribution: human}}, 0, false},
		{"missing", nil, 1, false},
		{"gap", []Range{{Start: 2, End: 2, Attribution: human}}, 2, false},
		{"overlap", []Range{
			{Start: 1, End: 2, Attribution: human},
			{Start: 2, End: 2, Attribution: human},
		}, 2, false},
		{"reversed", []Range{{Start: 1, End: 0, Attribution: human}}, 1, false},
		{"past end", []Range{{Start: 1, End: 2, Attribution: human}}, 1, false},
		{"bad attribution", []Range{{Start: 1, End: 1, Attribution: Attribution{Author: "other"}}}, 1, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateRanges(test.ranges, test.lineCount)
			if (err == nil) != test.valid {
				t.Fatalf("ValidateRanges(%+v, %d) error = %v, valid = %t", test.ranges, test.lineCount, err, test.valid)
			}
		})
	}
}

func TestNormalizeIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		who   string
		email string
		want  string
	}{
		{"email local part", "John Doe", "john.doe@example.com", "john.doe"},
		{"uppercase email", "John Doe", "John.Doe@Example.COM", "john.doe"},
		{"plus address", "John", "john+git@example.com", "john+git"},
		{"name fallback", "Maya Chen", "", "maya.chen"},
		{"name fallback without at sign", "Maya Chen", "not-an-email", "maya.chen"},
		{"unicode name is reduced", "Zofia Zajac", "zofia@example.com", "zofia"},
		{"name only unicode", "\u5f20\u4e09", "", ""},
		{"separators trimmed", "  .John.  ", "", "john"},
		{"empty input", "", "", ""},
		{"empty local part", "", "@example.com", ""},
		{"too long", "", strings.Repeat("a", 65) + "@example.com", ""},
		{"long name falls back to nothing", strings.Repeat("b", 65), "", ""},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeIdentity(test.who, test.email); got != test.want {
				t.Fatalf("NormalizeIdentity(%q, %q) = %q, want %q", test.who, test.email, got, test.want)
			}
		})
	}
}

func TestValidateIdentity(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		valid bool
	}{
		{"", true},
		{"john.doe", true},
		{"a", true},
		{"john+git", true},
		{"John.Doe", false},
		{".john", false},
		{"john.", false},
		{"john doe", false},
		{"john/doe", false},
		{"john:doe", false},
		{strings.Repeat("a", 64), true},
		{strings.Repeat("a", 65), false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.value, func(t *testing.T) {
			t.Parallel()
			if err := ValidateIdentity(test.value); (err == nil) != test.valid {
				t.Fatalf("ValidateIdentity(%q) error = %v, valid = %t", test.value, err, test.valid)
			}
		})
	}
}

func TestValidateAttributionIdentityRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value Attribution
		valid bool
	}{
		{"human with identity", Attribution{Author: AuthorHuman, Identity: "john.doe"}, true},
		{"human without identity", Attribution{Author: AuthorHuman}, true},
		{"human with invalid identity", Attribution{Author: AuthorHuman, Identity: "John Doe"}, false},
		{"untracked with identity", Attribution{Author: AuthorUntracked, Identity: "john.doe"}, false},
		{"ai with identity", Attribution{
			Author: AuthorAI, Agent: "droid", Identity: "john.doe",
		}, false},
		{"ai without identity", Attribution{Author: AuthorAI, Agent: "droid"}, true},
		{"override with identity", Attribution{
			Author: AuthorHumanOverride, Agent: "droid", Identity: "john.doe",
		}, true},
		{"override with invalid identity", Attribution{
			Author: AuthorHumanOverride, Agent: "droid", Identity: "john doe",
		}, false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateAttribution(test.value); (err == nil) != test.valid {
				t.Fatalf("ValidateAttribution(%+v) error = %v, valid = %t", test.value, err, test.valid)
			}
		})
	}
}

func TestAttributionLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value Attribution
		want  string
	}{
		{"human", Attribution{Author: AuthorHuman, Identity: "john.doe"}, "human:john.doe"},
		{"human without identity", Attribution{Author: AuthorHuman}, "human"},
		{"ai with model", Attribution{Author: AuthorAI, Agent: "droid", Model: "m1"}, "ai:droid/m1"},
		{"ai without model", Attribution{Author: AuthorAI, Agent: "droid"}, "ai:droid"},
		{"override", Attribution{
			Author: AuthorHumanOverride, Identity: "john.doe", Agent: "droid", Model: "m1",
		}, "human-override:john.doe/droid/m1"},
		{"override without identity", Attribution{
			Author: AuthorHumanOverride, Agent: "droid", Model: "m1",
		}, "human-override:droid/m1"},
		{"untracked", Attribution{Author: AuthorUntracked}, "untracked"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.value.Label(); got != test.want {
				t.Fatalf("Label() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestObjectIDAndNewState(t *testing.T) {
	t.Parallel()
	if !ValidObjectID("abcd1234") || ValidObjectID("xyz") || ValidObjectID("abc") {
		t.Fatal("ValidObjectID returned an unexpected result")
	}
	state := NewState()
	if state.Version != StateVersion || state.NotesVersion != NoteVersion || state.Pending.Files == nil {
		t.Fatalf("NewState() = %+v", state)
	}
}
