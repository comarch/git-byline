//go:build windows

package transcript

import (
	"os"
	"syscall"
)

// fileFlagOpenReparsePoint makes CreateFile open a reparse point itself
// instead of following it. It is a no-op on regular files.
const fileFlagOpenReparsePoint = 0x00200000

// openSessionFile opens a session file without following a final symlink or
// other reparse point, so a path swapped after the precheck cannot redirect
// the read. The opened handle still needs the caller's regular-file
// revalidation, which rejects the reparse point itself.
func openSessionFile(path string) (*os.File, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(
		pointer,
		syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil,
		syscall.OPEN_EXISTING,
		fileFlagOpenReparsePoint,
		0,
	)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), path), nil
}
