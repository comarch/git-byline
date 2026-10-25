package gitcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestObjectBatchReads(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "dir/file.txt", "one\ntwo\n")
	head := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, _, err := repo.BlobID(head, "dir/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	tree := strings.TrimSpace(runGit(t, root, "rev-parse", head+"^{tree}"))
	missing := strings.Repeat("1", len(blob))

	stop := repo.StartBatch()
	if repo.batch == nil || repo.batch.objects != nil {
		t.Fatal("StartBatch started cat-file before the first read")
	}
	if data, err := repo.ReadBlob(blob); err != nil || string(data) != "one\ntwo\n" {
		t.Fatalf("ReadBlob(batch) = %q, %v", data, err)
	}
	objects := repo.batch.objects
	if objects == nil {
		t.Fatal("ReadBlob did not start the batch process")
	}
	repo.StartBatch()()
	if repo.batch == nil || repo.batch.objects != objects {
		t.Fatal("nested StartBatch stopped the running batch")
	}
	tests := []struct {
		name     string
		oid      string
		limit    int
		answered bool
		want     string
		err      error
	}{
		{name: "blob", oid: blob, limit: maxOutputBytes, answered: true, want: "one\ntwo\n"},
		{name: "missing", oid: missing, limit: maxOutputBytes},
		{name: "tree", oid: tree, limit: maxOutputBytes},
		{name: "over limit", oid: blob, limit: 3, answered: true, err: ErrOutputLimit},
		{name: "blob after other answers", oid: blob, limit: maxOutputBytes, answered: true, want: "one\ntwo\n"},
	}
	for _, test := range tests {
		data, answered, err := objects.read("read blob", test.oid, test.limit)
		if answered != test.answered || !errors.Is(err, test.err) || string(data) != test.want {
			t.Fatalf("%s: read() = %q, %t, %v, want %q, %t, %v",
				test.name, data, answered, err, test.want, test.answered, test.err)
		}
	}
	// Objects the batch does not answer keep the single command error.
	if _, err := repo.ReadBlob(missing); err == nil {
		t.Fatal("ReadBlob accepted a missing object")
	}
	if _, err := repo.ReadBlob(tree); err == nil {
		t.Fatal("ReadBlob accepted a tree")
	}
	objects.broken = true
	if data, err := repo.ReadBlob(blob); err != nil || string(data) != "one\ntwo\n" {
		t.Fatalf("ReadBlob(broken batch) = %q, %v", data, err)
	}
	stop()
	if repo.batch != nil {
		t.Fatal("stop left the batch in place")
	}
	if data, err := repo.ReadBlob(blob); err != nil || string(data) != "one\ntwo\n" {
		t.Fatalf("ReadBlob(after stop) = %q, %v", data, err)
	}
}

func TestReadNoteBlob(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "file.txt", "one\n")
	head := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "notes", "--ref=byline", "add", "-m", "note text", head)
	notes, err := repo.ListNotes()
	if err != nil {
		t.Fatal(err)
	}
	read := func() string {
		t.Helper()
		data, err := repo.ReadNoteBlob(notes[head])
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	if got := read(); got != "note text\n" {
		t.Fatalf("ReadNoteBlob() = %q", got)
	}
	defer repo.StartBatch()()
	if got := read(); got != "note text\n" {
		t.Fatalf("ReadNoteBlob(batch) = %q", got)
	}
	if _, err := repo.ReadNoteBlob("not-an-object"); err == nil {
		t.Fatal("ReadNoteBlob accepted an invalid object ID")
	}
}

