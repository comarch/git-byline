package gitcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseWorktreeList(t *testing.T) {
	t.Parallel()
	mainRoot := testAbsPath("/repo")
	nested := testAbsPath("/repo/.worktrees/wt")
	sibling := testAbsPath("/sibling")
	tests := []struct {
		name    string
		output  string
		want    []string
		wantErr bool
	}{
		{
			name: "main and linked worktrees",
			output: "worktree " + mainRoot + "\nHEAD 0123\nbranch refs/heads/main\n\n" +
				"worktree " + nested + "\nHEAD 0123\ndetached\nlocked \"on usb\\ndisk\"\n" +
				"prunable gitdir file points to non-existent location\n\n",
			want: []string{filepath.FromSlash(mainRoot), filepath.FromSlash(nested)},
		},
		{
			name:   "bare main worktree",
			output: "worktree " + mainRoot + "\nbare\n\nworktree " + sibling + "\nHEAD 0123\nbranch refs/heads/side\n\n",
			want:   []string{filepath.FromSlash(sibling)},
		},
		{
			name:   "records without blank lines",
			output: "worktree " + mainRoot + "\nworktree " + sibling,
			want:   []string{filepath.FromSlash(mainRoot), filepath.FromSlash(sibling)},
		},
		{name: "empty output"},
		{name: "relative path", output: "worktree repo\n", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			roots, err := parseWorktreeList([]byte(test.output))
			if (err != nil) != test.wantErr || !reflect.DeepEqual(roots, test.want) {
				t.Fatalf("parseWorktreeList() = %q, %v; want %q, error %t", roots, err, test.want, test.wantErr)
			}
		})
	}
}

func TestRouteWorktreePaths(t *testing.T) {
	t.Parallel()
	fixture := newWorktreeFixture(t)
	other := initRepository(t)
	paths := []string{
		"a.txt",
		filepath.Join(fixture.root, "a.txt"),
		".worktrees/wt/a.txt",
		filepath.Join(fixture.nested, "new.txt"),
		filepath.Join(fixture.sibling, "a.txt"),
		filepath.Join(other, "a.txt"),
		"../escape",
		"",
	}
	routes, warnings, err := fixture.repo.RouteWorktreePaths(paths)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("RouteWorktreePaths() warnings = %q, error = %v", warnings, err)
	}
	if len(routes) != 3 || routes[0].Repo != fixture.repo {
		t.Fatalf("RouteWorktreePaths() = %+v, want the current worktree and two others", routes)
	}
	wantLocal := []string{"a.txt", filepath.Join(fixture.root, "a.txt"), filepath.Join(other, "a.txt"), "../escape", ""}
	if !reflect.DeepEqual(routes[0].Paths, wantLocal) {
		t.Fatalf("current worktree paths = %q, want %q", routes[0].Paths, wantLocal)
	}
	if routes[1].Repo.Root > routes[2].Repo.Root {
		t.Fatalf("other worktrees are not in root order: %q, %q", routes[1].Repo.Root, routes[2].Repo.Root)
	}
	got := map[string][]string{}
	for _, route := range routes[1:] {
		if route.Repo.CommonDir != fixture.repo.CommonDir || route.Repo.GitDir == fixture.repo.GitDir {
			t.Fatalf("route repository = %+v, want another worktree of %s", route.Repo, fixture.repo.CommonDir)
		}
		got[route.Repo.Root] = route.Paths
	}
	want := map[string][]string{
		fixture.nested:  {"a.txt", filepath.Join(fixture.nested, "new.txt")},
		fixture.sibling: {filepath.Join(fixture.sibling, "a.txt")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("other worktree paths = %q, want %q", got, want)
	}

	nestedRepo, err := Discover(fixture.nested)
	if err != nil {
		t.Fatal(err)
	}
	routes, warnings, err = nestedRepo.RouteWorktreePaths([]string{"a.txt", filepath.Join(fixture.root, "a.txt")})
	if err != nil || len(warnings) != 0 || len(routes) != 2 ||
		routes[0].Repo != nestedRepo || !reflect.DeepEqual(routes[0].Paths, []string{"a.txt"}) ||
		routes[1].Repo.Root != fixture.root ||
		!reflect.DeepEqual(routes[1].Paths, []string{filepath.Join(fixture.root, "a.txt")}) {
		t.Fatalf("RouteWorktreePaths(from nested) = %+v, %q, %v", routes, warnings, err)
	}
}

