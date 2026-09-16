package store

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/model"
)

const (
	lineBreak             = "\n"
	testBlobID            = "abcd1234"
	testFilePath          = "file"
	testPadding           = "x"
	invalidJSONLine       = "{bad}\n"
	unknownFieldLine      = `{"version":1,"unknown":true}` + lineBreak
	legacyStateTemplate   = `{"version":1,"notes_version":%d,"pending":{}}`
	unsupportedStateJSON  = `{"version":1,"notes_version":99,"pending":{}}`
	invalidPendingBase    = `{"version":1,"notes_version":3,"pending":{"base_commit":"bad"}}`
	malformedTrailingJSON = "{}{"
)

func writeStoreFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkpointJSON(t *testing.T, record model.Checkpoint) []byte {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func TestCheckpointReadAdditionalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		setup       func(*testing.T) Store
		skipWindows bool
	}{
		{
			name:        "parent is regular file",
			skipWindows: true,
			setup: func(t *testing.T) Store {
				path := filepath.Join(t.TempDir(), "not-a-directory")
				writeStoreFile(t, path, []byte(testPadding))
				return Store{Dir: path}
			},
		},
		{
			name: "checkpoint path is directory",
			setup: func(t *testing.T) Store {
				value := New(t.TempDir())
				if err := os.MkdirAll(value.Dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(value.CheckpointPath(), 0o700); err != nil {
					t.Fatal(err)
				}
				return value
			},
		},
		{
			name: "record exceeds line limit",
			setup: func(t *testing.T) Store {
				value := New(t.TempDir())
				data := []byte(strings.Repeat(testPadding, maxRecordBytes+2))
				writeStoreFile(t, value.CheckpointPath(), data)
				return value
			},
		},
		{
			name: "strict decode rejects unknown field",
			setup: func(t *testing.T) Store {
				value := New(t.TempDir())
				writeStoreFile(t, value.CheckpointPath(), []byte(unknownFieldLine))
				return value
			},
		},
		{
			name: "validation rejects invalid record",
			setup: func(t *testing.T) Store {
				value := New(t.TempDir())
				writeStoreFile(t, value.CheckpointPath(), checkpointJSON(t, validCheckpoint(0)))
				return value
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if test.skipWindows && runtime.GOOS == "windows" {
				t.Skip("regular-file parent errors differ on Windows")
			}
			if _, _, err := test.setup(t).ReadCheckpoints(); err == nil {
				t.Fatal("ReadCheckpoints accepted invalid input")
			}
		})
	}
}

func TestDropCheckpointRecordsReadFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links are not reliable on Windows")
	}
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(value.Dir, "missing"), value.CheckpointPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := value.DropCheckpointRecords(map[uint64]bool{1: true}); err == nil {
		t.Fatal("DropCheckpointRecords accepted a broken checkpoint link")
	}
}

func TestCheckpointValidationAdditional(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		record model.Checkpoint
	}{
		{
			name: "zero sequence",
			record: func() model.Checkpoint {
				value := validCheckpoint(1)
				value.Seq = 0
				return value
			}(),
		},
		{
			name: "invalid event id",
			record: func() model.Checkpoint {
				value := validCheckpoint(1)
				value.EventID = "\x00"
				return value
			}(),
		},
		{
			name: "ai attribution without agent",
			record: func() model.Checkpoint {
				value := validCheckpoint(1)
				value.Type = model.AuthorAI
				return value
			}(),
		},
		{
			name: "empty file path",
			record: func() model.Checkpoint {
				value := validCheckpoint(1)
				value.Files[0].Path = ""
				return value
			}(),
		},
		{
			name: "missing file has blob",
			record: func() model.Checkpoint {
				value := validCheckpoint(1)
				value.Files[0].Exists = false
				return value
			}(),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := New(t.TempDir())
			if err := value.CheckCheckpointAppend(test.record, 0); err == nil {
				t.Fatal("CheckCheckpointAppend accepted invalid checkpoint")
			}
		})
	}
}

