//go:build !windows

package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestApplyUpdateReportsRemovedVerifiedArchive(t *testing.T) {
	dir := t.TempDir()
	fixturePath := filepath.Join(dir, "fixture.tar.gz")
	buildTarGZ(t, fixturePath, releaseTarEntries("binary"))
	archiveData, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "release.tar.gz")
	if err := os.Remove(fixturePath); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(archivePath, 0o600); err != nil {
		t.Fatal(err)
	}
	checksumsPath := writeChecksums(t, dir, filepath.Base(archivePath), archiveData)

	removed := make(chan struct{})
	watcherErrors := make(chan error, 1)
	go func() {
		defer close(removed)
		if err := removeArchiveStage(dir); err != nil {
			watcherErrors <- err
			return
		}
		watcherErrors <- nil
	}()
	writerErrors := make(chan error, 1)
	go func() {
		writerErrors <- writeArchiveFIFO(archivePath, archiveData, removed)
	}()

	runErr := applyUpdate(testEnv(&bytes.Buffer{}), updateOptions{
		archivePath:   archivePath,
		checksumsPath: checksumsPath,
		targetPath:    filepath.Join(dir, "target"),
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "open archive") {
		t.Fatalf("applyUpdate error = %v, want removed staging error", runErr)
	}
	if err := <-writerErrors; err != nil {
		t.Fatalf("archive writer: %v", err)
	}
	if err := <-watcherErrors; err != nil {
		t.Fatalf("staging watcher: %v", err)
	}
}

func removeArchiveStage(dir string) error {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".git-byline-archive-") {
				continue
			}
			return os.Remove(filepath.Join(dir, entry.Name()))
		}
		time.Sleep(time.Millisecond)
	}
	return errors.New("archive staging file did not appear")
}

func writeArchiveFIFO(path string, data []byte, removed <-chan struct{}) error {
	file, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	if writeErr == nil {
		<-removed
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
