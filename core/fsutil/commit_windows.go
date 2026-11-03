//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const moveFileWriteThrough = 0x00000008

var (
	kernel32Proc       = syscall.NewLazyDLL("kernel32.dll")
	procMoveFileExW    = kernel32Proc.NewProc("MoveFileExW")
)

func moveFileNoReplace(src, dst string) error {
	srcPtr, err := syscall.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	dstPtr, err := syscall.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	// Deliberately omit MOVEFILE_REPLACE_EXISTING: Win32 fails atomically when
	// the destination appeared after the advisory existence check.
	r1, _, callErr := procMoveFileExW.Call(
		uintptr(unsafe.Pointer(srcPtr)),
		uintptr(unsafe.Pointer(dstPtr)),
		uintptr(moveFileWriteThrough),
	)
	if r1 != 0 {
		return nil
	}
	if callErr == syscall.Errno(0) {
		return fmt.Errorf("MoveFileExW failed for %s", src)
	}
	errno, ok := callErr.(syscall.Errno)
	if ok && (errno == 80 || errno == 183) { // ERROR_FILE_EXISTS / ERROR_ALREADY_EXISTS
		return os.ErrExist
	}
	return fmt.Errorf("atomic move %s -> %s failed: %w", src, dst, callErr)
}

func atomicMoveNoReplace(src, dst string) error {
	err := moveFileNoReplace(src, dst)
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrExist) {
		return err
	}
	return err
}
