package provenance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/comarch/git-byline/internal/gitcmd"
	"github.com/comarch/git-byline/internal/lock"
	"github.com/comarch/git-byline/internal/model"
	"github.com/comarch/git-byline/internal/notes"
	"github.com/comarch/git-byline/internal/rewrite"
	"github.com/comarch/git-byline/internal/store"
)

func TestCoverageRewriteBlobCache(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("cached\n"))
	if err != nil {
		t.Fatal(err)
	}
	cache := newRewriteBlobCache()
	cache.content[blob] = []byte("cached\n")
	if got, err := cache.Read(repo, blob); err != nil || string(got) != "cached\n" {
		t.Fatalf("cached Read() = %q, %v", got, err)
	}
	if _, err := cache.Read(repo, "not-a-blob"); err == nil {
		t.Fatal("Read() accepted an invalid blob ID")
	}
	budget := newRewriteBlobCache()
	budget.sizes[blob] = 1
	budget.used = maxRewriteBlobBytes
	if _, err := budget.Read(repo, blob); !errors.Is(err, errRewriteBudget) {
		t.Fatalf("size budget error = %v", err)
	}
	actualSize := newRewriteBlobCache()
	actualSize.sizes[blob] = 1
	actualSize.used = maxRewriteBlobBytes - 1
	if _, err := actualSize.Read(repo, blob); !errors.Is(err, errRewriteBudget) {
		t.Fatalf("content budget error = %v", err)
	}
	missing := newRewriteBlobCache()
	missing.sizes[blob] = 1
	if err := removeLooseObject(repo, blob); err != nil {
		t.Fatal(err)
	}
	if _, err := missing.Read(repo, blob); err == nil {
		t.Fatal("Read() accepted a missing blob object")
	}
}

func TestCoverageRewriteNilAndInputErrors(t *testing.T) {
	t.Parallel()
	if _, err := HandlePostRewrite(nil, strings.NewReader("")); err == nil {
		t.Fatal("HandlePostRewrite accepted nil repository")
	}
	if _, err := applyRewriteMapping(nil, rewrite.Mapping{}); err == nil {
		t.Fatal("applyRewriteMapping accepted nil repository")
	}
	if _, err := HandlePostCheckout(nil, "", ""); err == nil {
		t.Fatal("HandlePostCheckout accepted nil repository")
	}
	if _, err := rebasePending(nil, ""); err == nil {
		t.Fatal("rebasePending accepted nil repository")
	}
	if _, err := HandleReferenceTransaction(nil, strings.NewReader(""), "committed"); err == nil {
		t.Fatal("HandleReferenceTransaction accepted nil repository")
	}
	if _, err := HandleStashApply(nil, strings.Repeat("a", 40), false); err == nil {
		t.Fatal("HandleStashApply accepted nil repository")
	}

	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.Root = filepath.Join(root, "missing")
	if _, err := HandlePostRewrite(repo, strings.NewReader("")); err == nil {
		t.Fatal("HandlePostRewrite accepted a missing repository root")
	}
	repo.Root = root
	if _, err := HandlePostRewrite(repo, strings.NewReader("bad input")); err == nil {
		t.Fatal("HandlePostRewrite accepted malformed hook input")
	}
	repo.Root = filepath.Join(root, "missing")
	if _, err := HandleStashApply(repo, strings.Repeat("a", 40), false); err == nil {
		t.Fatal("HandleStashApply accepted a missing repository root")
	}
}

func TestCoverageRewriteLockAndStateErrors(t *testing.T) {
	t.Parallel()
	t.Run("common lock", coverageRewriteCommonLockFailure)
	t.Run("worktree lock", coverageRewriteWorktreeLockFailure)
	t.Run("state read", coverageRewriteStateReadFailure)
	t.Run("rewrite state retention", coverageRewriteStateRetentionFailure)
}

func coverageRewriteCommonLockFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	repo.CommonDir = filepath.Join(root, "common-file")
	if err := os.WriteFile(repo.CommonDir, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyRewriteMapping(repo, rewrite.Mapping{}); err == nil {
		t.Fatal("applyRewriteMapping acquired a blocked common lock")
	}
}

func coverageRewriteWorktreeLockFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(store.New(repo.GitDir).LockPath(), lockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	if _, err := applyRewriteMapping(repo, rewrite.Mapping{}); err == nil {
		t.Fatal("applyRewriteMapping acquired a blocked worktree lock")
	}
}

func coverageRewriteStateReadFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(repo.GitDir)
	if err := os.MkdirAll(dataStore.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataStore.StatePath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := applyRewriteMapping(repo, rewrite.Mapping{}); err == nil {
		t.Fatal("applyRewriteMapping accepted invalid state")
	}
}

func coverageRewriteStateRetentionFailure(t *testing.T) {
	root := testRepo(t)
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	state := model.NewState()
	refParent := filepath.Join(repo.GitDir, "refs", "worktree", "byline")
	if err := os.MkdirAll(filepath.Dir(refParent), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(refParent, []byte("block"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob, err := repo.HashBytes([]byte("pending\n"))
	if err != nil {
		t.Fatal(err)
	}
	state.Pending.Files[coverageFile] = model.PendingFile{Blob: blob}
	err = writeRewriteState(repo, store.New(repo.GitDir), state)
	if err == nil || !strings.Contains(err.Error(), "protect pending snapshots") {
		t.Fatalf("writeRewriteState() error = %v", err)
	}
}

func TestCoverageRewriteSessionAndPathHelpers(t *testing.T) {
	t.Parallel()
	conflict := map[string]model.NoteSession{
		"session": {Agent: "droid", Model: "one"},
	}
	if err := addRewriteSession(conflict, "session", model.NoteSession{Agent: "claude", Model: "one"}); err == nil {
		t.Fatal("addRewriteSession accepted conflicting agents")
	}
	overflowCases := []struct {
		name  string
		value model.NoteSession
	}{
		{"added", model.NoteSession{Agent: "droid", Model: "model", Added: 1}},
		{"deleted", model.NoteSession{Agent: "droid", Model: "model", Deleted: 1}},
		{"accepted", model.NoteSession{Agent: "droid", Model: "model", Accepted: 1}},
		{"overridden", model.NoteSession{Agent: "droid", Model: "model", Overridden: 1}},
	}
	for _, test := range overflowCases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			sessions := map[string]model.NoteSession{
				"session": {
					Agent: "droid",
					Model: "model",
					Added: maxIntValue(),
				},
			}
			switch test.name {
			case "added":
				sessions["session"] = model.NoteSession{Agent: "droid", Model: "model", Added: maxIntValue()}
			case "deleted":
				sessions["session"] = model.NoteSession{Agent: "droid", Model: "model", Deleted: maxIntValue()}
			case "accepted":
				sessions["session"] = model.NoteSession{Agent: "droid", Model: "model", Accepted: maxIntValue()}
			case "overridden":
				sessions["session"] = model.NoteSession{Agent: "droid", Model: "model", Overridden: maxIntValue()}
			}
			if err := addRewriteSession(sessions, "session", test.value); err == nil {
				t.Fatalf("addRewriteSession(%s) accepted overflow", test.name)
			}
		})
	}
	if _, ok := addRewriteMetric(maxIntValue(), 1); ok {
		t.Fatal("addRewriteMetric accepted overflow")
	}
	if value, ok := addRewriteMetric(2, 3); !ok || value != 5 {
		t.Fatalf("addRewriteMetric() = %d, %t", value, ok)
	}
	if _, err := cherryPickedCommit("(cherry picked from commit invalid)", 40); err == nil {
		t.Fatal("cherryPickedCommit accepted an invalid marker")
	}
	if _, err := cherryPickedCommit("(cherry picked from commit "+strings.Repeat("a", 40), 40); err == nil {
		t.Fatal("cherryPickedCommit accepted an unterminated marker")
	}
	if values := cherryPickMarkers("prefix (cherry picked from commit "+strings.Repeat("a", 40)+")", 40); len(values) != 1 {
		t.Fatalf("cherryPickMarkers() = %v", values)
	}
	if !isZero("") || !isZero(strings.Repeat("0", 40)) || isZero(strings.Repeat("0", 39)+"1") {
		t.Fatal("isZero() cases failed")
	}
}

func TestCoverageRewritePathAndAncestorBranches(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	first := commit(t, root, "first")
	write(t, root, coverageFile, "next\n")
	second := commit(t, root, "second")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := rewrittenPath(repo, second, coverageFile, strings.Repeat("a", 40), []gitcmd.Change{{Status: 'D', OldPath: coverageFile}}); err != nil || found {
		t.Fatalf("rewrittenPath(deleted) = %t, %v", found, err)
	}
	if _, _, err := rewrittenPath(repo, "not-a-commit", coverageFile, "", nil); err == nil {
		t.Fatal("rewrittenPath accepted an invalid commit")
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{Old: first, New: second}})
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := remapThroughAncestors(repo, second, mapping, map[string]bool{second: true}, nil); err != nil || !found || got != second {
		t.Fatalf("remapThroughAncestors() = %q, %t, %v", got, found, err)
	}
	if _, _, err := remapThroughAncestors(repo, "not-a-commit", mapping, nil, nil); err == nil {
		t.Fatal("remapThroughAncestors accepted an invalid boundary")
	}
	if isSymrefShadow(nil, rewrite.RefUpdate{Ref: "HEAD"}, true, "refs/heads/main") {
		t.Fatal("isSymrefShadow accepted a missing branch update")
	}
}

