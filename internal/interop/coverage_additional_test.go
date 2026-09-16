package interop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/store"
)

const (
	coverageCommit      = "0123456789abcdef0123456789abcdef01234567"
	coverageSchema      = "authorship/3.0.0"
	coveragePrompt      = "0123456789abcdef"
	coverageSession     = "s_0123456789abcd"
	coverageSessionAlt  = "s_abcdef01234567"
	coverageTrace       = "t_abcdef01234567"
	coverageTraceAlt    = "t_0123456789abcd"
	coverageHuman       = "h_89abcdef012345"
	coverageFile        = "file.txt"
	coverageAgent       = "cursor"
	coverageModel       = "model"
	coverageID          = "conversation"
	coverageAuthor      = "author"
	coverageRevision    = "HEAD"
	coverageOneLine     = "one\n"
	coverageMessage     = "content"
	coverageBadValue    = "bad"
	coverageBadRevision = "not-a-revision"
	coverageTimestamp   = "2026-01-01T00:00:00Z"
	coverageSeparator   = "::"
)

func TestInteropCoverageSessionAndIndex(t *testing.T) {
	t.Run("session wire", testSessionWireCoverage)
	t.Run("checkpoint index", testCheckpointIndexCoverage)
	t.Run("sort helpers", testSortHelpersCoverage)
	t.Run("hex helpers", testHexHelpersCoverage)
}

func testSessionWireCoverage(t *testing.T) {
	base := gitAISessionWire{
		AgentID: &gitAIAgentIDWire{
			Tool: coverageAgent, ID: coverageID, Model: coverageModel,
		},
	}
	tests := []struct {
		name  string
		left  gitAISessionWire
		right gitAISessionWire
		want  bool
	}{
		{name: "both nil", want: true},
		{name: "left nil", right: base},
		{name: "right nil", left: base},
		{name: "equal", left: base, right: base, want: true},
		{
			name: "different agent",
			left: base,
			right: gitAISessionWire{AgentID: &gitAIAgentIDWire{
				Tool: "claude", ID: coverageID, Model: coverageModel,
			}},
		},
		{
			name: "different human",
			left: base,
			right: gitAISessionWire{
				AgentID: &gitAIAgentIDWire{
					Tool: coverageAgent, ID: coverageID, Model: coverageModel,
				},
				HumanAuthor: coverageAuthor,
			},
		},
		{
			name: "different attributes",
			left: gitAISessionWire{
				AgentID: base.AgentID,
				CustomAttributes: map[string]string{
					"source": "one",
				},
			},
			right: gitAISessionWire{
				AgentID: base.AgentID,
				CustomAttributes: map[string]string{
					"source": "two",
				},
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if got := sameSessionWire(test.left, test.right); got != test.want {
				t.Fatalf("sameSessionWire() = %t, want %t", got, test.want)
			}
		})
	}
}

func testCheckpointIndexCoverage(t *testing.T) {
	records := []model.Checkpoint{
		{
			Type: model.AuthorHuman,
			Seq:  1,
		},
		{
			Type:    model.AuthorAI,
			Agent:   coverageAgent,
			Model:   coverageModel,
			Session: coverageID,
			TS:      coverageTimestamp,
			Seq:     7,
		},
		{
			Type:    model.AuthorAI,
			Agent:   coverageAgent,
			Model:   coverageModel,
			Session: coverageID,
			TS:      coverageTimestamp,
			Seq:     8,
		},
		{
			Type:    model.AuthorAI,
			Agent:   "claude",
			Model:   coverageModel,
			Session: "other",
			Seq:     9,
		},
	}
	index := newCheckpointIndex(records)
	exact, ok := index.sequence(model.Attribution{
		Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		TS: coverageTimestamp,
	})
	if !ok || exact != 7 {
		t.Fatalf("exact sequence = %d, %t", exact, ok)
	}
	loose, ok := index.sequence(model.Attribution{
		Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		TS: "2026-02-01T00:00:00Z",
	})
	if !ok || loose != 7 {
		t.Fatalf("loose sequence = %d, %t", loose, ok)
	}
	if _, ok := index.sequence(model.Attribution{Session: "missing"}); ok {
		t.Fatal("missing sequence matched")
	}
}

func testSortHelpersCoverage(t *testing.T) {
	values := []string{"z", "a", "m", "a"}
	sortStrings(values)
	if got := strings.Join(values, ","); got != "a,a,m,z" {
		t.Fatalf("sortStrings() = %q", got)
	}
	entries := []gitAIEntry{
		{key: "z", start: 2},
		{key: "b", start: 1},
		{key: "a", start: 1},
	}
	sortGitAIEntries(entries)
	if entries[0].key != "a" || entries[1].key != "b" || entries[2].key != "z" {
		t.Fatalf("sortGitAIEntries() = %+v", entries)
	}
}

func testHexHelpersCoverage(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", want: false},
		{name: "lowercase", value: "0123abcdef", want: true},
		{name: "uppercase", value: "ABCDEF", want: true},
		{name: "invalid", value: "0123g", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if got := isHex(test.value); got != test.want {
				t.Fatalf("isHex(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
	if validObjectID(coverageCommit) != true || validObjectID("not-an-object") {
		t.Fatal("validObjectID boundary mismatch")
	}
}

func TestInteropCoverageMetadataAndKeys(t *testing.T) {
	t.Run("strings", testMetadataStringCoverage)
	t.Run("keys", testGitAIKeyCoverage)
	t.Run("attestation ranges", testAttestationRangeCoverage)
	t.Run("attribution", testAttributionCoverage)
}

func testMetadataStringCoverage(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		required bool
		want     bool
	}{
		{name: "required empty", required: true},
		{name: "optional empty", want: true},
		{name: "ordinary", value: "value", want: true},
		{name: "too long", value: strings.Repeat("x", maxGitAIString+1)},
		{name: "invalid utf8", value: string([]byte{0xff})},
		{name: "control", value: "\x01"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if got := validGitAIString(test.value, test.required); got != test.want {
				t.Fatalf("validGitAIString() = %t, want %t", got, test.want)
			}
		})
	}
	valid := &gitAIAgentIDWire{
		Tool: coverageAgent, ID: coverageID, Model: coverageModel,
	}
	testsMetadata := []struct {
		name   string
		agent  *gitAIAgentIDWire
		human  string
		custom map[string]string
		want   bool
	}{
		{name: "valid", agent: valid, want: true},
		{name: "missing agent", human: coverageAuthor},
		{name: "missing tool", agent: &gitAIAgentIDWire{ID: coverageID, Model: coverageModel}},
		{name: "control custom key", agent: valid, custom: map[string]string{"\x01": "v"}},
		{name: "control custom value", agent: valid, custom: map[string]string{"k": "\x01"}},
	}
	for _, test := range testsMetadata {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateGitAIMetadata(test.agent, test.human, test.custom)
			if (err == nil) != test.want {
				t.Fatalf("validateGitAIMetadata() error = %v, want valid: %t", err, test.want)
			}
		})
	}
}

