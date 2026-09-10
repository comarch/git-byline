// Package gitcmd is the only production boundary for Git subprocesses.
package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/comarch/git-byline/internal/model"
)

const (
	commandTimeout = 30 * time.Second
	maxFileBytes   = 64 << 20
	maxOutputBytes = maxFileBytes
)

// ErrOutputLimit reports Git output larger than the supported file bound.
var ErrOutputLimit = errors.New("git output exceeds the supported limit")

// Repo describes one Git worktree.
type Repo struct {
	Root      string
	GitDir    string
	CommonDir string
	gitBin    string
}

// Change describes one path changed by a commit.
type Change struct {
	Status  byte
	OldPath string
	Path    string
}

// CommandError reports a failed Git operation without environment or content.
type CommandError struct {
	Operation string
	ExitCode  int
	Stderr    string
	Err       error
}

// IsNotRepository reports the ordinary rev-parse error outside a worktree.
func IsNotRepository(err error) bool {
	var commandErr *CommandError
	return errors.As(err, &commandErr) &&
		commandErr.Operation == "discover worktree" &&
		commandErr.ExitCode == 128 &&
		strings.Contains(strings.ToLower(commandErr.Stderr), "not a git repository")
}

func (err *CommandError) Error() string {
	if err.Stderr == "" {
		return fmt.Sprintf("git %s failed with exit code %d", err.Operation, err.ExitCode)
	}
	return fmt.Sprintf("git %s failed with exit code %d: %s", err.Operation, err.ExitCode, err.Stderr)
}

func (err *CommandError) Unwrap() error {
	return err.Err
}