func TestCoverageRebasePendingBranches(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	commitID := commit(t, root, "content")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rebasePending(repo, "not-a-commit"); err == nil {
		t.Fatal("rebasePending accepted an invalid target")
	}
	dataStore := store.New(repo.GitDir)
	state := model.NewState()
	state.Pending.Files[coverageFile] = model.PendingFile{
		Blob:   strings.Repeat("a", 40),
		Ranges: []model.Range{{Start: 1, End: 1, Attribution: model.Attribution{Author: model.AuthorAI}}},
	}
	if _, err := rebasePendingLocked(repo, commitID, dataStore, state); err == nil {
		t.Fatal("rebasePendingLocked accepted a missing pending snapshot")
	}
	if _, err := rebasePendingLocked(repo, commitID, dataStore, model.NewState()); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageReferenceAndCommitAdvanceBranches(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "content\n")
	first := commit(t, root, "first")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := HandleReferenceTransaction(repo, strings.NewReader(""), "preparing"); err != nil ||
		result.Mapped != 0 || result.Written != 0 || len(result.Warnings) != 0 {
		t.Fatalf("preparing transaction = %+v, %v", result, err)
	}
	if _, err := HandleReferenceTransaction(repo, strings.NewReader("bad"), "committed"); err == nil {
		t.Fatal("HandleReferenceTransaction accepted malformed input")
	}
	if advance, err := commitAdvance(repo, rewrite.RefUpdate{}); err != nil || advance {
		t.Fatalf("commitAdvance(empty) = %t, %v", advance, err)
	}
	if advance, err := commitAdvance(repo, rewrite.RefUpdate{Old: first, New: strings.Repeat("a", 40)}); err == nil || advance {
		t.Fatalf("commitAdvance(missing target) = %t, %v", advance, err)
	}
	if advance, err := commitAdvance(repo, rewrite.RefUpdate{Old: first, New: first}); err != nil || advance {
		t.Fatalf("commitAdvance(same commit) = %t, %v", advance, err)
	}
	if result, err := handleHeadMove(repo, rewrite.RefUpdate{Old: first, New: first}); err != nil ||
		result.Mapped != 0 || result.Written != 0 || len(result.Warnings) != 0 {
		t.Fatalf("handleHeadMove(same) = %+v, %v", result, err)
	}
}

func removeLooseObject(repo *gitcmd.Repo, oid string) error {
	return os.Remove(filepath.Join(repo.CommonDir, "objects", oid[:2], oid[2:]))
}

func maxIntValue() int {
	return int(^uint(0) >> 1)
}

func makeCoverageNote(blob string) model.Note {
	return makeCoverageNoteForPath(blob, coverageFile)
}

func makeCoverageNoteForPath(blob, path string) model.Note {
	return model.Note{
		Version: model.NoteVersion,
		Files: map[string]model.NoteFile{
			path: {
				Blob: blob,
				Ranges: []model.Range{{
					Start: 1,
					End:   1,
					Attribution: model.Attribution{
						Author: model.AuthorHuman,
					},
				}},
			},
		},
	}
}

func encodeCoverageNote(t *testing.T, note model.Note) []byte {
	t.Helper()
	data, err := notes.Encode(note)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertCoverageError(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("error = %v, want %q", err, text)
	}
}

func TestCoverageCherryPickValidation(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "source\n")
	source := commit(t, root, "source")
	if err := repo.WriteNote(source, encodeCoverageNote(t, mustNoteBlob(repo, source, coverageFile))); err != nil {
		t.Fatal(err)
	}
	git(t, root, "checkout", "-q", "--detach", base)
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	if valid, warning, err := validCherryPickSource(repo, source, target); err != nil || !valid || warning != "" {
		t.Fatalf("valid source = %t, %q, %v", valid, warning, err)
	}
	if valid, warning, err := validCherryPickSource(repo, source, source); err != nil || valid || warning == "" {
		t.Fatalf("self source = %t, %q, %v", valid, warning, err)
	}
	if valid, warning, err := validCherryPickSource(repo, base, target); err != nil || valid || warning == "" {
		t.Fatalf("missing source note = %t, %q, %v", valid, warning, err)
	}
	if _, err := cherryPickedCommit("(cherry picked from commit "+strings.Repeat("a", 39)+")", 40); err == nil {
		t.Fatal("cherryPickedCommit accepted a short source")
	}
}

