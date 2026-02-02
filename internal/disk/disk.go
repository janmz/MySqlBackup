// Package disk provides cross-platform free disk space checks.
package disk

import (
	"errors"
	"os"
	"path/filepath"
)

// ErrInsufficientSpace can be used by callers when a write fails with ENOSPC.
var ErrInsufficientSpace = errors.New("insufficient disk space")

// MinFreeBytes is a reasonable minimum (e.g. 100 MB) to require before starting backup.
const MinFreeBytes = 100 * 1024 * 1024

// Available returns the number of bytes available for writing in the given path's volume.
// Uses syscall.Statfs on Unix and GetDiskFreeSpaceEx on Windows.
// available() is defined in disk_unix.go (build !windows) and disk_windows.go (build windows).
func Available(path string) (uint64, error) {
	path = filepath.FromSlash(path)
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	// Ensure we have a directory (volume root works too)
	if info, err := os.Stat(abs); err == nil && !info.IsDir() {
		abs = filepath.Dir(abs)
	}
	return available(abs)
}