func TestPrefetchBlobIDs(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "dir/nested.txt", "nested\n")
	writeFile(t, root, "top.txt", "top\n")
	paths := []string{"dir/nested.txt", "top.txt", "missing.txt", "dir"}
	if runtime.GOOS != "windows" {
		writeFile(t, root, "st*r.txt", "star\n")
		paths = append(paths, "st*r.txt")
	}
	head := commitAll(t, root, "first")
	plain, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	// Enough long missing paths to need more than one ls-tree chunk.
	for index := 0; len(strings.Join(paths, " ")) < 2*maxBatchArgBytes; index++ {
		paths = append(paths, fmt.Sprintf("absent/%03d/%s", index, strings.Repeat("x", 100)))
	}
	if err := repo.PrefetchBlobIDs(head, paths); err != nil {
		t.Fatal(err)
	}
	if _, cached := repo.trees[head]["dir"]; cached {
		t.Fatal("PrefetchBlobIDs cached a tree entry")
	}
	for _, path := range paths {
		if path == "dir" {
			continue
		}
		if _, cached := repo.trees[head][path]; !cached {
			t.Fatalf("PrefetchBlobIDs did not cache %q", path)
		}
	}
	for _, path := range paths[:min(len(paths), 8)] {
		wantBlob, wantExists, wantErr := plain.BlobID(head, path)
		gotBlob, gotExists, gotErr := repo.BlobID(head, path)
		if gotBlob != wantBlob || gotExists != wantExists || (gotErr == nil) != (wantErr == nil) {
			t.Fatalf("BlobID(%q) = %q, %t, %v, want %q, %t, %v",
				path, gotBlob, gotExists, gotErr, wantBlob, wantExists, wantErr)
		}
	}
	if err := repo.PrefetchBlobIDs("HEAD", paths); err == nil {
		t.Fatal("PrefetchBlobIDs accepted a symbolic revision")
	}
}

func TestPrefetchIgnored(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, ".gitignore", "*.log\n")
	commitAll(t, root, "ignore logs")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.PrefetchIgnored([]string{"a.log"})
	if repo.batch != nil {
		t.Fatal("PrefetchIgnored opened a batch span")
	}
	stop := repo.StartBatch()
	repo.PrefetchIgnored([]string{"a.log", "b.txt", "a.log", "../outside"})
	if ignored, ok := repo.batch.ignored["a.log"]; !ok || !ignored {
		t.Fatalf("ignored[a.log] = %t, %t", ignored, ok)
	}
	if ignored, ok := repo.batch.ignored["b.txt"]; !ok || ignored {
		t.Fatalf("ignored[b.txt] = %t, %t", ignored, ok)
	}
	if ignored, err := repo.Ignored("a.log"); err != nil || !ignored {
		t.Fatalf("Ignored(a.log) = %t, %v", ignored, err)
	}
	repo.batch.ignored = nil
	repo.PrefetchIgnored([]string{"b.txt", "c.txt"})
	if ignored, ok := repo.batch.ignored["c.txt"]; !ok || ignored {
		t.Fatalf("ignored[c.txt] with no ignored path = %t, %t", ignored, ok)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(filepath.Join(root, "dir"), filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		repo.batch.ignored = nil
		repo.PrefetchIgnored([]string{"a.log", "link/inside.log"})
		if repo.batch.ignored != nil {
			t.Fatalf("PrefetchIgnored kept results of a failed run: %v", repo.batch.ignored)
		}
	}
	stop()
	if ignored, err := repo.Ignored("b.txt"); err != nil || ignored {
		t.Fatalf("Ignored(b.txt) after stop = %t, %v", ignored, err)
	}
	if repo.batch != nil {
		t.Fatal("stop kept the batch span")
	}
}

// fakeBatchGit returns a repo whose Git answers cat-file --batch, ls-tree,
// check-ignore, and hash-object with broken output chosen by mode. Single
// cat-file blob commands print "single", so a fallback is visible.
func fakeBatchGit(t *testing.T, mode string) *Repo {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fake git is not executable on Windows")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
oid=` + strings.Repeat("a", 40) + `
case "$1 $2" in
"cat-file --batch")
  case "$FAKE_BATCH_MODE" in
  eof) exit 0 ;;
  header) read line; printf 'garbage\n' ;;
  size) read line; printf '%s blob -5\n' "$oid" ;;
  short) read line; printf '%s blob 10\nabc' "$oid" ;;
  discard) read line; printf '%s tree 10\nab' "$oid" ;;
  terminator) read line; printf '%s blob 3\nabcX' "$oid" ;;
  esac
  exit 0 ;;
