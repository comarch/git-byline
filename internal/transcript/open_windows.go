//go:build windows

package transcript

import (
	"fmt"
	"os"
	"syscall"
)

// fileFlagOpenReparsePoint makes CreateFile open a reparse point itself
// instead of following it. It is a no-op on regular files.
const fileFlagOpenReparsePoint = 0x00200000

// openSessionHandle opens a path without following a final symlink or other
// reparse point.
func openSessionHandle(path string) (syscall.Handle, error) {
	pointer, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
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
		return 0, err
	}
	return handle, nil
}

// openVerifiedSessionFile opens the session file without following a final
// symlink or other reparse point and returns the opened handle with its
// stat, always a regular file. The regular check and the read share one
// handle, so no path resolution can slip between the check and the read.
func openVerifiedSessionFile(path string) (*os.File, os.FileInfo, error) {
	handle, err := openSessionHandle(path)
	if err != nil {
		return nil, nil, err
	}
	file := os.NewFile(uintptr(handle), path)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, fmt.Errorf("session file %s is not a regular file", path)
	}
	return file, info, nil
}
