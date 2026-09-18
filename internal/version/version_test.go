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

// TestCompareReleaseTags verifies the tag ordering used by the update
// command's automatic selection and the version check.
func TestCompareReleaseTags(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.0", "v1.2.0", 0},
		{"v1.2.0", "v1.2.1", -1},
		{"v1.3.0", "v1.2.9", 1},
		{"v2.0.0", "v10.0.0", -1},
		{"v1.0.0", "v1.0.0", 0},
	}
	for _, c := range cases {
		if got := CompareReleaseTags(c.a, c.b); got != c.want {
			t.Fatalf("CompareReleaseTags(%s, %s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