func testGitAIKeyCoverage(t *testing.T) {
	note := GitAINote{
		Sessions: map[string]GitAISession{
			coverageSession: {},
		},
		Humans: map[string]GitAIHuman{
			coverageHuman: {},
		},
		Prompts: map[string]GitAIPrompt{
			coveragePrompt: {},
		},
	}
	tests := []struct {
		name string
		key  string
		note GitAINote
		want bool
	}{
		{name: "session valid", key: coverageSession + coverageSeparator + coverageTrace, note: note, want: true},
		{name: "session bad shape", key: "s_bad", note: note},
		{name: "session bad trace", key: coverageSession + "::bad", note: note},
		{name: "session missing", key: "s_abcdef01234567::" + coverageTrace, note: note},
		{name: "human valid", key: coverageHuman, note: note, want: true},
		{name: "human bad shape", key: "h_bad", note: note},
		{name: "human missing", key: "h_0123456789abcd", note: note},
		{name: "legacy valid", key: coveragePrompt, note: note, want: true},
		{name: "legacy bad shape", key: coverageBadValue, note: note},
		{name: "legacy missing", key: "abcdef0123456789", note: note},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateGitAIKey(test.key, test.note)
			if (err == nil) != test.want {
				t.Fatalf("validateGitAIKey(%q) error = %v, want valid: %t", test.key, err, test.want)
			}
		})
	}
}

func testAttestationRangeCoverage(t *testing.T) {
	tests := []struct {
		name    string
		entries []GitAIAttestation
		wantErr bool
	}{
		{name: "empty"},
		{
			name: "ordered",
			entries: []GitAIAttestation{{
				Ranges: []GitAILineRange{{Start: 1, End: 1}, {Start: 3, End: 4}},
			}},
		},
		{
			name: "overlap",
			entries: []GitAIAttestation{{
				Ranges: []GitAILineRange{{Start: 1, End: 2}, {Start: 2, End: 3}},
			}},
			wantErr: true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateAttestationRanges(test.entries)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateAttestationRanges() error = %v, want error: %t", err, test.wantErr)
			}
		})
	}
	tooMany := make([]GitAILineRange, maxGitAIEntries+1)
	for index := range tooMany {
		tooMany[index] = GitAILineRange{Start: index*2 + 1, End: index*2 + 1}
	}
	if err := validateAttestationRanges([]GitAIAttestation{{Ranges: tooMany}}); err == nil {
		t.Fatal("validateAttestationRanges accepted too many ranges")
	}
}

func testAttributionCoverage(t *testing.T) {
	note := GitAINote{
		Sessions: map[string]GitAISession{
			coverageSession: {
				AgentID: GitAIAgentID{Tool: coverageAgent, Model: coverageModel},
			},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
		Prompts: map[string]GitAIPrompt{
			coveragePrompt: {
				AgentID: GitAIAgentID{Tool: coverageAgent, Model: coverageModel},
			},
		},
	}
	tests := []struct {
		name string
		key  string
		want model.Author
	}{
		{name: "session", key: coverageSession + coverageSeparator + coverageTrace, want: model.AuthorAI},
		{name: "human", key: coverageHuman, want: model.AuthorHuman},
		{name: "prompt", key: coveragePrompt, want: model.AuthorAI},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			value, err := note.attribution(test.key)
			if err != nil || value.Author != test.want {
				t.Fatalf("attribution(%q) = %+v, %v", test.key, value, err)
			}
		})
	}
	testsMissing := []string{
		coverageSession + coverageSeparator + coverageTrace,
		coverageHuman,
		coveragePrompt,
	}
	for _, key := range testsMissing {
		missing := GitAINote{
			Sessions: map[string]GitAISession{},
			Humans:   map[string]GitAIHuman{},
			Prompts:  map[string]GitAIPrompt{},
		}
		if _, err := missing.attribution(key); err == nil {
			t.Fatalf("attribution(%q) accepted missing metadata", key)
		}
	}
	invalid := GitAINote{
		Sessions: map[string]GitAISession{
			coverageSession: {AgentID: GitAIAgentID{}},
		},
	}
	if _, err := invalid.attribution(coverageSession + coverageSeparator + coverageTrace); err == nil {
		t.Fatal("attribution accepted invalid session metadata")
	}
}

func TestInteropCoverageParsing(t *testing.T) {
	t.Run("paths", testGitAIPathCoverage)
	t.Run("line ranges", testLineRangeCoverage)
	t.Run("attribution ranges", testAttributionRangeConversionCoverage)
	t.Run("trace metadata", testTraceMetadataCoverage)
}

func testGitAIPathCoverage(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: coverageFile, want: coverageFile},
		{name: "quoted", value: `"with space.txt"`, want: "with space.txt"},
		{name: "empty", value: ""},
		{name: "bad quote", value: `"unterminated`},
		{name: "unquoted whitespace", value: "with space.txt"},
		{name: "absolute", value: "/tmp/file.txt"},
		{name: "parent", value: "../file.txt"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := parseGitAIPath(test.value)
			if test.want != "" {
				if err != nil || got != test.want {
					t.Fatalf("parseGitAIPath(%q) = %q, %v", test.value, got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseGitAIPath(%q) accepted invalid path", test.value)
			}
		})
	}
}

func testLineRangeCoverage(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []GitAILineRange
	}{
		{name: "single", value: "2", want: []GitAILineRange{{Start: 2, End: 2}}},
		{name: "range list", value: "1-2,4", want: []GitAILineRange{{Start: 1, End: 2}, {Start: 4, End: 4}}},
		{name: "empty"},
		{name: "empty item", value: "1,"},
		{name: "wrong range shape", value: "1-2-3"},
		{name: "bad start", value: "x-2"},
		{name: "bad end", value: "1-x"},
		{name: "zero", value: "0"},
		{name: "descending", value: "2-1"},
		{name: "unordered", value: "2,2"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := parseLineRanges(test.value)
			if test.want != nil {
				if err != nil || fmt.Sprint(got) != fmt.Sprint(test.want) {
					t.Fatalf("parseLineRanges(%q) = %v, %v", test.value, got, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseLineRanges(%q) accepted invalid input", test.value)
			}
		})
	}
}

func testAttributionRangeConversionCoverage(t *testing.T) {
	if got := rangesFromAttributions(nil); got != nil {
		t.Fatalf("rangesFromAttributions(nil) = %+v", got)
	}
	values := []model.Attribution{
		{Author: model.AuthorHuman},
		{Author: model.AuthorHuman},
		{Author: model.AuthorAI, Agent: coverageAgent},
	}
	got := rangesFromAttributions(values)
	if len(got) != 2 || got[0].Start != 1 || got[0].End != 2 || got[1].Start != 3 {
		t.Fatalf("rangesFromAttributions() = %+v", got)
	}
	formatted := formatLineRanges([]GitAILineRange{
		{Start: 1, End: 1},
		{Start: 3, End: 5},
	})
	if formatted != "1,3-5" {
		t.Fatalf("formatLineRanges() = %q", formatted)
	}
}