func TestRouteWorktreePathsSkipsUnverifiedWorktrees(t *testing.T) {
	t.Parallel()
	fixture := newWorktreeFixture(t)
	other := initRepository(t)
	emptied := filepath.Join(resolvedTempDir(t), "emptied")
	runGit(t, fixture.root, "worktree", "add", "-q", "-b", "emptied", emptied)
	// Without its .git file, Git resolves the nested directory to the main worktree.
	removeTestFile(t, filepath.Join(fixture.nested, ".git"))
	writeFile(t, fixture.sibling, ".git", "gitdir: "+filepath.ToSlash(filepath.Join(other, ".git"))+"\n")
	removeTestFile(t, filepath.Join(emptied, ".git"))
	nestedPath := filepath.Join(fixture.nested, "a.txt")
	siblingPath := filepath.Join(fixture.sibling, "a.txt")
	emptiedPath := filepath.Join(emptied, "a.txt")

	routes, warnings, err := fixture.repo.RouteWorktreePaths([]string{nestedPath, siblingPath, emptiedPath, "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Repo != fixture.repo || !reflect.DeepEqual(routes[0].Paths, []string{"a.txt"}) {
		t.Fatalf("RouteWorktreePaths() = %+v, want only the current worktree", routes)
	}
	if len(warnings) != 3 {
		t.Fatalf("warnings = %q, want one per unverified path", warnings)
	}
	for path, reason := range map[string]string{
		nestedPath:  "does not belong to this repository",
		siblingPath: "does not belong to this repository",
		emptiedPath: "",
	} {
		if !containsWarning(warnings, fmt.Sprintf("skipped %q: ", path), reason) {
			t.Fatalf("warnings = %q, want a skip for %q with %q", warnings, path, reason)
		}
	}
}

func TestRouteWorktreePathsPrefersCurrentWorktree(t *testing.T) {
	t.Parallel()
	// With core.worktree, git worktree list names the directory of the Git
	// dir, not the checkout.
	parent := initRepository(t)
	checkout := filepath.Join(parent, "checkout")
	if err := os.Mkdir(checkout, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, parent, "config", "core.worktree", filepath.ToSlash(checkout))
	repo, err := Discover(checkout)
	if err != nil || repo.Root != checkout {
		t.Fatalf("Discover(checkout) = %+v, %v", repo, err)
	}
	path := filepath.Join(checkout, "a.txt")
	routes, warnings, err := repo.RouteWorktreePaths([]string{path})
	if err != nil || len(warnings) != 0 || len(routes) != 1 || routes[0].Repo != repo ||
		!reflect.DeepEqual(routes[0].Paths, []string{path}) {
		t.Fatalf("RouteWorktreePaths(core.worktree) = %+v, %q, %v", routes, warnings, err)
	}
}

func TestRouteWorktreePathsKeepsUnresolvablePathLocal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink loop creation is not portable on Windows")
	}
	t.Parallel()
	root := initRepository(t)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	loop := filepath.Join(root, "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	routes, warnings, err := repo.RouteWorktreePaths([]string{loop})
	if err != nil || len(warnings) != 0 || len(routes) != 1 || routes[0].Repo != repo ||
		!reflect.DeepEqual(routes[0].Paths, []string{loop}) {
		t.Fatalf("RouteWorktreePaths(loop) = %+v, %q, %v", routes, warnings, err)
	}
}

