//go:build windows

package transcript

import "os"

// openSessionFile opens a session file. Windows has no O_NOFOLLOW, so the
// Lstat precheck and the opened-descriptor revalidation carry the symlink
// defense for these reads.
func openSessionFile(path string) (*os.File, error) {
	return os.Open(path)
}
