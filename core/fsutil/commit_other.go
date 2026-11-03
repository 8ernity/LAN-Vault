//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
)

// atomicMoveNoReplace uses link(2) followed by unlink(2). link(2) fails with
// EEXIST if the destination already exists, which makes the no-replace decision
// atomic even under concurrent writers. Both paths are in the same directory,
// so no cross-device fallback is required.
func atomicMoveNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return os.ErrExist
		}
		return fmt.Errorf("create link %s -> %s failed: %w", dst, src, err)
	}
	if err := os.Remove(src); err != nil {
		// The destination is committed; clean up the source best-effort. Leaving
		// a hard link behind is safe (same inode) but reported for diagnostics.
		return fmt.Errorf("destination committed but source cleanup failed: %w", err)
	}
	return nil
}
