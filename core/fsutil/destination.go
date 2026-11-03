package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrPathEscapesRoot = errors.New("planned path escapes destination root")
	ErrSymlinkInPath   = errors.New("planned path traverses a symlink or reparse point")
	ErrDestinationBusy = errors.New("could not find a free destination name")
)

// IsWithin reports whether target is lexically inside root. Both paths must be
// absolute and cleaned. It is a defence-in-depth check, not a replacement for
// filesystem-level no-follow semantics.
func IsWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

// SafeJoin validates a wire-relative path and returns a confined absolute
// destination path. It rejects traversal, absolute paths, drive designators and
// any existing component that is a symlink/reparse point. The destination root
// itself must exist and be a directory.
func SafeJoin(root, rel string) (string, error) {
	if why := ValidatePath(rel); why != "" {
		return "", fmt.Errorf("%w: %s", ErrPathEscapesRoot, why)
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs = filepath.Clean(rootAbs)

	relOS := filepath.FromSlash(rel)
	if filepath.IsAbs(relOS) || filepath.VolumeName(relOS) != "" {
		return "", ErrPathEscapesRoot
	}
	cleanRel := filepath.Clean(relOS)
	if cleanRel == "." || cleanRel == ".." ||
		strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return "", ErrPathEscapesRoot
	}

	target := filepath.Join(rootAbs, cleanRel)
	if !IsWithin(rootAbs, target) {
		return "", ErrPathEscapesRoot
	}

	// Walk every existing component (including the root) without following
	// links. This blocks symlinks and Windows junctions/reparse points that
	// would redirect writes outside the chosen destination.
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return "", fmt.Errorf("destination root unavailable: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("destination root is not a directory: %s", rootAbs)
	}

	component := rootAbs
	for _, seg := range strings.Split(cleanRel, string(filepath.Separator)) {
		component = filepath.Join(component, seg)
		info, statErr := os.Lstat(component)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s", ErrSymlinkInPath, component)
		}
		// If a non-final component is a regular file, writes below it cannot be
		// performed safely. Let the caller report a path error instead of
		// silently overwriting it.
		if !info.IsDir() && component != target {
			return "", fmt.Errorf("%w: component is not a directory: %s", ErrPathEscapesRoot, component)
		}
	}

	// Resolve the deepest existing ancestor and re-check confinement. This
	// catches reparse points that Go reports as directories.
	existing := filepath.Dir(target)
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break
		}
		existing = parent
	}
	if resolved, evalErr := filepath.EvalSymlinks(existing); evalErr == nil {
		if resolvedRoot, rootErr := filepath.EvalSymlinks(rootAbs); rootErr == nil {
			if !IsWithin(resolvedRoot, resolved) && resolved != resolvedRoot {
				return "", fmt.Errorf("%w: %s resolves outside %s", ErrPathEscapesRoot, existing, rootAbs)
			}
		}
	}

	return target, nil
}

// AvailableDiskName returns a candidate that does not currently exist. It is
// advisory: callers must still commit with CommitNoReplace, which is atomic.
func AvailableDiskName(fullPath string) string {
	for i := 0; i < 10000; i++ {
		candidate := fullPath
		if i > 0 {
			candidate = numberedFullPath(fullPath, i)
		}
		if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
	return fullPath
}

func numberedFullPath(fullPath string, n int) string {
	dir := filepath.Dir(fullPath)
	base := filepath.Base(fullPath)
	return filepath.Join(dir, numberedName(base, n))
}

// CommitNoReplace atomically moves src to dst without ever replacing an
// existing destination. If dst exists, the next free numbered name is used.
// It returns the final path actually committed.
func CommitNoReplace(src, dst string) (string, error) {
	for i := 0; i < 10000; i++ {
		candidate := dst
		if i > 0 {
			candidate = numberedFullPath(dst, i)
		}
		err := atomicMoveNoReplace(src, candidate)
		if err == nil {
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if _, statErr := os.Lstat(candidate); statErr == nil {
			continue
		}
		return "", err
	}
	return "", ErrDestinationBusy
}
