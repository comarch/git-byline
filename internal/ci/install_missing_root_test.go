package ci

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallRejectsMissingRoot covers the install root stat failure.
func TestInstallRejectsMissingRoot(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "missing-root")
	if _, err := Install(missing, ProviderGitHub); err == nil ||
		!strings.Contains(err.Error(), "stat CI install root") {
		t.Fatalf("Install(missing root) = %v, want stat CI install root error", err)
	}
}
