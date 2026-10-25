package gitcmd

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/comarch/git-byline/internal/model"
)

// maxBatchArgBytes keeps one ls-tree path chunk far below the Windows
// command line limit of 32767 characters.
const maxBatchArgBytes = 16 << 10

// objectBatch is one long-lived git cat-file --batch process. Annotating a
// large merge reads hundreds of blobs, and one Git process per blob costs
// minutes on systems with slow process creation.
type objectBatch struct {
	mu      sync.Mutex
	command *exec.Cmd
	cancel  context.CancelFunc
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	broken  bool
}

type treeEntry struct {
	blob   string
	exists bool
}

// batchScope holds what StartBatch keeps until its stop function runs.
type batchScope struct {
	objects  *objectBatch
	unusable bool
	ignored  map[string]bool
}

// StartBatch opens a span in which ReadBlob and ReadNoteBlob share one
// cat-file process, started on the first read, and Ignored answers from
// PrefetchIgnored results. The stop function ends the process and drops the
// ignore results, because .gitignore files can change between commands.
// A nested StartBatch returns a stop function that does nothing.
func (repo *Repo) StartBatch() func() {
	if repo.batch != nil {
		return func() {}
	}
	scope := &batchScope{}
	repo.batch = scope
	return func() {
		repo.batch = nil
		if scope.objects != nil {
			scope.objects.close()
		}
	}
}

// readBatched reads one blob through the batch process. It reports false
// when the caller must run its own cat-file command instead: no batch span
// is open, the process cannot start or broke, or the object is missing or
// not a blob. Those rare cases keep the exact errors of a single command.
func (repo *Repo) readBatched(operation, oid string, limit int) ([]byte, bool, error) {
	if repo.batch == nil || repo.batch.unusable {
		return nil, false, nil
	}
	if repo.batch.objects == nil {
		objects, err := repo.startObjectBatch()
		if err != nil {
			repo.batch.unusable = true
			return nil, false, nil
		}
		repo.batch.objects = objects
	}
	return repo.batch.objects.read(operation, oid, limit)
}

func (repo *Repo) startObjectBatch() (*objectBatch, error) {
	ctx, cancel := context.WithCancel(context.Background())
	command := exec.CommandContext(ctx, repo.gitBin, "cat-file", "--batch")
	command.Dir = repo.Root
	command.Env = append(gitEnvironment(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_BYLINE_NESTED=1",
		"LC_ALL=C",
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start object batch: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("start object batch: %w", err)
	}
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start object batch: %w", err)
	}
	return &objectBatch{
		command: command,
		cancel:  cancel,
		stdin:   stdin,
		stdout:  bufio.NewReaderSize(stdout, 64<<10),
	}, nil
}

// close ends the process. Every answer it owes was already read, so it is
// stopped instead of waiting for it to notice the closed input.
func (batch *objectBatch) close() {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	_ = batch.stdin.Close()
	batch.cancel()
	_ = batch.command.Wait()
}

// read returns one blob of at most limit bytes. A larger blob is skipped
// and reported with ErrOutputLimit, like a limited single command. It
// reports false for everything it does not answer itself.
func (batch *objectBatch) read(operation, oid string, limit int) ([]byte, bool, error) {
	batch.mu.Lock()
	defer batch.mu.Unlock()
	if batch.broken {
		return nil, false, nil
	}
	timer := time.AfterFunc(commandTimeout, batch.cancel)
	data, outcome, err := batch.readLocked(oid, limit)
	if !timer.Stop() || err != nil {
		// A timeout or a protocol error leaves the stream at an unknown
		// position, so no later read can trust it.
		batch.broken = true
		return nil, false, nil
	}
	switch outcome {
	case batchBlob:
		return data, true, nil
	case batchTooLarge:
		return nil, true, fmt.Errorf("git %s: %w of %d bytes", operation, ErrOutputLimit, limit)
	default:
		return nil, false, nil
	}
}

type batchOutcome int

const (
	batchOther batchOutcome = iota
	batchBlob
	batchTooLarge
)

// readLocked returns an error only when the stream is out of step. A
// missing object or one that is not a blob is batchOther.
func (batch *objectBatch) readLocked(oid string, limit int) ([]byte, batchOutcome, error) {
	if _, err := io.WriteString(batch.stdin, oid+"\n"); err != nil {
		return nil, batchOther, err
	}
	header, err := batch.stdout.ReadString('\n')
	if err != nil {
		return nil, batchOther, err
	}
	fields := strings.Fields(header)
	if len(fields) == 2 && fields[0] == oid && fields[1] == "missing" {
		return nil, batchOther, nil
	}
	if len(fields) != 3 || fields[0] != oid {
		return nil, batchOther, errors.New("invalid cat-file batch header")
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return nil, batchOther, errors.New("invalid cat-file batch size")
	}
	if fields[1] != "blob" || size > int64(limit) {
		if _, err := io.CopyN(io.Discard, batch.stdout, size+1); err != nil {
			return nil, batchOther, err
		}
		if fields[1] != "blob" {
			return nil, batchOther, nil
		}
		return nil, batchTooLarge, nil
	}
	data := make([]byte, size+1)
	if _, err := io.ReadFull(batch.stdout, data); err != nil {
		return nil, batchOther, err
	}
	if data[size] != '\n' {
		return nil, batchOther, errors.New("invalid cat-file batch terminator")
	}
	return data[:size], batchBlob, nil
}