func testTraceMetadataCoverage(t *testing.T) {
	metadata := appendTraceMetadata(nil, TraceAttribution{
		File: coverageFile, Conversation: 0, Agent: coverageAgent,
	})
	metadata = appendTraceMetadata(metadata, TraceAttribution{
		File: "other.txt", Conversation: 1, Session: coverageID,
	})
	if len(metadata["comarch.git-byline"]) != 2 {
		t.Fatalf("trace metadata = %+v", metadata)
	}
	ranges := []model.Range{
		{Start: 1, End: 1, Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		}},
		{Start: 2, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 3, End: 3, Attribution: model.Attribution{Author: model.AuthorUntracked}},
	}
	conversations, sources, err := traceConversations(coverageFile, ranges)
	if err != nil || len(conversations) != 3 || len(sources) != 1 {
		t.Fatalf("traceConversations() = %+v, %+v, %v", conversations, sources, err)
	}
	if _, _, err := traceConversations(coverageFile, []model.Range{{
		Start: 1, End: 1, Attribution: model.Attribution{Author: "other"},
	}}); err == nil {
		t.Fatal("traceConversations accepted unsupported author")
	}
}

func TestDecodeGitAICoverageEdges(t *testing.T) {
	validAttestation := coverageFile + "\n  " + coveragePrompt + " 1"
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "oversized",
			data: bytes.Repeat([]byte("x"), maxGitAINoteBytes+1),
		},
		{
			name: "empty metadata",
			data: []byte(validAttestation + "\n---\n"),
		},
		{
			name: "malformed metadata",
			data: []byte(validAttestation + "\n---\n{"),
		},
		{
			name: "multiple metadata values",
			data: []byte(validAttestation + "\n---\n" +
				coverageMetadataJSON(t, coverageMetadataMap(coverageCommit)) + "\n{}"),
		},
		{
			name: "invalid metadata tail",
			data: []byte(validAttestation + "\n---\n" +
				coverageMetadataJSON(t, coverageMetadataMap(coverageCommit)) + "\n{"),
		},
		{
			name: "missing required metadata",
			data: coverageNoteWithMetadata(t, validAttestation, map[string]any{
				"prompts": map[string]any{},
			}),
		},
		{
			name: "invalid Git AI version",
			data: coverageNoteWithMetadata(t, validAttestation, map[string]any{
				"schema_version":  coverageSchema,
				"base_commit_sha": coverageCommit,
				"git_ai_version":  "\x01",
				"prompts":         map[string]any{},
			}),
		},
		{
			name: "unknown schema",
			data: coverageNoteWithMetadata(t, validAttestation, map[string]any{
				"schema_version":  "authorship/2.0.0",
				"base_commit_sha": coverageCommit,
				"prompts":         map[string]any{},
			}),
		},
		{
			name: "invalid base",
			data: coverageNoteWithMetadata(t, validAttestation, map[string]any{
				"schema_version":  coverageSchema,
				"base_commit_sha": coverageBadValue,
				"prompts":         map[string]any{},
			}),
		},
		{
			name: "invalid session key",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "sessions", map[string]any{coverageBadValue: coverageSessionWireMap()},
			)),
		},
		{
			name: "invalid session metadata",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "sessions", map[string]any{
					coverageSession: map[string]any{"agent_id": nil},
				},
			)),
		},
		{
			name: "invalid human key",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "humans", map[string]any{coverageBadValue: map[string]any{"author": "x"}},
			)),
		},
		{
			name: "invalid human value",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "humans", map[string]any{
					coverageHuman: map[string]any{"author": "\x01"},
				},
			)),
		},
		{
			name: "invalid prompt key",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "prompts", map[string]any{coverageBadValue: coveragePromptWireMap()},
			)),
		},
		{
			name: "missing prompt counter",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "prompts", map[string]any{
					coveragePrompt: map[string]any{
						"agent_id":        coverageAgentIDMap(),
						"total_additions": 1,
						"total_deletions": 0,
						"accepted_lines":  1,
					},
				},
			)),
		},
		{
			name: "negative prompt counter",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "prompts", map[string]any{
					coveragePrompt: map[string]any{
						"agent_id":        coverageAgentIDMap(),
						"total_additions": -1,
						"total_deletions": 0,
						"accepted_lines":  1,
						"overriden_lines": 0,
					},
				},
			)),
		},
		{
			name: "invalid prompt metadata",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "prompts", map[string]any{
					coveragePrompt: map[string]any{
						"agent_id":        nil,
						"total_additions": 1,
						"total_deletions": 0,
						"accepted_lines":  1,
						"overriden_lines": 0,
					},
				},
			)),
		},
		{
			name: "invalid messages URL",
			data: coverageNoteWithMetadata(t, validAttestation, coverageMetadataMapWith(
				coverageCommit, "prompts", map[string]any{
					coveragePrompt: map[string]any{
						"agent_id":        coverageAgentIDMap(),
						"messages_url":    "\x01",
						"total_additions": 1,
						"total_deletions": 0,
						"accepted_lines":  1,
						"overriden_lines": 0,
					},
				},
			)),
		},
		{
			name: "empty attestation line",
			data: coverageNoteWithRawMetadata("\n", coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "invalid indentation",
			data: coverageNoteWithRawMetadata("   "+coveragePrompt+" 1", coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "attestation without path",
			data: coverageNoteWithRawMetadata("  "+coveragePrompt+" 1", coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "attestation fields",
			data: coverageNoteWithRawMetadata(coverageFile+"\n  "+coveragePrompt, coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "invalid line range",
			data: coverageNoteWithRawMetadata(coverageFile+"\n  "+coveragePrompt+" x", coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "missing prompt key",
			data: coverageNoteWithRawMetadata(coverageFile+"\n  bad 1", coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "invalid path indentation",
			data: coverageNoteWithRawMetadata(" "+coverageFile, coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
		{
			name: "repeated path",
			data: coverageNoteWithRawMetadata(
				coverageFile+"\n  "+coveragePrompt+" 1\n"+coverageFile+"\n  "+coveragePrompt+" 2",
				coverageMetadataJSON(t, coverageMetadataMap(coverageCommit)),
			),
		},
		{
			name: "path without attestations",
			data: coverageNoteWithRawMetadata(coverageFile, coverageMetadataJSON(t, coverageMetadataMap(coverageCommit))),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeGitAI(test.data); err == nil {
				t.Fatal("DecodeGitAI accepted invalid input")
			}
		})
	}
}

func TestDecodeGitAIFileLimit(t *testing.T) {
	var builder strings.Builder
	for index := 0; index <= maxGitAIFiles; index++ {
		fmt.Fprintf(&builder, "file-%03d.txt\n  %s 1\n", index, coveragePrompt)
	}
	data := coverageNoteWithRawMetadata(builder.String(), coverageMetadataJSON(t, coverageMetadataMap(coverageCommit)))
	if _, err := DecodeGitAI(data); err == nil {
		t.Fatal("DecodeGitAI accepted too many files")
	}
}

func TestGitAIEntriesCoverage(t *testing.T) {
	record := model.Checkpoint{
		Type:    model.AuthorAI,
		Agent:   coverageAgent,
		Model:   coverageModel,
		Session: coverageID,
		TS:      coverageTimestamp,
		Seq:     4,
	}
	index := newCheckpointIndex([]model.Checkpoint{record})
	ranges := []model.Range{
		{Start: 1, End: 1, Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		}},
		{Start: 2, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 3, End: 3, Attribution: model.Attribution{Author: model.AuthorHumanOverride}},
		{Start: 4, End: 4, Attribution: model.Attribution{Author: model.AuthorUntracked}},
	}
	entries, sessions, humans, err := gitAIEntries(coverageFile, ranges, index)
	if err != nil || len(entries) != 2 || len(sessions) != 1 || len(humans) != 1 {
		t.Fatalf("gitAIEntries() = %+v, %+v, %+v, %v", entries, sessions, humans, err)
	}
	if entries[0].ranges[0].Start != 1 || entries[1].ranges[0].Start != 2 {
		t.Fatalf("gitAIEntries order = %+v", entries)
	}
	unknownIndex := newCheckpointIndex([]model.Checkpoint{{
		Type: model.AuthorAI, Agent: coverageAgent, Session: coverageID, Seq: 5,
	}})
	unknownEntries, unknownSessions, _, err := gitAIEntries(coverageFile, []model.Range{{
		Start: 1, End: 1, Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Session: coverageID,
		},
	}}, unknownIndex)
	if err != nil || len(unknownEntries) != 1 ||
		unknownSessions[gitAISessionID(coverageAgent, coverageID)].AgentID.Model != "unknown" {
		t.Fatalf("unknown Git AI entry = %+v, %+v, %v", unknownEntries, unknownSessions, err)
	}
	conflict := []model.Range{
		{Start: 1, End: 1, Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		}},
		{Start: 2, End: 2, Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Model: "other-model", Session: coverageID,
		}},
	}
	conflictIndex := newCheckpointIndex([]model.Checkpoint{
		record,
		{Type: model.AuthorAI, Agent: coverageAgent, Model: "other-model", Session: coverageID, Seq: 5},
	})
	if _, _, _, err := gitAIEntries(coverageFile, conflict, conflictIndex); err == nil {
		t.Fatal("gitAIEntries accepted conflicting session metadata")
	}
	if _, _, _, err := gitAIEntries(coverageFile, []model.Range{{
		Start: 1, End: 1, Attribution: model.Attribution{Author: "other"},
	}}, index); err == nil {
		t.Fatal("gitAIEntries accepted unsupported author")
	}
}

func TestToBylineNoteCoverage(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, "one\ntwo\n")
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (GitAINote{}).ToBylineNote(nil, commit); err == nil {
		t.Fatal("ToBylineNote accepted nil repository")
	}
	if _, err := (GitAINote{BaseCommit: coverageBadValue}).ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted mismatched base")
	}
	missing := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			"missing.txt": {{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 1, End: 1}}}},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
	}
	if _, err := missing.ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted missing file")
	}
	blob, exists, err := repo.BlobID(commit, coverageFile)
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	rangeNote := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			coverageFile: {{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 3, End: 3}}}},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
	}
	if _, err := rangeNote.ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted range beyond file")
	}
	overlap := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			coverageFile: {
				{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 1, End: 1}}},
				{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 1, End: 1}}},
			},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
	}
	if _, err := overlap.ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted overlapping ranges")
	}
	if blob == "" {
		t.Fatal("empty blob")
	}
}