func mustNoteBlob(repo *gitcmd.Repo, commitID, path string) model.Note {
	blob, _, _ := repo.BlobID(commitID, path)
	return makeCoverageNote(blob)
}

func TestCoveragePostMergeMarkerWarnings(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	invalid := commitWithMessage(t, root, "(cherry picked from commit invalid)")
	result, err := HandlePostMerge(repo)
	if err != nil || len(result.Warnings) != 1 {
		t.Fatalf("invalid marker = %+v, %v", result, err)
	}
	if !strings.Contains(result.Warnings[0], "no valid") {
		t.Fatalf("invalid marker warning = %v", result.Warnings)
	}
	missing := commitWithMessage(t, root, "(cherry picked from commit "+base+")")
	result, err = HandlePostMerge(repo)
	if err != nil || len(result.Warnings) != 1 ||
		!strings.Contains(result.Warnings[0], "without source note") {
		t.Fatalf("missing source marker = %+v, %v", result, err)
	}
	if invalid == missing {
		t.Fatal("marker warning commits did not advance")
	}
}

func TestCoverageAmendMappingAndRootMergePaths(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	rootCommit := commit(t, root, "root")
	if _, err := HandlePostMerge(repo); err != nil {
		t.Fatal(err)
	}
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "amended\n")
	old := commit(t, root, "old")
	if _, err := Annotate(repo); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "--amend", "-m", "amended")
	result, err := HandlePostMerge(repo)
	if err != nil {
		t.Fatal(err)
	}
	if result.Written == 0 || rootCommit == old {
		t.Fatalf("amend merge handling = %+v", result)
	}
}

func commitWithMessage(t *testing.T, root, message string) string {
	t.Helper()
	git(t, root, "commit", "--allow-empty", "-m", message)
	return strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
}

func TestCoverageValidCherryPickSourceErrors(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "source\n")
	source := commit(t, root, "source")
	blob, err := repo.HashBytes([]byte("not-a-commit\n"))
	if err != nil {
		t.Fatal(err)
	}
	emptyNote := encodeCoverageNote(t, model.Note{Version: model.NoteVersion})
	if err := repo.WriteNote(blob, emptyNote); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(source, emptyNote); err != nil {
		t.Fatal(err)
	}
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	if valid, warning, err := validCherryPickSource(repo, source, target); err != nil || valid || warning == "" {
		t.Fatalf("unrelated patch source = %t, %q, %v", valid, warning, err)
	}
	if valid, warning, err := validCherryPickSource(repo, base, target); err != nil || valid || warning == "" {
		t.Fatalf("missing source = %t, %q, %v", valid, warning, err)
	}
	if _, _, err := validCherryPickSource(repo, source, blob); err == nil {
		t.Fatal("validCherryPickSource accepted a blob target")
	}
}

