//go:build windows

package disk

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func available(path string) (uint64, error) {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeBytesAvailable uint64
	r, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(ptr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		0,
		0,
	)
	if r == 0 {
		return 0, err
	}
	return freeBytesAvailable, nil
}