func TestRouteWorktreePathsKeepsSymlinkComponents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs extra privileges on Windows")
	}
	t.Parallel()
	fixture := newWorktreeFixture(t)
	link := filepath.Join(fixture.nested, "link.txt")
	if err := os.Symlink("a.txt", link); err != nil {
		t.Fatal(err)
	}
	throughLink := filepath.Join(fixture.root, "to-sibling", "a.txt")
	if err := os.Symlink(fixture.sibling, filepath.Join(fixture.root, "to-sibling")); err != nil {
		t.Fatal(err)
	}
	routes, warnings, err := fixture.repo.RouteWorktreePaths(
		[]string{".worktrees/wt/link.txt", link, "to-sibling/a.txt", throughLink})
	if err != nil || len(warnings) != 0 {
		t.Fatalf("RouteWorktreePaths() warnings = %q, error = %v", warnings, err)
	}
	got := map[string][]string{}
	for _, route := range routes {
		got[route.Repo.Root] = route.Paths
	}
	// A relative path moves relative to the owner root, or stays here when
	// only a symlink leads to the owner. An absolute path moves as it is.
	want := map[string][]string{
		fixture.root:    {"to-sibling/a.txt"},
		fixture.nested:  {"link.txt", link},
		fixture.sibling: {throughLink},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("RouteWorktreePaths() paths = %q, want %q", got, want)
	}
	nestedRepo, err := Discover(fixture.nested)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		repo *Repo
		path string
		want string
	}{
		{nestedRepo, "link.txt", "not a regular file"},
		{fixture.repo, "to-sibling/a.txt", "beyond a symbolic link"},
	} {
		if _, _, _, err := check.repo.WorktreeFile(check.path); err == nil || !strings.Contains(err.Error(), check.want) {
			t.Fatalf("WorktreeFile(%q) = %v, want an error with %q", check.path, err, check.want)
		}
	}
}

func TestRouteWorktreePathsFailures(t *testing.T) {
	t.Parallel()
	failing := coverageOutputRepo(t, "", "fatal: worktree list failed", 128)
	if routes, warnings, err := failing.RouteWorktreePaths(nil); err != nil || routes != nil || warnings != nil {
		t.Fatalf("RouteWorktreePaths(nil) = %+v, %q, %v", routes, warnings, err)
	}
	if _, _, err := failing.RouteWorktreePaths([]string{"a.txt"}); err == nil ||
		!strings.Contains(err.Error(), "list worktrees") {
		t.Fatalf("RouteWorktreePaths(list failure) = %v", err)
	}
	relative := coverageOutputRepo(t, "worktree relative\n", "", 0)
	if _, _, err := relative.RouteWorktreePaths([]string{"a.txt"}); err == nil ||
		!strings.Contains(err.Error(), "non-absolute worktree path") {
		t.Fatalf("RouteWorktreePaths(relative root) = %v", err)
	}
	missing := &Repo{Root: filepath.Join(t.TempDir(), "missing"), gitBin: failing.gitBin}
	if _, _, err := missing.RouteWorktreePaths([]string{"a.txt"}); err == nil ||
		!strings.Contains(err.Error(), "resolve worktree root") {
		t.Fatalf("RouteWorktreePaths(missing root) = %v", err)
	}
}

func TestWorktreeRootsSkipsMissingDirectories(t *testing.T) {
	t.Parallel()
	repo := coverageRepo(t, "printf 'worktree %s\\n\\nworktree %s\\n' /nonexistent/byline-worktree \"$(pwd -P)\"\n")
	roots, err := repo.worktreeRoots()
	if err != nil || !reflect.DeepEqual(roots, []string{repo.Root}) {
		t.Fatalf("worktreeRoots() = %q, %v; want %q", roots, err, repo.Root)
	}
}

// worktreeFixture is a main worktree with one nested and one sibling linked
// worktree of the same repository.
type worktreeFixture struct {
	root    string
	nested  string
	sibling string
	repo    *Repo
}

func newWorktreeFixture(t *testing.T) worktreeFixture {
	t.Helper()
	root := initRepository(t)
	writeFile(t, root, ".gitignore", ".worktrees/\n")
	writeFile(t, root, "a.txt", "a\n")
	commitAll(t, root, "base")
	nested := filepath.Join(root, ".worktrees", "wt")
	runGit(t, root, "worktree", "add", "-q", "-b", "nested", nested)
	sibling := filepath.Join(resolvedTempDir(t), "sibling")
	runGit(t, root, "worktree", "add", "-q", "-b", "sibling", sibling)
	repo, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	return worktreeFixture{root: root, nested: nested, sibling: sibling, repo: repo}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func removeTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func containsWarning(warnings []string, prefix, reason string) bool {
	for _, warning := range warnings {
		if strings.HasPrefix(warning, prefix) && strings.Contains(warning, reason) {
			return true
		}
	}
	return false
}

// testAbsPath turns a slash path into an absolute path on this platform.
func testAbsPath(slash string) string {
	if runtime.GOOS == "windows" {
		return "C:" + slash
	}
	return slash
}