func TestInteropReadAndResolveCoverage(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, coverageOneLine)
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(nil, commit); err == nil {
		t.Fatal("readByline accepted nil repository")
	}
	if _, _, err := readByline(repo, coverageBadValue); err == nil {
		t.Fatal("readByline accepted invalid commit")
	}
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted missing note")
	}
	if _, err := resolveCommit(nil, coverageRevision); err == nil {
		t.Fatal("resolveCommit accepted nil repository")
	}
	if _, err := resolveCommit(repo, ""); err == nil {
		t.Fatal("resolveCommit accepted empty revision")
	}
	if _, err := resolveCommit(repo, coverageBadRevision); err == nil {
		t.Fatal("resolveCommit accepted invalid revision")
	}
	if _, err := resolveCommit(repo, "HEAD..HEAD"); err == nil {
		t.Fatal("resolveCommit accepted zero-result revision")
	}
	if got, err := resolveCommit(repo, coverageRevision); err != nil || got != commit {
		t.Fatalf("resolveCommit(HEAD) = %q, %v", got, err)
	}
	if _, _, err := ReadGitAINote(repo, coverageBadRevision); err == nil {
		t.Fatal("ReadGitAINote accepted invalid revision")
	}
}

func TestInteropReadBylineValidationCoverage(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, "one\ntwo\n")
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, exists, err := repo.BlobID(commit, coverageFile)
	if err != nil || !exists {
		t.Fatalf("blob = %q, %t, %v", blob, exists, err)
	}
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: 2,
					Attribution: model.Attribution{Author: model.AuthorUntracked},
				}},
			},
		},
	}
	encoded, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, encoded); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(repo, commit); err != nil {
		t.Fatalf("readByline(valid) = %v", err)
	}
	wrongBlob := note
	wrongBlob.Files[coverageFile] = model.NoteFile{
		Blob:   "abcdef0123456789abcdef0123456789abcdef01",
		Ranges: note.Files[coverageFile].Ranges,
	}
	wrongEncoded, err := notes.Encode(wrongBlob)
	if err != nil {
		t.Fatal(err)
	}
	runInteropGit(t, root, "notes", "--ref=refs/notes/byline", "remove", commit)
	if err := repo.WriteNoteRef("refs/notes/byline", commit, wrongEncoded); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted mismatched blob")
	}
}

func TestInteropMetadataDecodeAndExportErrors(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, coverageOneLine)
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExportGitAI(repo, coverageBadRevision); err == nil {
		t.Fatal("ExportGitAI accepted invalid revision")
	}
	if _, err := ExportAgentTrace(repo, coverageBadRevision); err == nil {
		t.Fatal("ExportAgentTrace accepted invalid revision")
	}
	if err := WriteGitAINote(nil, commit); err == nil {
		t.Fatal("WriteGitAINote accepted nil repository")
	}
	if err := WriteGitAINote(repo, commit); err == nil {
		t.Fatal("WriteGitAINote accepted missing byline note")
	}
	if _, _, err := ReadGitAINote(nil, commit); err == nil {
		t.Fatal("ReadGitAINote accepted nil repository")
	}
	if _, err := decodeGitAIMetadata([]byte(`{"schema_version":"x","prompts":{}}`)); err == nil {
		t.Fatal("decodeGitAIMetadata accepted missing required values")
	}
}

func TestExportGitAIEmptyEntriesCoverage(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 1)
	data, err := ExportGitAI(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("---\n")) || bytes.Contains(data, []byte(coverageFile)) {
		t.Fatalf("empty Git AI export = %s", data)
	}
}

