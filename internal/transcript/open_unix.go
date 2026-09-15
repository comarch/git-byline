//go:build unix

package transcript

import (
	"fmt"
	"os"
	"syscall"
)

// openVerifiedSessionFile opens the session file after an os.Lstat precheck
// and returns the opened handle with its stat, always a regular file. The
// open does not follow a final symlink and the opened identity must match
// the prechecked one, so swapping the file or its directory entries
// between the precheck and the open is rejected. Ancestor directories may
// be symlinks, because they are legitimate path resolution on many
// systems; only a swap after the precheck changes the identity and fails.
func openVerifiedSessionFile(path string) (*os.File, os.FileInfo, error) {
	pre, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !pre.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("session file %s is not a regular file", path)
	}
	preID, preOK := fileIdentityOf(pre)
	// The nonblocking flag is a no-op on regular files but stops a swapped
	// FIFO from blocking the open, letting the revalidation reject it.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("session file %s is not a regular file", path)
	}
	openedID, openedOK := fileIdentityOf(info)
	if !preOK || !openedOK {
		_ = file.Close()
		return nil, nil, fmt.Errorf("identity of session file %s is unavailable", path)
	}
	if openedID != preID {
		_ = file.Close()
		return nil, nil, fmt.Errorf("session file %s was replaced between checks", path)
	}
	return file, info, nil
}