func TestCheckCheckpointAppendBranches(t *testing.T) {
	t.Parallel()
	value := New(t.TempDir())
	if err := value.CheckCheckpointAppend(validCheckpoint(1), 0); err != nil {
		t.Fatalf("valid append check = %v", err)
	}
	if err := value.CheckCheckpointAppend(validCheckpoint(1), -1); err == nil {
		t.Fatal("negative record count was accepted")
	}
	if err := value.CheckCheckpointAppend(validCheckpoint(1), maxCheckpointRecords); err == nil {
		t.Fatal("maximum record count was accepted")
	}

	large := validCheckpoint(1)
	large.Files[0].Path = strings.Repeat(testPadding, maxRecordBytes)
	if err := value.CheckCheckpointAppend(large, 0); err == nil {
		t.Fatal("oversized checkpoint was accepted")
	}

	t.Run("parent is regular file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("regular-file parent errors differ on Windows")
		}
		parent := filepath.Join(t.TempDir(), testFilePath)
		writeStoreFile(t, parent, []byte(testPadding))
		if err := (Store{Dir: parent}).CheckCheckpointAppend(validCheckpoint(1), 0); err == nil {
			t.Fatal("checkpoint append accepted an invalid parent")
		}
	})

	full := New(t.TempDir())
	if err := os.MkdirAll(full.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(full.CheckpointPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCheckpointBytes); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if _, err := file.WriteAt([]byte{'\n'}, maxCheckpointBytes-1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := full.CheckCheckpointAppend(validCheckpoint(1), 0); err == nil {
		t.Fatal("checkpoint append exceeded size limit without error")
	}
}

func TestInspectCheckpointTailAdditionalErrors(t *testing.T) {
	t.Parallel()
	t.Run("parent is regular file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("regular-file parent errors differ on Windows")
		}
		parent := filepath.Join(t.TempDir(), testFilePath)
		writeStoreFile(t, parent, []byte(testPadding))
		if _, _, _, err := inspectCheckpointTail(filepath.Join(parent, "tail"), false); err == nil {
			t.Fatal("inspectCheckpointTail accepted invalid parent")
		}
	})

	oversized := filepath.Join(t.TempDir(), "oversized")
	file, err := os.OpenFile(oversized, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxCheckpointBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inspectCheckpointTail(oversized, false); err == nil {
		t.Fatal("inspectCheckpointTail accepted oversized log")
	}

	directory := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	// Windows opens directories without an error, so the rejection is
	// unix behavior only.
	if runtime.GOOS != "windows" {
		if _, _, _, err := inspectCheckpointTail(directory, false); err == nil {
			t.Fatal("inspectCheckpointTail read a directory tail")
		}
	}

	tooLong := filepath.Join(t.TempDir(), "too-long")
	writeStoreFile(t, tooLong, []byte(strings.Repeat(testPadding, maxRecordBytes+2)))
	if _, _, _, err := inspectCheckpointTail(tooLong, false); err == nil {
		t.Fatal("inspectCheckpointTail accepted an oversized tail")
	}

	invalid := filepath.Join(t.TempDir(), "invalid")
	writeStoreFile(t, invalid, []byte("{bad}"))
	if _, _, _, err := inspectCheckpointTail(invalid, false); err == nil {
		t.Fatal("inspectCheckpointTail accepted malformed JSON")
	}

	invalidRecord := filepath.Join(t.TempDir(), "invalid-record")
	writeStoreFile(t, invalidRecord, bytes.TrimSuffix(checkpointJSON(t, validCheckpoint(0)), []byte{'\n'}))
	if _, _, _, err := inspectCheckpointTail(invalidRecord, false); err == nil {
		t.Fatal("inspectCheckpointTail accepted invalid checkpoint")
	}
}

func TestAppendCheckpointFailureBranches(t *testing.T) {
	t.Run("store directory creation", func(t *testing.T) {
		t.Chdir(t.TempDir())
		if err := (Store{}).AppendCheckpoint(validCheckpoint(1)); err == nil {
			t.Fatal("AppendCheckpoint created an empty store directory")
		}
	})

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		return
	}
	t.Run("checkpoint device rejects permission change", func(t *testing.T) {
		value := New(t.TempDir())
		if err := os.MkdirAll(value.Dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("/dev/null", value.CheckpointPath()); err != nil {
			t.Fatal(err)
		}
		if err := value.AppendCheckpoint(validCheckpoint(1)); err == nil {
			t.Fatal("AppendCheckpoint changed device permissions")
		}
	})
	t.Run("tail repair requires writable log", func(t *testing.T) {
		value := New(t.TempDir())
		writeStoreFile(t, value.CheckpointPath(), []byte(mustJSON(validCheckpoint(1))+lineBreak))
		if err := os.Chmod(value.CheckpointPath(), 0o400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(value.CheckpointPath(), 0o600)
		})
		if err := value.AppendCheckpoint(validCheckpoint(2)); err == nil {
			t.Fatal("AppendCheckpoint wrote a read-only checkpoint log")
		}
	})

	t.Run("atomic checkpoint rewrite cannot create temp", func(t *testing.T) {
		value := New(t.TempDir())
		writeStoreFile(t, value.CheckpointPath(), []byte(mustJSON(validCheckpoint(1))+lineBreak))
		if err := os.Chmod(value.Dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(value.Dir, 0o700)
		})
		if _, err := value.DropCheckpointRecords(map[uint64]bool{1: true}); err == nil {
			t.Fatal("DropCheckpointRecords wrote through a read-only directory")
		}
	})
}

func TestStateReadAdditionalCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
	}{
		{name: "malformed JSON", data: invalidJSONLine},
		{name: "unsupported notes version", data: unsupportedStateJSON},
		{name: "invalid pending base", data: invalidPendingBase},
		{name: "legacy notes version one", data: stateJSONWithNotesVersion(model.NoteVersionV1)},
		{name: "legacy notes version two", data: stateJSONWithNotesVersion(model.NoteVersionV2)},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			value := New(t.TempDir())
			writeStoreFile(t, value.StatePath(), []byte(test.data))
			state, err := value.ReadState()
			if test.name == "legacy notes version one" || test.name == "legacy notes version two" {
				if err != nil || state.NotesVersion != model.NoteVersion {
					t.Fatalf("ReadState() = %+v, %v", state, err)
				}
				return
			}
			if err == nil {
				t.Fatal("ReadState accepted invalid state")
			}
		})
	}

	value := New(t.TempDir())
	writeStoreFile(t, value.StatePath(), []byte(`{"version":1,"notes_version":3,"pending":{}}`))
	state, err := value.ReadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.Pending.Files == nil {
		t.Fatal("ReadState did not initialize pending files")
	}

	t.Run("parent is regular file", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("regular-file parent errors differ on Windows")
		}
		parent := filepath.Join(t.TempDir(), testFilePath)
		writeStoreFile(t, parent, []byte(testPadding))
		if _, err := (Store{Dir: parent}).ReadState(); err == nil {
			t.Fatal("ReadState accepted an invalid parent")
		}
	})

	directoryStore := New(t.TempDir())
	if err := os.MkdirAll(directoryStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directoryStore.StatePath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := directoryStore.ReadState(); err == nil {
		t.Fatal("ReadState accepted a directory as state")
	}
}

func TestStateValidationAdditional(t *testing.T) {
	t.Parallel()
	tests := []model.State{
		func() model.State {
			value := model.NewState()
			value.Pending.BaseCommit = "bad"
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Pending.Files[""] = model.PendingFile{Blob: testBlobID}
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Pending.Files["dir/.git/config"] = model.PendingFile{Blob: testBlobID}
			return value
		}(),
		func() model.State {
			value := model.NewState()
			value.Pending.Files["file.go"] = model.PendingFile{
				Blob: testBlobID,
				Ranges: []model.Range{{
					Start: 2,
					End:   1,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			}
			return value
		}(),
	}
	for index, state := range tests {
		index, state := index, state
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			t.Parallel()
			if err := (New(t.TempDir())).CheckStateWrite(state); err == nil {
				t.Fatal("CheckStateWrite accepted invalid state")
			}
		})
	}
}

func TestWriteAtomicFailureBranches(t *testing.T) {
	t.Parallel()
	parent := filepath.Join(t.TempDir(), testFilePath)
	writeStoreFile(t, parent, []byte(testPadding))
	if err := writeAtomicFile(parent, filepath.Join(parent, "target"), "test-*.tmp", "test", []byte(testPadding)); err == nil {
		t.Fatal("writeAtomicFile created a file below a regular file")
	}

	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(value.StatePath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := value.WriteState(model.NewState()); err == nil {
		t.Fatal("WriteState replaced a directory")
	}
}

func TestCheckStateWriteAndNilPendingFiles(t *testing.T) {
	t.Parallel()
	value := model.NewState()
	value.Pending.Files = nil
	if err := (New(t.TempDir())).CheckStateWrite(value); err != nil {
		t.Fatal(err)
	}
	if err := (New(t.TempDir())).CheckStateWrite(model.State{}); err == nil {
		t.Fatal("CheckStateWrite accepted invalid state")
	}
}

func TestDecodeStrictAdditionalErrors(t *testing.T) {
	t.Parallel()
	tests := []string{invalidJSONLine, malformedTrailingJSON}
	for _, data := range tests {
		data := data
		t.Run(data, func(t *testing.T) {
			t.Parallel()
			var target map[string]any
			if err := decodeStrict([]byte(data), &target); err == nil {
				t.Fatal("decodeStrict accepted malformed JSON")
			}
		})
	}
	var target map[string]any
	if err := decodeStrict([]byte("{}{}"), &target); err == nil {
		t.Fatal("decodeStrict accepted multiple JSON values")
	}
}

func stateJSONWithNotesVersion(version int) string {
	return strings.TrimSpace(
		sprintfStateJSON(version),
	)
}

func sprintfStateJSON(version int) string {
	return strings.Replace(legacyStateTemplate, "%d", string(rune('0'+version)), 1)
}
