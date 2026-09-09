// Package lock provides a portable repository operation lock.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// File is an acquired lock file.
type File struct {
	path string
	file *os.File
}

// Acquire waits until path can be locked exclusively.
func Acquire(path string, timeout time.Duration) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		err := tryPlatformLock(file)
		if err == nil {
			if truncateErr := file.Truncate(0); truncateErr != nil {
				unlockPlatform(file)
				file.Close()
				return nil, fmt.Errorf("truncate lock metadata: %w", truncateErr)
			}
			if _, seekErr := file.Seek(0, 0); seekErr != nil {
				unlockPlatform(file)
				file.Close()
				return nil, fmt.Errorf("seek lock metadata: %w", seekErr)
			}
			if _, writeErr := fmt.Fprintf(file, "%d\n", os.Getpid()); writeErr != nil {
				unlockPlatform(file)
				file.Close()
				return nil, fmt.Errorf("write lock metadata: %w", writeErr)
			}
			return &File{path: path, file: file}, nil
		}
		if !platformLockBusy(err) {
			file.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			file.Close()
			return nil, fmt.Errorf("lock %s is busy after %s", path, timeout)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Release unlocks and closes the held file.
func (file *File) Release() error {
	if file == nil || file.file == nil {
		return nil
	}
	unlockErr := unlockPlatform(file.file)
	closeErr := file.file.Close()
	file.file = nil
	if unlockErr != nil {
		return fmt.Errorf("unlock %s: %w", file.path, unlockErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close lock: %w", closeErr)
	}
	return nil
}

// String returns diagnostic lock ownership without exposing other state.
func (file *File) String() string {
	if file == nil || file.file == nil {
		return "released"
	}
	return "pid " + strconv.Itoa(os.Getpid())
}