func TestCoverageApplyRewriteSkippedMappings(t *testing.T) {
	t.Parallel()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	oldBlob := mustBlob(t, repo, old, coverageFile)
	noteData := encodeCoverageNote(t, makeCoverageNote(oldBlob))
	if err := repo.WriteNote(old, noteData); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(target, noteData); err != nil {
		t.Fatal(err)
	}
	mapping, err := rewrite.NewMapping([]rewrite.Pair{
		{Old: strings.Repeat("a", 40), New: target},
		{Old: old, New: target},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := applyRewriteMapping(repo, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("applyRewriteMapping did not warn for an unrelated source")
	}
}

func TestCoverageApplyRewriteInputErrors(t *testing.T) {
	t.Parallel()
	t.Run("read note", coverageApplyReadNoteError)
	t.Run("non commit source", coverageApplyNonCommitSource)
	t.Run("target parent", coverageApplyTargetParentError)
	t.Run("target changes", coverageApplyTargetChangesError)
	t.Run("source tree", coverageApplySourceTreeError)
	t.Run("source blob", coverageApplySourceBlobError)
	t.Run("existing note", coverageApplyExistingNoteError)
	t.Run("write note", coverageApplyWriteNoteError)
}

func coverageApplyReadNoteError(t *testing.T) {
	root, repo, old, target := rewriteCoverageRepo(t)
	bad, err := repo.HashBytes([]byte("not a notes tree\n"))
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-ref", "refs/notes/byline", bad)
	mapping := coverageMapping(t, old, target)
	_, err = applyRewriteMapping(repo, mapping)
	assertCoverageError(t, err, "read old attribution note")
}

func coverageApplyNonCommitSource(t *testing.T) {
	root, repo, old, target := rewriteCoverageRepo(t)
	blob, err := repo.HashBytes([]byte("blob target\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(blob, encodeCoverageNote(t, model.Note{Version: model.NoteVersion})); err != nil {
		t.Fatal(err)
	}
	mapping := coverageMapping(t, blob, target)
	result, err := applyRewriteMapping(repo, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) == 0 || !strings.Contains(result.Warnings[0], "non-commit") {
		t.Fatalf("non-commit mapping = %+v", result)
	}
	_ = root
	_ = old
}

func coverageApplyTargetParentError(t *testing.T) {
	_, repo, old, _ := rewriteCoverageRepo(t)
	blob, err := repo.HashBytes([]byte("blob target\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(mustBlob(t, repo, old, coverageFile)))); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, blob))
	assertCoverageError(t, err, "read rewritten commit parent")
}

func coverageApplyTargetChangesError(t *testing.T) {
	root, repo, old, target := rewriteCoverageRepo(t)
	tree := strings.TrimSpace(git(t, root, "rev-parse", target+"^{tree}"))
	if err := removeLooseObject(repo, tree); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(mustBlob(t, repo, old, coverageFile)))); err != nil {
		t.Fatal(err)
	}
	_, err := applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "read rewritten commit changes")
}

func coverageApplySourceTreeError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	base := commit(t, root, "base")
	write(t, root, coverageFile, "old\n")
	old := commit(t, root, "old")
	git(t, root, "checkout", "-q", "--detach", base)
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	tree := strings.TrimSpace(git(t, root, "rev-parse", old+"^{tree}"))
	if err := removeLooseObject(repo, tree); err != nil {
		t.Fatal(err)
	}
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "verify source blob")
}

func coverageApplySourceBlobError(t *testing.T) {
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	commitWithMessage(t, root, "target")
	target := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	if err := removeLooseObject(repo, blob); err != nil {
		t.Fatal(err)
	}
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "read source blob")
}

func coverageApplyExistingNoteError(t *testing.T) {
	root, repo, old, target := rewriteCoverageRepo(t)
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	bad, err := repo.HashBytes([]byte("bad notes ref\n"))
	if err != nil {
		t.Fatal(err)
	}
	git(t, root, "update-ref", "refs/notes/byline", bad)
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "read old attribution note")
}

func coverageApplyWriteNoteError(t *testing.T) {
	skipIfUnsupportedPermissions(t)
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	commitWithMessage(t, root, "target")
	target := strings.TrimSpace(git(t, root, "rev-parse", "HEAD"))
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	blob := mustBlob(t, repo, old, coverageFile)
	if err := repo.WriteNote(old, encodeCoverageNote(t, makeCoverageNote(blob))); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(repo.CommonDir, "objects"), 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(repo.CommonDir, "objects"), 0o700)
	_, err = applyRewriteMapping(repo, coverageMapping(t, old, target))
	assertCoverageError(t, err, "write rewritten note")
}

func skipIfUnsupportedPermissions(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod-based permission failure is not portable on Windows")
	}
	if runtime.GOOS != "windows" && os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

func rewriteCoverageRepo(t *testing.T) (string, *gitcmd.Repo, string, string) {
	t.Helper()
	root := testRepo(t)
	write(t, root, coverageFile, "base\n")
	old := commit(t, root, "old")
	write(t, root, coverageFile, "target\n")
	target := commit(t, root, "target")
	repo, err := gitcmd.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, repo, old, target
}

func coverageMapping(t *testing.T, old, target string) rewrite.Mapping {
	t.Helper()
	mapping, err := rewrite.NewMapping([]rewrite.Pair{{Old: old, New: target}})
	if err != nil {
		t.Fatal(err)
	}
	return mapping
}

func mustBlob(t *testing.T, repo *gitcmd.Repo, commitID, path string) string {
	t.Helper()
	blob, exists, err := repo.BlobID(commitID, path)
	if err != nil || !exists {
		t.Fatalf("BlobID() = %q, %t, %v", blob, exists, err)
	}
	return blob
}
