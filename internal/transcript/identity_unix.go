//go:build unix

package transcript

import (
	"os"
	"syscall"
)

// fileID identifies one file for the open swap check.
type fileID struct {
	device uint64
	index  uint64
}

// fileIdentityOf extracts the device and inode identity of one stat result.
func fileIdentityOf(info os.FileInfo) (fileID, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}, false
	}
	return fileID{device: uint64(stat.Dev), index: uint64(stat.Ino)}, true
}
