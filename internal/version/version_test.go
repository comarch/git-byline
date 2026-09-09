package version

import "testing"

// TestVersionDefault verifies that builds without linker injection report
// the dev version. The linker injection path itself is covered by the
// release-style build smoke test in cmd/git-byline.
func TestVersionDefault(t *testing.T) {
	t.Parallel()
	if Version != "dev" {
		t.Fatalf("Version without linker injection = %q, want %q", Version, "dev")
	}
}
