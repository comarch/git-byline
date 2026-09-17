//go:build !windows

package store

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func runFileSizeLimitTest(t *testing.T, mode string, limit uint64) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestFileSizeLimitChild$", "-test.v")
	command.Env = append(os.Environ(),
		"GIT_BYLINE_FILE_LIMIT_MODE="+mode,
		"GIT_BYLINE_FILE_LIMIT="+strconv.FormatUint(limit, 10),
	)
	output, err := command.CombinedOutput()
	if bytes.Contains(output, []byte("--- SKIP: TestFileSizeLimitChild")) {
		t.Skip("file-size limit unavailable in child process")
	}
	if err != nil {
		t.Fatalf("file-size child: %v\n%s", err, output)
	}
}

func TestFileSizeLimitChild(t *testing.T) {
	mode := os.Getenv("GIT_BYLINE_FILE_LIMIT_MODE")
	if mode == "" {
		return
	}
	limit, err := strconv.ParseUint(os.Getenv("GIT_BYLINE_FILE_LIMIT"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Skipf("file-size limit unavailable: %v", err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
			t.Errorf("restore file-size limit: %v", err)
		}
		signal.Reset(syscall.SIGXFSZ)
	}()
	restricted := original
	restricted.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &restricted); err != nil {
		t.Skipf("cannot set file-size limit: %v", err)
	}
	switch mode {
	case "append":
		if err := New(t.TempDir()).AppendCheckpoint(validCheckpoint(1)); err == nil {
			t.Fatal("AppendCheckpoint accepted a partial write")
		}
	case "atomic":
		dir := t.TempDir()
		if err := writeAtomicFile(
			dir,
			dir+"/state.json",
			"state-*.tmp",
			"state",
			[]byte("larger than one byte"),
		); err == nil {
			t.Fatal("writeAtomicFile accepted a partial write")
		}
	default:
		t.Fatalf("unknown file-size test mode %q", mode)
	}
}

func TestAppendCheckpointWriteError(t *testing.T) {
	runFileSizeLimitTest(t, "append", 1)
}

func TestWriteAtomicFileWriteError(t *testing.T) {
	runFileSizeLimitTest(t, "atomic", 1)
}

func TestRewriteCheckpointBasesReadError(t *testing.T) {
	value := New(t.TempDir())
	if err := os.MkdirAll(value.CheckpointPath(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := value.RewriteCheckpointBases(
		"refs/heads/main",
		map[string]string{"aaaa": "bbbb"},
	); err == nil {
		t.Fatal("RewriteCheckpointBases accepted an unreadable checkpoint path")
	}
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
