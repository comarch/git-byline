package gitcmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WorktreePaths is a group of hook paths owned by one worktree.
type WorktreePaths struct {
	Repo  *Repo
	Paths []string
}

// worktreeRoots returns the resolved root of every worktree of the
// repository that exists on disk. Bare entries and worktrees whose directory
// is gone are left out.
func (repo *Repo) worktreeRoots() ([]string, error) {
	out, err := repo.run("list worktrees", nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	listed, err := parseWorktreeList(out)
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(listed))
	for _, root := range listed {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		roots = append(roots, resolved)
	}
	return roots, nil
}

// parseWorktreeList reads worktree paths from git worktree list --porcelain.
// Git 2.31 has no -z for this command, so a path with a newline can parse
// wrong; callers must verify each root before they trust it.
func parseWorktreeList(out []byte) ([]string, error) {
	var roots []string
	path, bare := "", false
	flush := func() {
		if path != "" && !bare {
			roots = append(roots, path)
		}
		path, bare = "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		value, isPath := strings.CutPrefix(line, "worktree ")
		switch {
		case isPath:
			flush()
			native := filepath.FromSlash(value)
			if !filepath.IsAbs(native) {
				return nil, errors.New("git returned a non-absolute worktree path")
			}
			path = filepath.Clean(native)
		case line == "bare":
			bare = true
		case line == "":
			flush()
		}
	}
	flush()
	return roots, nil
}

// RouteWorktreePaths groups hook paths by the worktree of this repository
// that owns them, so a hook that runs in one worktree can record an edit in
// another. A path inside another worktree, including a linked worktree
// nested in this one, moves there as a resolved absolute path. Every other
// path stays with this worktree unchanged, where the usual checks accept or
// reject it. A path whose worktree fails verification is skipped with a
// warning. This worktree comes first, the others follow in root order.
func (repo *Repo) RouteWorktreePaths(paths []string) ([]WorktreePaths, []string, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	current, err := filepath.EvalSymlinks(repo.Root)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve worktree root: %w", err)
	}
	roots, err := repo.worktreeRoots()
	if err != nil {
		return nil, nil, err
	}
	// Git lists the main worktree next to the common dir even when
	// core.worktree puts the checkout elsewhere. The current root is
	// always a candidate, and a linked worktree nested in it still wins.
	roots = append(roots, current)
	local := WorktreePaths{Repo: repo}
	moved := map[string][]string{}
	for _, path := range paths {
		owner, resolved := pathOwner(path, current, roots)
		if owner == "" || owner == current {
			local.Paths = append(local.Paths, path)
			continue
		}
		moved[owner] = append(moved[owner], resolved)
	}
	var routes []WorktreePaths
	if len(local.Paths) > 0 {
		routes = append(routes, local)
	}
	owners := make([]string, 0, len(moved))
	for owner := range moved {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	var warnings []string
	for _, owner := range owners {
		target, err := repo.verifiedWorktree(owner)
		if err != nil {
			for _, path := range moved[owner] {
				warnings = append(warnings, fmt.Sprintf("skipped %q: %v", path, err))
			}
			continue
		}
		routes = append(routes, WorktreePaths{Repo: target, Paths: moved[owner]})
	}
	return routes, warnings, nil
}

// pathOwner returns the innermost worktree root that contains path, with
// the resolved absolute path. A relative path is read against current. The
// root is empty when the path is invalid or no worktree contains it.
func pathOwner(path, current string, roots []string) (string, string) {
	native := filepath.FromSlash(path)
	if !filepath.IsAbs(native) {
		clean, err := NormalizePath(path)
		if err != nil {
			return "", ""
		}
		native = filepath.Join(current, filepath.FromSlash(clean))
	}
	resolved, err := resolvePathAliases(filepath.Clean(native))
	if err != nil {
		return "", ""
	}
	owner := ""
	for _, root := range roots {
		if len(root) > len(owner) && inside(root, resolved) {
			owner = root
		}
	}
	return owner, resolved
}

// verifiedWorktree opens the worktree at root and checks that Git reports
// the same root and the same repository, so a replaced, emptied, or stale
// directory never receives checkpoints.
func (repo *Repo) verifiedWorktree(root string) (*Repo, error) {
	target, err := Discover(root)
	if err != nil {
		return nil, fmt.Errorf("open worktree %q: %w", root, err)
	}
	if !sameDirectory(target.Root, root) || !sameDirectory(target.CommonDir, repo.CommonDir) {
		return nil, fmt.Errorf("worktree %q does not belong to this repository", root)
	}
	return target, nil
}

func sameDirectory(first, second string) bool {
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	return firstErr == nil && secondErr == nil && os.SameFile(firstInfo, secondInfo)
}
