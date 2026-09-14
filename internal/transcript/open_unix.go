//go:build unix

package transcript

import (
	"os"
	"syscall"
)

// openSessionFile opens a session file without following a final symlink,
// so a swapped symlink cannot redirect the read after the precheck. The
// nonblocking flag is a no-op on regular files but stops a swapped FIFO
// from blocking the open, letting descriptor revalidation reject it.
func openSessionFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