func TestInteropGitAIEntriesAndTraceErrors(t *testing.T) {
	index := newCheckpointIndex(nil)
	if _, _, _, err := gitAIEntries(coverageFile, []model.Range{{
		Start: 1, End: 1,
		Attribution: model.Attribution{
			Author: model.AuthorAI, Agent: coverageAgent, Model: coverageModel, Session: coverageID,
		},
	}}, index); err != nil {
		t.Fatalf("unmatched Git AI entry returned error: %v", err)
	}
	if _, _, _, err := gitAIEntries(coverageFile, []model.Range{{
		Start: 1, End: 1,
		Attribution: model.Attribution{Author: "unsupported"},
	}}, index); err == nil {
		t.Fatal("unsupported Git AI entry accepted")
	}
	if _, _, err := traceConversations(coverageFile, []model.Range{{
		Start: 1, End: 1,
		Attribution: model.Attribution{Author: "unsupported"},
	}}); err == nil {
		t.Fatal("unsupported trace attribution accepted")
	}
}

func TestImportCommitsCoverage(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, coverageOneLine)
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := importCommits(repo, "")
	if err != nil || len(commits) != 1 || commits[0] != commit {
		t.Fatalf("importCommits(HEAD) = %v, %v", commits, err)
	}
	if _, err := importCommits(repo, coverageBadRevision); err == nil {
		t.Fatal("importCommits accepted invalid range")
	}
}

func TestInteropCoverageHelperFixtures(t *testing.T) {
	value := coverageMetadataMap(coverageCommit)
	if coverageMetadataJSON(t, value) == "" {
		t.Fatal("empty metadata fixture")
	}
	if coverageSessionWireMap()["agent_id"] == nil {
		t.Fatal("empty session fixture")
	}
	if coveragePromptWireMap()["agent_id"] == nil {
		t.Fatal("empty prompt fixture")
	}
}

func coverageMetadataMap(base string) map[string]any {
	return map[string]any{
		"schema_version":  coverageSchema,
		"base_commit_sha": base,
		"prompts": map[string]any{
			coveragePrompt: coveragePromptWireMap(),
		},
	}
}

func coverageMetadataMapWith(base, key string, value any) map[string]any {
	result := coverageMetadataMap(base)
	result[key] = value
	return result
}

func coverageAgentIDMap() map[string]any {
	return map[string]any{
		"tool":  coverageAgent,
		"id":    coverageID,
		"model": coverageModel,
	}
}

func coverageSessionWireMap() map[string]any {
	return map[string]any{"agent_id": coverageAgentIDMap()}
}

func coveragePromptWireMap() map[string]any {
	return map[string]any{
		"agent_id":        coverageAgentIDMap(),
		"total_additions": 1,
		"total_deletions": 0,
		"accepted_lines":  1,
		"overriden_lines": 0,
	}
}

func coverageMetadataJSON(t *testing.T, value map[string]any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func coverageNoteWithMetadata(t *testing.T, attestations string, metadata map[string]any) []byte {
	t.Helper()
	return coverageNoteWithRawMetadata(attestations, coverageMetadataJSON(t, metadata))
}

func coverageNoteWithRawMetadata(attestations, metadata string) []byte {
	return []byte(attestations + "\n---\n" + metadata)
}

func coverageRepoWithFile(t *testing.T, content, message string) (string, string, *gitcmd.Repo) {
	t.Helper()
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, content)
	commit := commitInterop(t, root, message)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, commit, repo
}

func writeCoverageUntrackedNote(t *testing.T, repo *gitcmd.Repo, commit, path string, end int) {
	t.Helper()
	blob := mustInteropBlob(t, repo, commit, path)
	if err := repo.WriteNote(commit, coverageUntrackedNote(t, blob, path, end)); err != nil {
		t.Fatal(err)
	}
}

func coverageUntrackedNote(t *testing.T, blob, path string, end int) []byte {
	t.Helper()
	note, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			path: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1, End: end,
					Attribution: model.Attribution{Author: model.AuthorUntracked},
				}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return note
}