// PrefetchBlobIDs resolves many paths at revision with a few ls-tree runs
// and caches the result for BlobID. Revision is an object ID, so its tree
// never changes and the cache stays valid. Paths whose entry is not a blob
// are left out, so BlobID reports them the same way as before.
func (repo *Repo) PrefetchBlobIDs(revision string, paths []string) error {
	if !model.ValidObjectID(revision) {
		return errors.New("invalid revision object ID")
	}
	wanted := map[string]bool{}
	var pending []string
	for _, path := range paths {
		normalized, err := NormalizePath(path)
		if err != nil {
			continue
		}
		if _, cached := repo.trees[revision][normalized]; cached || wanted[normalized] {
			continue
		}
		wanted[normalized] = true
		pending = append(pending, normalized)
	}
	if len(pending) == 0 {
		return nil
	}
	if repo.trees == nil {
		repo.trees = map[string]map[string]treeEntry{}
	}
	if repo.trees[revision] == nil {
		repo.trees[revision] = map[string]treeEntry{}
	}
	entries := repo.trees[revision]
	for len(pending) > 0 {
		chunk, size := 0, 0
		for chunk < len(pending) && (chunk == 0 || size+len(pending[chunk])+1 <= maxBatchArgBytes) {
			size += len(pending[chunk]) + 1
			chunk++
		}
		args := append([]string{"--literal-pathspecs", "ls-tree", "-r", "-t", "-z", revision, "--"}, pending[:chunk]...)
		found := map[string]treeEntry{}
		nonBlob := map[string]bool{}
		err := repo.streamRecords("read tree entries", 0, func(record string) error {
			meta, path, ok := strings.Cut(record, "\t")
			if !ok {
				return errors.New("git returned invalid tree entry")
			}
			if !wanted[path] {
				return nil
			}
			fields := strings.Fields(meta)
			if len(fields) != 3 || !model.ValidObjectID(fields[2]) {
				return errors.New("git returned invalid tree entry")
			}
			if fields[1] != "blob" {
				nonBlob[path] = true
				return nil
			}
			found[path] = treeEntry{blob: fields[2], exists: true}
			return nil
		}, args...)
		if err != nil {
			return err
		}
		for _, path := range pending[:chunk] {
			if nonBlob[path] {
				continue
			}
			entries[path] = found[path]
		}
		pending = pending[chunk:]
	}
	return nil
}

// PrefetchIgnored checks many worktree paths with one check-ignore run and
// keeps the answers for Ignored until the batch stops. Without an open
// batch span it does nothing. A failed run, for example a path beyond a
// symbolic link, leaves every path to its own check-ignore call.
func (repo *Repo) PrefetchIgnored(paths []string) {
	if repo.batch == nil || len(paths) == 0 {
		return
	}
	var input strings.Builder
	asked := map[string]bool{}
	for _, path := range paths {
		normalized, err := NormalizePath(path)
		if err != nil || asked[normalized] {
			continue
		}
		asked[normalized] = true
		input.WriteString(normalized)
		input.WriteByte(0)
	}
	if len(asked) == 0 {
		return
	}
	out, err := repo.run("check ignored paths", strings.NewReader(input.String()), "check-ignore", "-z", "--stdin")
	if err != nil {
		var commandErr *CommandError
		if !errors.As(err, &commandErr) || commandErr.ExitCode != 1 {
			return
		}
		out = nil
	}
	ignored := map[string]bool{}
	for path := range asked {
		ignored[path] = false
	}
	for _, path := range splitNUL(out) {
		if !asked[path] {
			return
		}
		ignored[path] = true
	}
	repo.batch.ignored = ignored
}

// HashContent returns the blob object ID that hash-object --stdin assigns to
// content, without writing it. Standard input skips attribute filters, so
// the ID is the hash of the object header and the bytes.
func (repo *Repo) HashContent(content []byte) (string, error) {
	length, err := repo.ObjectIDLength()
	if err != nil {
		return "", err
	}
	hasher := sha1.New()
	if length == 64 {
		hasher = sha256.New()
	}
	fmt.Fprintf(hasher, "blob %d\x00", len(content))
	hasher.Write(content)
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
