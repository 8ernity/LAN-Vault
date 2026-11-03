//go:build windows

package fsutil

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	procGetDiskFreeSpaceExW   = kernel32Proc.NewProc("GetDiskFreeSpaceExW")
	procGetVolumeInformationW = kernel32Proc.NewProc("GetVolumeInformationW")
)

// FreeBytes returns the available bytes for the volume containing path.
func FreeBytes(path string) (int64, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeForCaller, total, totalFree uint64
	r1, _, callErr := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeForCaller)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("GetDiskFreeSpaceExW(%s) failed: %w", path, callErr)
	}
	if freeForCaller > uint64(^uint64(0)>>1) {
		return int64(^uint64(0) >> 1), nil
	}
	return int64(freeForCaller), nil
}

// MaxFileSize returns the maximum single-file size supported by the volume
// containing path. FAT32 is limited to 4 GiB minus one byte; other filesystems
// are treated as unbounded for LANVault's purposes.
func MaxFileSize(path string) int64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return int64(^uint64(0) >> 1)
	}
	var volumeName [261]uint16
	var fsName [261]uint16
	var serial, maxComponent, fsFlags uint32
	r1, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&volumeName[0])),
		uintptr(len(volumeName)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComponent)),
		uintptr(unsafe.Pointer(&fsFlags)),
		uintptr(unsafe.Pointer(&fsName[0])),
		uintptr(len(fsName)),
	)
	if r1 == 0 {
		return int64(^uint64(0) >> 1)
	}
	name := syscall.UTF16ToString(fsName[:])
	if name == "FAT32" {
		return 4*1024*1024*1024 - 1
	}
	return int64(^uint64(0) >> 1)
}
