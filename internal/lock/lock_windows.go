//go:build windows

package lock

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

const (
	lockfileFailImmediately = 0x00000001
	lockfileExclusiveLock   = 0x00000002
	errorLockViolation      = syscall.Errno(33)
)

var (
	kernel32Proc     = syscall.NewLazyDLL("kernel32.dll")
	lockFileExProc   = kernel32Proc.NewProc("LockFileEx")
	unlockFileExProc = kernel32Proc.NewProc("UnlockFileEx")
)

func tryPlatformLock(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := lockFileExProc.Call(
		file.Fd(),
		lockfileExclusiveLock|lockfileFailImmediately,
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func unlockPlatform(file *os.File) error {
	var overlapped syscall.Overlapped
	result, _, callErr := unlockFileExProc.Call(
		file.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&overlapped)),
	)
	if result == 0 {
		return callErr
	}
	return nil
}

func platformLockBusy(err error) bool {
	return errors.Is(err, errorLockViolation)
}