func TestInteropReadBylineCheckpointError(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 1)
	stateDir := filepath.Join(repo.GitDir, "byline")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "checkpoints.jsonl"), []byte(`{"version":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted malformed checkpoints")
	}
}

func TestInteropReadGitAINoteMalformed(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, coverageOneLine)
	commit := commitInterop(t, root, coverageMessage)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNoteRef(GitAINotesRef, commit, []byte(coverageBadValue)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadGitAINote(repo, commit); err == nil {
		t.Fatal("ReadGitAINote accepted malformed note")
	}
}

func TestInteropTraceAndSessionDirectCoverage(t *testing.T) {
	if got := gitAIHumanID(coverageAuthor); got == "" || !strings.HasPrefix(got, "h_") {
		t.Fatalf("gitAIHumanID() = %q", got)
	}
	if got := gitAITraceID(1); got == "" || !strings.HasPrefix(got, "t_") {
		t.Fatalf("gitAITraceID() = %q", got)
	}
	if got := traceUUID(coverageCommit); len(got) != 36 {
		t.Fatalf("traceUUID() = %q", got)
	}
	if !sameNoteBytes([]byte(coverageOneLine), []byte("one")) {
		t.Fatal("sameNoteBytes did not ignore one trailing newline")
	}
	if sameNoteBytes([]byte("one"), []byte("two")) {
		t.Fatal("sameNoteBytes accepted different notes")
	}
}

func TestInteropImportResultNilAndAliases(t *testing.T) {
	if _, err := ImportGitAI(nil, "", false); err == nil {
		t.Fatal("ImportGitAI accepted nil repository")
	}
	if _, err := ExportAgentTrace(nil, coverageCommit); err == nil {
		t.Fatal("ExportAgentTrace accepted nil repository")
	}
	if _, err := EncodeGitAI(nil, coverageCommit); err == nil {
		t.Fatal("EncodeGitAI accepted nil repository")
	}
	if _, err := EncodeAgentTrace(nil, coverageCommit); err == nil {
		t.Fatal("EncodeAgentTrace accepted nil repository")
	}
	if _, err := DecodeGitAI([]byte("")); err == nil {
		t.Fatal("DecodeGitAI accepted empty note")
	}
}

func TestInteropCoverageConflictAndReadErrors(t *testing.T) {
	t.Run("Git AI export session conflict", testExportGitAISessionConflict)
	t.Run("Git AI note read error", testReadGitAINoteError)
	t.Run("Git AI write errors", testWriteGitAINoteErrors)
	t.Run("Agent Trace missing byline", testExportAgentTraceMissingByline)
	t.Run("import Git AI read error", testImportGitAIReadError)
	t.Run("import invalid range", testImportGitAIInvalidRange)
}

func testExportGitAISessionConflict(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, "a.txt", "a\n")
	writeInteropFile(t, root, "b.txt", "b\n")
	commit := commitInterop(t, root, "conflict")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blobs := mustInteropBlobs(t, repo, commit, "a.txt", "b.txt")
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			"a.txt": {
				Blob: blobs["a.txt"],
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: coverageAgent,
						Model: coverageModel, Session: coverageID,
					},
				}},
			},
			"b.txt": {
				Blob: blobs["b.txt"],
				Ranges: []model.Range{{
					Start: 1, End: 1,
					Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: coverageAgent,
						Model: "other-model", Session: coverageID,
					},
				}},
			},
		},
	}
	encoded, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, encoded); err != nil {
		t.Fatal(err)
	}
	for sequence, entry := range []struct {
		model string
		blob  string
	}{
		{model: coverageModel, blob: blobs["a.txt"]},
		{model: "other-model", blob: blobs["b.txt"]},
	} {
		if err := storeCheckpointForCoverage(t, repo, model.Checkpoint{
			Version: model.CheckpointVersion,
			Kind:    model.CheckpointKindEdit,
			Seq:     uint64(sequence + 1),
			TS:      coverageTimestamp,
			Type:    model.AuthorAI,
			Session: coverageID,
			Agent:   coverageAgent,
			Model:   entry.model,
			Files:   []model.Snapshot{{Path: "file.txt", Exists: true, Blob: entry.blob}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ExportGitAI(repo, commit); err == nil {
		t.Fatal("ExportGitAI accepted conflicting session metadata")
	} else {
		t.Logf("conflict error: %v", err)
	}
}

func TestExportGitAIEntryConflict(t *testing.T) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, "one\ntwo\n")
	commit := commitInterop(t, root, "entry conflict")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustInteropBlob(t, repo, commit, coverageFile)
	note := model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {
				Blob: blob,
				Ranges: []model.Range{
					{Start: 1, End: 1, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: coverageAgent,
						Model: coverageModel, Session: coverageID,
					}},
					{Start: 2, End: 2, Attribution: model.Attribution{
						Author: model.AuthorAI, Agent: coverageAgent,
						Model: "other-model", Session: coverageID,
					}},
				},
			},
		},
	}
	encoded, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, encoded); err != nil {
		t.Fatal(err)
	}
	for sequence, modelName := range []string{coverageModel, "other-model"} {
		if err := storeCheckpointForCoverage(t, repo, model.Checkpoint{
			Version: model.CheckpointVersion,
			Kind:    model.CheckpointKindEdit,
			Seq:     uint64(sequence + 1),
			TS:      coverageTimestamp,
			Type:    model.AuthorAI,
			Session: coverageID,
			Agent:   coverageAgent,
			Model:   modelName,
			Files:   []model.Snapshot{{Path: coverageFile, Exists: true, Blob: blob}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ExportGitAI(repo, commit); err == nil {
		t.Fatal("ExportGitAI accepted conflicting entries in one file")
	}
}

func testReadGitAINoteError(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	setInteropRefToBlob(t, root, GitAINotesRef)
	if _, _, err := ReadGitAINote(repo, commit); err == nil {
		t.Fatal("ReadGitAINote accepted an invalid notes ref")
	}
}

func testWriteGitAINoteErrors(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	if err := WriteGitAINote(repo, coverageBadRevision); err == nil {
		t.Fatal("WriteGitAINote accepted an invalid revision")
	}
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 1)
	if err := WriteGitAINote(repo, commit); err != nil {
		t.Fatal(err)
	}
	runInteropGit(t, root, "notes", "--ref="+GitAINotesRef, "add", "-f", "-m", "different", commit)
	if err := WriteGitAINote(repo, commit); err == nil {
		t.Fatal("WriteGitAINote accepted a different existing note")
	}
}

func testExportAgentTraceMissingByline(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	if _, err := ExportAgentTrace(repo, commit); err == nil {
		t.Fatal("ExportAgentTrace accepted missing byline note")
	}
}

func TestExportAgentTraceTimestampError(t *testing.T) {
	root, commit, _ := coverageRepoWithFile(t, coverageOneLine, "timestamp error")
	note, err := notes.Encode(model.Note{Version: model.NoteVersion})
	if err != nil {
		t.Fatal(err)
	}
	fakeRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeRoot, "note"), note, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeRepo := discoverInteropFakeRepo(t, root, `
rev-list) printf '%s\n' "$FAKE_COMMIT" ;;
notes) cat "$FAKE_ROOT/note" ;;
show) echo "timestamp failure" >&2; exit 1 ;;
*) exit 1 ;;
`)
	t.Setenv("FAKE_COMMIT", commit)
	if err := os.Rename(filepath.Join(fakeRoot, "note"), filepath.Join(root, "note")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(fakeRepo, commit); err != nil {
		t.Fatalf("readByline(fake) = %v", err)
	}
	if _, err := ExportAgentTrace(fakeRepo, commit); err == nil {
		t.Fatal("ExportAgentTrace accepted timestamp failure")
	}
}

func testImportGitAIReadError(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	setInteropRefToBlob(t, root, GitAINotesRef)
	if _, err := ImportGitAI(repo, commit, false); err == nil {
		t.Fatal("ImportGitAI accepted an invalid Git AI notes ref")
	}
}

func TestImportGitAICoversMissingAndSkippedNotes(t *testing.T) {
	t.Run("missing Git AI note", testImportGitAIMissingNote)
	t.Run("invalid byline conversion", testImportGitAIConversionError)
	t.Run("byline note read error", testImportGitAIBylineReadError)
	t.Run("encoded note failure", testImportGitAIEncodeFailure)
	t.Run("write conflict", func(t *testing.T) {
		testImportGitAIWriteFailure(t, true)
	})
	t.Run("write failure", func(t *testing.T) {
		testImportGitAIWriteFailure(t, false)
	})
}

func testImportGitAIMissingNote(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, "no Git AI note")
	result, err := ImportGitAI(repo, commit, false)
	if err != nil || result.Imported != 0 || result.Skipped != 0 {
		t.Fatalf("ImportGitAI missing note = %+v, %v", result, err)
	}
}

func testImportGitAIConversionError(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, "invalid conversion")
	data := coverageNoteWithRawMetadata(
		coverageFile+"\n  "+coveragePrompt+" 2",
		coverageMetadataJSON(t, coverageMetadataMap(commit)),
	)
	if err := repo.WriteNoteRef(GitAINotesRef, commit, data); err != nil {
		t.Fatal(err)
	}
	result, err := ImportGitAI(repo, commit, false)
	if err != nil || result.Imported != 0 || result.Skipped != 1 || len(result.Warnings) != 1 {
		t.Fatalf("ImportGitAI conversion error = %+v, %v", result, err)
	}
}

func testImportGitAIBylineReadError(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, "byline read error")
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 1)
	if err := WriteGitAINote(repo, commit); err != nil {
		t.Fatal(err)
	}
	setInteropRefToBlob(t, root, "refs/notes/byline")
	if _, err := ImportGitAI(repo, commit, false); err == nil {
		t.Fatal("ImportGitAI accepted byline note read failure")
	}
}

func testImportGitAIWriteFailure(t *testing.T, conflict bool) {
	root := interopRepo(t)
	writeInteropFile(t, root, coverageFile, coverageOneLine)
	commit := commitInterop(t, root, "write failure")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustInteropBlob(t, repo, commit, coverageFile)
	gitAINote := coverageNoteWithRawMetadata(
		coverageFile+"\n  "+coveragePrompt+" 1",
		coverageMetadataJSON(t, coverageMetadataMap(commit)),
	)
	if err := os.WriteFile(filepath.Join(root, "gitai-note"), gitAINote, 0o600); err != nil {
		t.Fatal(err)
	}
	script := `