// Discover finds the worktree containing dir.
func Discover(dir string) (*Repo, error) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("find git executable: %w", err)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	temp := &Repo{Root: absolute, gitBin: gitBin}
	root, err := temp.text("discover worktree", nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	gitDir, err := temp.text("discover git directory", nil, "rev-parse", "--path-format=absolute", "--git-dir")
	if err != nil {
		return nil, err
	}
	commonDir, err := temp.text("discover common git directory", nil, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	return &Repo{
		Root:      filepath.Clean(root),
		GitDir:    filepath.Clean(gitDir),
		CommonDir: filepath.Clean(commonDir),
		gitBin:    gitBin,
	}, nil
}

// Head returns HEAD or an empty value for an unborn repository.
func (repo *Repo) Head() (string, error) {
	out, err := repo.run("read HEAD", nil, "rev-parse", "--verify", "HEAD")
	if err != nil {
		var commandErr *CommandError
		if errors.As(err, &commandErr) && commandErr.ExitCode == 128 {
			return "", nil
		}
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if !model.ValidObjectID(value) {
		return "", fmt.Errorf("read HEAD: invalid object ID %q", value)
	}
	return value, nil
}

// Parent returns the first parent of commit, or empty for a root commit.
func (repo *Repo) Parent(commit string) (string, error) {
	parents, err := repo.Parents(commit)
	if err != nil {
		return "", err
	}
	if len(parents) == 0 {
		return "", nil
	}
	return parents[0], nil
}

// Parents returns all commit parents in Git order.
func (repo *Repo) Parents(commit string) ([]string, error) {
	if !model.ValidObjectID(commit) {
		return nil, errors.New("invalid commit object ID")
	}
	out, err := repo.run("read commit parents", nil, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 || fields[0] != commit {
		return nil, errors.New("git returned invalid commit parent data")
	}
	parents := make([]string, 0, len(fields)-1)
	for _, value := range fields[1:] {
		if !model.ValidObjectID(value) {
			return nil, errors.New("git returned invalid parent object ID")
		}
		parents = append(parents, value)
	}
	return parents, nil
}

// Changes returns changed paths for commit relative to its first parent.
func (repo *Repo) Changes(commit, parent string) ([]Change, error) {
	if !model.ValidObjectID(commit) {
		return nil, errors.New("invalid commit object ID")
	}
	args := []string{"diff-tree", "--no-commit-id", "--name-status", "-z", "-r", "-M"}
	if parent == "" {
		args = append(args, "--root", commit)
	} else {
		if !model.ValidObjectID(parent) {
			return nil, errors.New("invalid parent object ID")
		}
		args = append(args, parent, commit)
	}
	out, err := repo.run("list changed paths", nil, args...)
	if err != nil {
		return nil, err
	}
	fields := splitNUL(out)
	var changes []Change
	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			continue
		}
		kind := status[0]
		if kind == 'R' || kind == 'C' {
			if i+1 >= len(fields) {
				return nil, errors.New("git returned truncated rename data")
			}
			oldPath, err := NormalizePath(fields[i])
			if err != nil {
				return nil, fmt.Errorf("normalize old path: %w", err)
			}
			path, err := NormalizePath(fields[i+1])
			if err != nil {
				return nil, fmt.Errorf("normalize path: %w", err)
			}
			changes = append(changes, Change{Status: kind, OldPath: oldPath, Path: path})
			i += 2
			continue
		}
		if i >= len(fields) {
			return nil, errors.New("git returned truncated path data")
		}
		path, err := NormalizePath(fields[i])
		if err != nil {
			return nil, fmt.Errorf("normalize path: %w", err)
		}
		changes = append(changes, Change{Status: kind, Path: path})
		i++
	}
	return changes, nil
}

// BlobID returns the blob object for path at revision.
func (repo *Repo) BlobID(revision, path string) (string, bool, error) {
	if !model.ValidObjectID(revision) {
		return "", false, errors.New("invalid revision object ID")
	}
	path, err := NormalizePath(path)
	if err != nil {
		return "", false, err
	}
	out, err := repo.run("read tree entry", nil, "ls-tree", "-z", revision, "--", path)
	if err != nil {
		return "", false, err
	}
	if len(out) == 0 {
		return "", false, nil
	}
	line := strings.TrimSuffix(string(out), "\x00")
	meta, returnedPath, ok := strings.Cut(line, "\t")
	if !ok || returnedPath != path {
		return "", false, errors.New("git returned invalid tree entry")
	}
	fields := strings.Fields(meta)
	if len(fields) != 3 || fields[1] != "blob" || !model.ValidObjectID(fields[2]) {
		return "", false, errors.New("git returned non-blob tree entry")
	}
	return fields[2], true, nil
}

// ReadBlob reads a bounded blob.
func (repo *Repo) ReadBlob(oid string) ([]byte, error) {
	if !model.ValidObjectID(oid) {
		return nil, errors.New("invalid blob object ID")
	}
	out, err := repo.run("read blob", nil, "cat-file", "blob", oid)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// BlobSize returns the size of one validated blob without reading its content.
func (repo *Repo) BlobSize(oid string) (int64, error) {
	if !model.ValidObjectID(oid) {
		return 0, errors.New("invalid blob object ID")
	}
	out, err := repo.run("read blob size", nil, "cat-file", "-s", oid)
	if err != nil {
		return 0, err
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || size < 0 {
		return 0, errors.New("git returned invalid blob size")
	}
	return size, nil
}

// HashBytes writes content to the Git object database.
func (repo *Repo) HashBytes(content []byte) (string, error) {
	out, err := repo.run("write blob", bytes.NewReader(content), "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	oid := strings.TrimSpace(string(out))
	if !model.ValidObjectID(oid) {
		return "", errors.New("git returned invalid blob object ID")
	}
	return oid, nil
}

// WorktreeFile safely reads a regular, non-ignored file inside the worktree.
func (repo *Repo) WorktreeFile(path string) ([]byte, bool, string, error) {
	path, err := repo.NormalizeWorktreePath(path)
	if err != nil {
		return nil, false, "", err
	}
	ignored, err := repo.Ignored(path)
	if err != nil {
		return nil, false, "", err
	}
	if ignored {
		return nil, false, "", fmt.Errorf("path %q is ignored by Git", path)
	}
	full := filepath.Join(repo.Root, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, path, nil
	}
	if err != nil {
		return nil, false, "", fmt.Errorf("stat path %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, "", fmt.Errorf("path %q is not a regular file", path)
	}
	resolved, err := filepath.EvalSymlinks(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, path, nil
	}
	if err != nil {
		return nil, false, "", fmt.Errorf("resolve path %q: %w", path, err)
	}
	if !inside(repo.Root, resolved) {
		return nil, false, "", fmt.Errorf("path %q escapes the worktree", path)
	}
	if info.Size() > maxFileBytes {
		return nil, false, "", fmt.Errorf("path %q exceeds %d bytes", path, maxFileBytes)
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, false, "", fmt.Errorf("open path %q: %w", path, err)
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, false, "", fmt.Errorf("stat opened path %q: %w", path, err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, false, "", fmt.Errorf("path %q changed while opening", path)
	}
	if openedInfo.Size() > maxFileBytes {
		return nil, false, "", fmt.Errorf("path %q exceeds %d bytes", path, maxFileBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, false, "", fmt.Errorf("read path %q: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return nil, false, "", fmt.Errorf("path %q exceeds %d bytes", path, maxFileBytes)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, false, "", fmt.Errorf("path %q is binary or invalid UTF-8", path)
	}
	lineCount := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		lineCount++
	}
	if lineCount > model.MaxTextLines {
		return nil, false, "", fmt.Errorf("path %q exceeds %d lines", path, model.MaxTextLines)
	}
	return data, true, path, nil
}

// NormalizeWorktreePath accepts an absolute or relative path inside the worktree.
func (repo *Repo) NormalizeWorktreePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("path is empty or contains NUL")
	}
	native := filepath.FromSlash(path)
	if filepath.IsAbs(native) {
		relative, err := filepath.Rel(repo.Root, filepath.Clean(native))
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", errors.New("absolute path escapes the worktree")
		}
		path = filepath.ToSlash(relative)
	}
	return NormalizePath(path)
}

// SnapshotWorktree hashes one allowed worktree path.
func (repo *Repo) SnapshotWorktree(path string) (model.Snapshot, error) {
	content, exists, normalized, err := repo.WorktreeFile(path)
	if err != nil {
		return model.Snapshot{}, err
	}
	if !exists {
		return model.Snapshot{Path: normalized, Exists: false}, nil
	}
	oid, err := repo.HashBytes(content)
	if err != nil {
		return model.Snapshot{}, err
	}
	return model.Snapshot{Path: normalized, Exists: true, Blob: oid}, nil
}

// Ignored reports whether path is ignored by Git.
func (repo *Repo) Ignored(path string) (bool, error) {
	out, err := repo.run("check ignored path", nil, "check-ignore", "-q", "--", path)
	if err == nil {
		return true, nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return false, nil
	}
	_ = out
	return false, err
}

// NormalizePath validates and normalizes a repository-relative slash path.
func NormalizePath(path string) (string, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return "", errors.New("path is empty or contains NUL")
	}
	for _, char := range path {
		if unicode.IsControl(char) {
			return "", errors.New("path contains a control character")
		}
	}
	path = filepath.ToSlash(path)
	if filepath.IsAbs(filepath.FromSlash(path)) || strings.HasPrefix(path, "/") {
		return "", errors.New("absolute path is not allowed")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path escapes the worktree")
	}
	first, _, _ := strings.Cut(clean, "/")
	if strings.EqualFold(first, ".git") {
		return "", errors.New("Git administrative paths are not allowed")
	}
	return clean, nil
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// ReadNote reads a note for commit. Unknown or absent values are reported.
func (repo *Repo) ReadNote(commit string) ([]byte, bool, error) {
	if !model.ValidObjectID(commit) {
		return nil, false, errors.New("invalid commit object ID")
	}
	out, err := repo.run("read attribution note", nil, "notes", "--ref=refs/notes/byline", "show", commit)
	if err == nil {
		return out, true, nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return nil, false, nil
	}
	return nil, false, err
}

// WriteNote creates a note or accepts an existing byte-identical note.
func (repo *Repo) WriteNote(commit string, data []byte) error {
	existing, ok, err := repo.ReadNote(commit)
	if err != nil {
		return err
	}
	if ok {
		if bytes.Equal(existing, data) || bytes.Equal(bytes.TrimSuffix(existing, []byte{'\n'}), bytes.TrimSuffix(data, []byte{'\n'})) {
			return nil
		}
		return errors.New("commit already has a different attribution note")
	}
	path, err := tempFile(repo.GitDir, "byline-note-", data)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	_, err = repo.run("write attribution note", nil, "notes", "--ref=refs/notes/byline", "add", "-F", path, commit)
	if err != nil {
		var commandErr *CommandError
		// The subprocess environment strips GIT_* variables, so the note
		// writer identity can only come from Git configuration.
		if errors.As(err, &commandErr) && isMissingIdentity(commandErr.Stderr) {
			return fmt.Errorf("%w; set user.name and user.email with git config", err)
		}
	}
	return err
}

// isMissingIdentity reports git output that fails for a missing committer
// identity rather than a note or repository problem.
func isMissingIdentity(stderr string) bool {
	lower := strings.ToLower(stderr)
	return strings.Contains(lower, "identity unknown") ||
		strings.Contains(lower, "unable to auto-detect email address")
}

// ProtectBlobs updates the local retention ref.
func (repo *Repo) ProtectBlobs(oids []string) error {
	unique := map[string]bool{}
	for _, oid := range oids {
		if !model.ValidObjectID(oid) {
			return errors.New("invalid retained blob object ID")
		}
		unique[oid] = true
	}
	if len(unique) == 0 {
		_, err := repo.run("delete retention ref", nil, "update-ref", "-d", "refs/worktree/byline/checkpoints")
		if err != nil {
			var commandErr *CommandError
			if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
				return nil
			}
		}
		return err
	}
	oids = oids[:0]
	for oid := range unique {
		oids = append(oids, oid)
	}
	sort.Strings(oids)
	var tree bytes.Buffer
	for i, oid := range oids {
		fmt.Fprintf(&tree, "100644 blob %s\tblob-%08d\n", oid, i)
	}
	out, err := repo.run("write retention tree", &tree, "mktree")
	if err != nil {
		return err
	}
	treeID := strings.TrimSpace(string(out))
	if !model.ValidObjectID(treeID) {
		return errors.New("git returned invalid retention tree ID")
	}
	_, err = repo.run("update retention ref", nil, "update-ref", "refs/worktree/byline/checkpoints", treeID)
	return err
}

// ProtectedBlobCount reports retained snapshots.
func (repo *Repo) ProtectedBlobCount() (int, error) {
	out, err := repo.run("count retained snapshots", nil, "ls-tree", "-r", "--name-only", "refs/worktree/byline/checkpoints")
	if err != nil {
		var commandErr *CommandError
		if errors.As(err, &commandErr) && commandErr.ExitCode == 128 {
			return 0, nil
		}
		return 0, err
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return 0, nil
	}
	return len(bytes.Split(bytes.TrimSpace(out), []byte{'\n'})), nil
}

// FirstParentHistory returns commits newest first.
func (repo *Repo) FirstParentHistory(start string) ([]string, error) {
	if !model.ValidObjectID(start) {
		return nil, errors.New("invalid start commit object ID")
	}
	out, err := repo.run("read first-parent history", nil, "rev-list", "--first-parent", start)
	if err != nil {
		return nil, err
	}
	var commits []string
	for _, line := range strings.Fields(string(out)) {
		if !model.ValidObjectID(line) {
			return nil, errors.New("git returned invalid history object ID")
		}
		commits = append(commits, line)
	}
	return commits, nil
}

// ConfigPath returns a local Git config path value.
func (repo *Repo) ConfigPath(key string) (string, bool, error) {
	out, err := repo.run("read git config", nil, "config", "--local", "--path", "--get", key)
	if err == nil {
		return strings.TrimSpace(string(out)), true, nil
	}
	var commandErr *CommandError
	if errors.As(err, &commandErr) && commandErr.ExitCode == 1 {
		return "", false, nil
	}
	return "", false, err
}

func tempFile(dir, pattern string, data []byte) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temporary file: %w", err)
	}
	name := file.Name()
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(name)
		return "", fmt.Errorf("write temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("close temporary file: %w", err)
	}
	return name, nil
}

func (repo *Repo) text(operation string, stdin io.Reader, args ...string) (string, error) {
	out, err := repo.run(operation, stdin, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GitPath resolves one Git-managed path as an absolute path.
func (repo *Repo) GitPath(name string) (string, error) {
	if name == "" || strings.ContainsRune(name, 0) {
		return "", errors.New("Git path name is empty or contains NUL")
	}
	out, err := repo.run(
		"resolve Git path",
		nil,
		"rev-parse",
		"--path-format=absolute",
		"--git-path",
		name,
	)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" || !filepath.IsAbs(path) {
		return "", errors.New("Git returned a non-absolute managed path")
	}
	return filepath.Clean(path), nil
}

func (repo *Repo) run(operation string, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, repo.gitBin, args...)
	command.Dir = repo.Root
	command.Env = append(gitEnvironment(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_TERMINAL_PROMPT=0",
		"LC_ALL=C",
	)
	command.Stdin = stdin
	var stdout, stderr limitedBuffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("git %s timed out: %w", operation, ctx.Err())
	}
	if stdout.exceeded || stderr.exceeded {
		return nil, fmt.Errorf("git %s: %w of %d bytes", operation, ErrOutputLimit, maxOutputBytes)
	}
	if err != nil {
		exitCode := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
		return nil, &CommandError{
			Operation: operation,
			ExitCode:  exitCode,
			Stderr:    strings.TrimSpace(stderr.String()),
			Err:       err,
		}
	}
	return stdout.Bytes(), nil
}

func gitEnvironment() []string {
	source := os.Environ()
	result := make([]string, 0, len(source))
	for _, value := range source {
		name, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(strings.ToUpper(name), "GIT_") {
			continue
		}
		result = append(result, value)
	}
	return result
}

type limitedBuffer struct {
	data     bytes.Buffer
	exceeded bool
}

func (buffer *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := maxOutputBytes - buffer.data.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		data = data[:remaining]
	}
	_, _ = buffer.data.Write(data)
	return original, nil
}

func (buffer *limitedBuffer) Bytes() []byte {
	return buffer.data.Bytes()
}

func (buffer *limitedBuffer) String() string {
	return buffer.data.String()
}

func splitNUL(data []byte) []string {
	data = bytes.TrimSuffix(data, []byte{0})
	if len(data) == 0 {
		return nil
	}
	raw := bytes.Split(data, []byte{0})
	values := make([]string, len(raw))
	for i, value := range raw {
		values[i] = string(value)
	}
	return values
}

// Version returns the installed Git version components.
func (repo *Repo) Version() (major, minor int, err error) {
	out, err := repo.run("read version", nil, "version")
	if err != nil {
		return 0, 0, err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return 0, 0, errors.New("git returned invalid version")
	}
	parts := strings.Split(fields[2], ".")
	if len(parts) < 2 {
		return 0, 0, errors.New("git returned invalid version")
	}
	major, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, errors.New("git returned invalid major version")
	}
	minor, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, errors.New("git returned invalid minor version")
	}
	return major, minor, nil
}
