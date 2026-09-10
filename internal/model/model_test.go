package model

import "testing"

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
		{"ai missing agent", Attribution{Author: AuthorAI}, false},
		{"ai control character", Attribution{Author: AuthorAI, Agent: "droid\nother"}, false},
		{"ai oversized model", Attribution{Author: AuthorAI, Agent: "droid", Model: string(make([]byte, maxAttributionValueBytes+1))}, false},
		{"ai invalid timestamp", Attribution{Author: AuthorAI, Agent: "droid", TS: "invalid"}, false},
		{"human metadata", Attribution{Author: AuthorHuman, Agent: "droid"}, false},
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