rev-list) printf '%s\n' "$FAKE_COMMIT" ;;
notes)
	case "$2" in
	--ref=refs/notes/ai) cat "$FAKE_ROOT/gitai-note" ;;
	--ref=refs/notes/byline)
		if [ -f "$FAKE_ROOT/byline-read" ]; then
			if ` + strconv.FormatBool(conflict) + ` = true; then
				printf 'different\n'
				exit 0
			fi
			exit 1
		fi
		touch "$FAKE_ROOT/byline-read"
		exit 1
		;;
	*) exit 1 ;;
	esac
	;;
ls-tree) printf '100644 blob %s\t%s\0' ` + blob + ` ` + coverageFile + ` ;;
cat-file) printf 'one\n' ;;
*) exit 1 ;;
`
	t.Setenv("FAKE_COMMIT", commit)
	fakeRepo := discoverInteropFakeRepo(t, root, script)
	result, err := ImportGitAI(fakeRepo, commit, false)
	if conflict {
		if err != nil || result.Skipped != 1 || len(result.Warnings) != 1 {
			t.Fatalf("ImportGitAI conflict = %+v, %v", result, err)
		}
		return
	}
	if err == nil {
		t.Fatal("ImportGitAI accepted write failure")
	}
}

func testImportGitAIEncodeFailure(t *testing.T) {
	root := interopRepo(t)
	const lineCount = 20_000
	var content strings.Builder
	var attestations strings.Builder
	content.Grow(lineCount * 2)
	attestations.WriteString(coverageFile + "\n")
	for line := 1; line <= lineCount; line++ {
		content.WriteString("x\n")
		if line%2 == 0 {
			fmt.Fprintf(&attestations, "  %s::%s %d\n", coverageSession, coverageTrace, line)
		} else {
			fmt.Fprintf(&attestations, "  %s::%s %d\n", coverageSessionAlt, coverageTraceAlt, line)
		}
	}
	writeInteropFile(t, root, coverageFile, content.String())
	commit := commitInterop(t, root, "large Git AI note")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	longValue := strings.Repeat("x", maxGitAIString)
	metadata := fmt.Sprintf(
		`{"schema_version":%q,"base_commit_sha":%q,"prompts":{},"sessions":{`+
			`%q:{"agent_id":{"tool":%q,"id":"id","model":%q}},`+
			`%q:{"agent_id":{"tool":%q,"id":"id","model":%q}}}}`,
		coverageSchema, commit,
		coverageSession, longValue, longValue,
		coverageSessionAlt, longValue, longValue,
	)
	data := coverageNoteWithRawMetadata(strings.TrimSuffix(attestations.String(), "\n"), metadata)
	if len(data) >= maxGitAINoteBytes {
		t.Fatalf("large Git AI fixture is too large: %d", len(data))
	}
	if err := repo.WriteNoteRef(GitAINotesRef, commit, data); err != nil {
		t.Fatal(err)
	}
	result, err := ImportGitAI(repo, commit, false)
	if err == nil || result.Imported != 0 {
		t.Fatalf("ImportGitAI large note = %+v, %v", result, err)
	}
}

func testImportGitAIInvalidRange(t *testing.T) {
	_, _, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	if _, err := ImportGitAI(repo, coverageBadRevision, false); err == nil {
		t.Fatal("ImportGitAI accepted an invalid revision range")
	}
}

func TestInteropCoverageBylineAndBinaryErrors(t *testing.T) {
	t.Run("ToByline binary", testToBylineBinary)
	t.Run("ToByline missing metadata", testToBylineMissingMetadata)
	t.Run("readByline malformed note", testReadBylineMalformedNote)
	t.Run("readByline binary", testReadBylineBinary)
	t.Run("readByline invalid ranges", testReadBylineInvalidRanges)
	t.Run("readByline invalid ref", testReadBylineInvalidRef)
}

func testToBylineBinary(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, string([]byte{0}), "binary")
	note := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			coverageFile: {{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 1, End: 1}}}},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
	}
	if _, err := note.ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted binary content")
	}
}

func TestToBylineGitErrors(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, "Git errors")
	note := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			coverageFile: {{Key: coverageHuman, Ranges: []GitAILineRange{{Start: 1, End: 1}}}},
		},
		Humans: map[string]GitAIHuman{coverageHuman: {}},
	}
	invalidRepo := discoverInteropFakeRepo(t, root, `ls-tree) echo "tree failure" >&2; exit 1 ;;`)
	if _, err := note.ToBylineNote(invalidRepo, commit); err == nil {
		t.Fatal("ToBylineNote accepted BlobID failure")
	}
	blob := mustInteropBlob(t, repo, commit, coverageFile)
	fakeRepo := discoverInteropFakeRepo(t, root, fmt.Sprintf(`
ls-tree) printf '100644 blob %s\t%s\0' %s %s ;;
cat-file) echo "blob failure" >&2; exit 1 ;;
*) exit 1 ;;
`, blob, coverageFile, blob, coverageFile))
	t.Setenv("FAKE_COMMIT", commit)
	if _, err := note.ToBylineNote(fakeRepo, commit); err == nil {
		t.Fatal("ToBylineNote accepted ReadBlob failure")
	}
}

func testToBylineMissingMetadata(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	note := GitAINote{
		BaseCommit: commit,
		Files: map[string][]GitAIAttestation{
			coverageFile: {{Key: coveragePrompt, Ranges: []GitAILineRange{{Start: 1, End: 1}}}},
		},
		Prompts: map[string]GitAIPrompt{},
	}
	if _, err := note.ToBylineNote(repo, commit); err == nil {
		t.Fatal("ToBylineNote accepted missing prompt metadata")
	}
}

func testReadBylineMalformedNote(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	if err := repo.WriteNoteRef("refs/notes/byline", commit, []byte(coverageBadValue)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted malformed note")
	}
}

func testReadBylineBinary(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, string([]byte{0}), "binary")
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 1)
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted binary content")
	}
}

func testReadBylineInvalidRanges(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	writeCoverageUntrackedNote(t, repo, commit, coverageFile, 2)
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted out-of-bounds ranges")
	}
}

func testReadBylineInvalidRef(t *testing.T) {
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, coverageMessage)
	setInteropRefToBlob(t, root, "refs/notes/byline")
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted an invalid notes ref")
	}
}

func TestInteropCoverageEmptyTraceAndDecode(t *testing.T) {
	t.Run("missing prompts", testDecodeMissingPrompts)
	t.Run("empty trace file", testExportEmptyTraceFile)
	t.Run("empty quoted path", testEmptyQuotedPath)
	t.Run("equal range starts", testEqualRangeStarts)
}

func testDecodeMissingPrompts(t *testing.T) {
	data := []byte("---\n" + coverageMetadataJSON(t, map[string]any{
		"schema_version":  coverageSchema,
		"base_commit_sha": coverageCommit,
		"prompts":         nil,
	}))
	if _, err := DecodeGitAI(data); err == nil {
		t.Fatal("DecodeGitAI accepted nil prompts")
	}
}

func testExportEmptyTraceFile(t *testing.T) {
	_, commit, repo := coverageRepoWithFile(t, "", "empty")
	blob := mustInteropBlob(t, repo, commit, coverageFile)
	note, err := notes.Encode(model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			coverageFile: {Blob: blob},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(commit, note); err != nil {
		t.Fatal(err)
	}
	data, err := ExportAgentTrace(repo, commit)
	if err != nil {
		t.Fatal(err)
	}
	var record AgentTraceRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if len(record.Files) != 0 || record.Metadata != nil {
		t.Fatalf("empty trace record = %+v", record)
	}
}

func testEmptyQuotedPath(t *testing.T) {
	if _, err := parseGitAIPath(`""`); err == nil {
		t.Fatal("parseGitAIPath accepted empty quoted path")
	}
}

func testEqualRangeStarts(t *testing.T) {
	err := validateAttestationRanges([]GitAIAttestation{{
		Ranges: []GitAILineRange{
			{Start: 1, End: 3},
			{Start: 1, End: 2},
		},
	}})
	if err == nil {
		t.Fatal("validateAttestationRanges accepted equal-start overlap")
	}
}

func TestReadBylineGitErrors(t *testing.T) {
	t.Run("blob lookup", testReadBylineBlobLookupError)
	t.Run("blob read", testReadBylineBlobReadError)
}

func testReadBylineBlobLookupError(t *testing.T) {
	root, note, commit, _ := coverageFakeReadBylineFixture(t)
	if err := os.WriteFile(filepath.Join(root, "note"), note, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := discoverInteropFakeRepo(t, root, `