"cat-file blob") printf 'single'; exit 0 ;;
"rev-parse --show-object-format=storage") printf 'sha1\n'; exit 0 ;;
"hash-object -w") printf 'forced hash failure\n' >&2; exit 2 ;;
"check-ignore -z") printf 'unexpected\0'; exit 0 ;;
"check-ignore -q") exit 1 ;;
"--literal-pathspecs ls-tree")
  case "$FAKE_BATCH_MODE" in
  tab) printf 'no tab here\0' ;;
  fields) printf '100644 blob not-an-oid\tfile.txt\0' ;;
  esac
  exit 0 ;;
esac
exit 3
`
	gitBin := filepath.Join(dir, "git")
	if err := os.WriteFile(gitBin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_BATCH_MODE", mode)
	return &Repo{Root: dir, GitDir: dir, CommonDir: dir, gitBin: gitBin}
}

func TestObjectBatchFallsBackOnBrokenStream(t *testing.T) {
	oid := strings.Repeat("a", 40)
	for _, mode := range []string{"eof", "header", "size", "short", "discard", "terminator"} {
		t.Run(mode, func(t *testing.T) {
			repo := fakeBatchGit(t, mode)
			defer repo.StartBatch()()
			for attempt := 0; attempt < 2; attempt++ {
				data, err := repo.ReadBlob(oid)
				if err != nil || string(data) != "single" {
					t.Fatalf("ReadBlob attempt %d = %q, %v, want the single command", attempt, data, err)
				}
			}
			if repo.batch.objects == nil || !repo.batch.objects.broken {
				t.Fatal("broken stream did not disable the batch")
			}
		})
	}
}

func TestObjectBatchStartFailureFallsBack(t *testing.T) {
	repo := fakeBatchGit(t, "eof")
	defer repo.StartBatch()()
	gitBin := repo.gitBin
	repo.gitBin = filepath.Join(t.TempDir(), "missing-git")
	if _, err := repo.ReadBlob(strings.Repeat("a", 40)); err == nil {
		t.Fatal("ReadBlob without a Git binary succeeded")
	}
	if !repo.batch.unusable {
		t.Fatal("failed start did not mark the batch unusable")
	}
	repo.gitBin = gitBin
	if data, err := repo.ReadBlob(strings.Repeat("a", 40)); err != nil || string(data) != "single" {
		t.Fatalf("ReadBlob after failed start = %q, %v", data, err)
	}
}

func TestPrefetchRejectsBrokenGitOutput(t *testing.T) {
	revision := strings.Repeat("b", 40)
	for _, mode := range []string{"tab", "fields"} {
		t.Run(mode, func(t *testing.T) {
			repo := fakeBatchGit(t, mode)
			if err := repo.PrefetchBlobIDs(revision, []string{"file.txt"}); err == nil {
				t.Fatal("PrefetchBlobIDs accepted broken ls-tree output")
			}
		})
	}
	repo := fakeBatchGit(t, "")
	defer repo.StartBatch()()
	repo.PrefetchIgnored([]string{"file.txt"})
	if repo.batch.ignored != nil {
		t.Fatalf("PrefetchIgnored kept an answer for a path it did not ask: %v", repo.batch.ignored)
	}
	repo.PrefetchIgnored([]string{"../outside", "/absolute"})
	if repo.batch.ignored != nil {
		t.Fatal("PrefetchIgnored ran without a valid path")
	}
}

func TestPrefetchBlobIDsSkipsCachedAndInvalidPaths(t *testing.T) {
	t.Parallel()
	root := initRepository(t)
	writeFile(t, root, "dir/nested.txt", "nested\n")
	head := commitAll(t, root, "first")
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.PrefetchBlobIDs(head, []string{"dir/nested.txt", "../outside"}); err != nil {
		t.Fatal(err)
	}
	if _, cached := repo.trees[head]["dir"]; cached {
		t.Fatal("PrefetchBlobIDs cached a parent tree it was not asked for")
	}
	// Everything is cached now, so a second prefetch needs no ls-tree.
	repo.gitBin = filepath.Join(t.TempDir(), "missing-git")
	if err := repo.PrefetchBlobIDs(head, []string{"dir/nested.txt", "dir/nested.txt"}); err != nil {
		t.Fatalf("PrefetchBlobIDs(cached) = %v", err)
	}
	if err := repo.PrefetchBlobIDs(head, []string{"new.txt"}); err == nil {
		t.Fatal("PrefetchBlobIDs hid an ls-tree failure")
	}
}
