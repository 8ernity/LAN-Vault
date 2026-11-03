//go:build !windows

package fsutil

import "syscall"

// FreeBytes returns the available bytes for the filesystem containing path.
func FreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// MaxFileSize returns the maximum single-file size supported by the filesystem
// containing path. LANVault's POSIX targets do not impose an application-level
// limit.
func MaxFileSize(path string) int64 {
	return int64(^uint64(0) >> 1)
}