notes) cat "$FAKE_ROOT/note" ;;
ls-tree) echo "tree failure" >&2; exit 1 ;;
*) exit 1 ;;
`)
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted BlobID failure")
	}
}

func testReadBylineBlobReadError(t *testing.T) {
	root, note, commit, blob := coverageFakeReadBylineFixture(t)
	if err := os.WriteFile(filepath.Join(root, "note"), note, 0o600); err != nil {
		t.Fatal(err)
	}
	repo := discoverInteropFakeRepo(t, root, fmt.Sprintf(`
notes) cat "$FAKE_ROOT/note" ;;
ls-tree) printf '100644 blob %s\t%s\0' %s %s ;;
cat-file) echo "blob failure" >&2; exit 1 ;;
*) exit 1 ;;
`, blob, coverageFile, blob, coverageFile))
	if _, _, err := readByline(repo, commit); err == nil {
		t.Fatal("readByline accepted ReadBlob failure")
	}
}

func TestInteropCoverageImportCommits(t *testing.T) {
	t.Run("empty HEAD", testImportCommitsEmptyHead)
	t.Run("HEAD error", testImportCommitsHeadError)
	t.Run("too many commits", testImportCommitsTooMany)
}

func testImportCommitsEmptyHead(t *testing.T) {
	root := t.TempDir()
	runInteropGit(t, root, "init", "-b", "main")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	commits, err := importCommits(repo, "")
	if err != nil || commits != nil {
		t.Fatalf("importCommits(empty HEAD) = %v, %v", commits, err)
	}
}

func testImportCommitsHeadError(t *testing.T) {
	repo := &gitcmd.Repo{Root: t.TempDir()}
	if _, err := importCommits(repo, ""); err == nil {
		t.Fatal("importCommits accepted a repository with no Git executable")
	}
}

func testImportCommitsTooMany(t *testing.T) {
	root := interopRepo(t)
	makeInteropFastImport(t, root, maxImportCommits+1)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := importCommits(repo, coverageRevision); err == nil {
		t.Fatal("importCommits accepted too many commits")
	}
}

func TestGitAIEntriesUpdatesStart(t *testing.T) {
	index := newCheckpointIndex(nil)
	entries, _, _, err := gitAIEntries(coverageFile, []model.Range{
		{Start: 3, End: 3, Attribution: model.Attribution{Author: model.AuthorHuman}},
		{Start: 2, End: 2, Attribution: model.Attribution{Author: model.AuthorHuman}},
	}, index)
	if err != nil || len(entries) != 1 || entries[0].start != 2 {
		t.Fatalf("gitAIEntries start = %+v, %v", entries, err)
	}
}

func TestGitAIPromptAttributionValidation(t *testing.T) {
	note := GitAINote{
		Prompts: map[string]GitAIPrompt{
			coveragePrompt: {},
		},
	}
	if _, err := note.attribution(coveragePrompt); err == nil {
		t.Fatal("attribution accepted invalid prompt metadata")
	}
}

func storeCheckpointForCoverage(t *testing.T, repo *gitcmd.Repo, checkpoint model.Checkpoint) error {
	t.Helper()
	return store.New(repo.GitDir).AppendCheckpoint(checkpoint)
}

func setInteropRefToBlob(t *testing.T, root, ref string) {
	t.Helper()
	objectPath := filepath.Join(t.TempDir(), "object")
	if err := os.WriteFile(objectPath, []byte("not a notes tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	object := strings.TrimSpace(runInteropGit(t, root, "hash-object", "-w", objectPath))
	runInteropGit(t, root, "update-ref", ref, object)
}

func discoverInteropFakeRepo(t *testing.T, root, body string) *gitcmd.Repo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake Git is not executable on Windows")
	}
	binDir := t.TempDir()
	path := filepath.Join(binDir, "git")
	script := `#!/bin/sh
if [ "$1" = rev-parse ]; then
	case "$2" in
	--show-toplevel) printf '%s\n' "$FAKE_ROOT" ;;
	--path-format=absolute)
		case "$3" in
		--git-dir) printf '%s/.git\n' "$FAKE_ROOT" ;;
		--git-common-dir) printf '%s/.git\n' "$FAKE_ROOT" ;;
		*) exit 1 ;;
		esac
		;;
	*) exit 1 ;;
	esac
else
	case "$1" in
` + body + `
	esac
fi
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_ROOT", root)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return repo
}

func coverageFakeReadBylineFixture(t *testing.T) (string, []byte, string, string) {
	t.Helper()
	root, commit, repo := coverageRepoWithFile(t, coverageOneLine, "fake read byline")
	blob := mustInteropBlob(t, repo, commit, coverageFile)
	note := coverageUntrackedNote(t, blob, coverageFile, 1)
	return root, note, commit, blob
}

func makeInteropFastImport(t *testing.T, root string, count int) {
	t.Helper()
	var stream strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&stream, "commit refs/heads/main\nmark :%d\n", index)
		fmt.Fprintf(&stream, "author Test User <test@example.invalid> %d +0000\n", index)
		fmt.Fprintf(&stream, "committer Test User <test@example.invalid> %d +0000\n", index)
		stream.WriteString("data 1\nx\n")
		if index > 1 {
			fmt.Fprintf(&stream, "from :%d\n", index-1)
		}
		stream.WriteString("M 100644 inline file.txt\n")
		stream.WriteString("data 1\nx\n")
	}
	command := exec.Command("git", "fast-import")
	command.Dir = root
	command.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
	)
	command.Stdin = strings.NewReader(stream.String())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git fast-import: %v\n%s", err, output)
	}
}
