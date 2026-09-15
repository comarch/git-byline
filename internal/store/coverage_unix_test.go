//go:build !windows

package store

import (
	"bytes"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

func withFileSizeLimit(t *testing.T, limit uint64, test func() error) {
	t.Helper()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Skipf("file-size limit unavailable: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	defer func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)
		signal.Reset(syscall.SIGXFSZ)
	}()
	restricted := original
	restricted.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &restricted); err != nil {
		t.Skipf("cannot set file-size limit: %v", err)
	}
	if err := test(); err != nil {
		t.Fatal(err)
	}
}

func TestAppendCheckpointWriteError(t *testing.T) {
	value := New(t.TempDir())
	withFileSizeLimit(t, 1, func() error {
		if err := value.AppendCheckpoint(validCheckpoint(1)); err == nil {
			return errors.New("AppendCheckpoint accepted a partial write")
		}
		return nil
	})
}

func TestWriteAtomicFileWriteError(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/state.json"
	withFileSizeLimit(t, 1, func() error {
		if err := writeAtomicFile(dir, path, "state-*.tmp", "state", []byte("larger than one byte")); err == nil {
			return errors.New("writeAtomicFile accepted a partial write")
		}
		return nil
	})
}

func TestReadBoundedFileSpecialFiles(t *testing.T) {
	if _, err := readBoundedFile("/dev/zero", 1); err == nil {
		t.Fatal("readBoundedFile accepted an unbounded special file")
	}
	if _, err := readBoundedFile("/dev/fd", maxStateBytes); err == nil {
		t.Fatal("readBoundedFile accepted an unsupported special file")
	}
}

func TestReadCheckpointsDetectsGrowingStream(t *testing.T) {
	value := New(t.TempDir())
	if err := os.MkdirAll(value.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(value.CheckpointPath(), 0o600); err != nil {
		t.Fatal(err)
	}
	const (
		prefix = `{"version":9,"padding":"`
		suffix = `"}` + "\n"
	)
	makeLine := func(size int) []byte {
		return []byte(prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix)
	}
	data := bytes.Join([][]byte{
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes),
		makeLine(maxRecordBytes + 1),
	}, nil)
	written := make(chan error, 1)
	go func() {
		file, err := os.OpenFile(value.CheckpointPath(), os.O_WRONLY, 0)
		if err != nil {
			written <- err
			return
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil && !errors.Is(writeErr, syscall.EPIPE) {
			written <- writeErr
			return
		}
		if closeErr != nil && !errors.Is(closeErr, syscall.EPIPE) {
			written <- closeErr
			return
		}
		written <- nil
	}()
	if _, _, err := value.ReadCheckpoints(); err == nil {
		t.Fatal("ReadCheckpoints accepted a stream beyond its byte limit")
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
}

func TestInspectCheckpointTailEmptyAndTailLimit(t *testing.T) {
	empty := t.TempDir() + "/empty"
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, size, dropped, err := inspectCheckpointTail(empty, false); err != nil || size != 0 || dropped {
		t.Fatalf("empty tail = %d, %t, %v", size, dropped, err)
	}

	tooLong := t.TempDir() + "/too-long"
	if err := os.WriteFile(tooLong, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(tooLong, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxRecordBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := inspectCheckpointTail(tooLong, false); err == nil {
		t.Fatal("inspectCheckpointTail accepted an oversized tail")
	}
}
