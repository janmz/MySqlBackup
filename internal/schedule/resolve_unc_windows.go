//go:build windows

package schedule

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ERROR_SUCCESS       = 0
	ERROR_BAD_DEVICE    = 1200 // ungültiger Laufwerksbuchstabe
	ERROR_MORE_DATA     = 234  // Puffer zu klein
	ERROR_NOT_CONNECTED = 2250 // Laufwerk ist kein Netzlaufwerk
)

var (
	mpr                = windows.NewLazySystemDLL("mpr.dll")
	wnetGetConnectionW = mpr.NewProc("WNetGetConnectionW")
)

// getUNCForDrive returns the UNC root for the given drive (e.g. "N:" -> "\\server\share").
// Uses WNetGetConnectionW; returns ("", false) on error or if the drive is not a network drive.
func getUNCForDrive(drive string) (string, bool) {
	localName, err := windows.UTF16PtrFromString(drive)
	if err != nil {
		return "", false
	}
	bufLen := uint32(256)
	buf := make([]uint16, bufLen)
	r1, _, _ := wnetGetConnectionW.Call(
		uintptr(unsafe.Pointer(localName)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)
	switch r1 {
	case ERROR_SUCCESS:
		unc := windows.UTF16ToString(buf)
		if len(unc) >= 2 && unc[:2] == `\\` {
			return strings.TrimSuffix(unc, `\`), true
		}
		return "", false
	case ERROR_MORE_DATA:
		buf = make([]uint16, bufLen)
		r1, _, _ = wnetGetConnectionW.Call(
			uintptr(unsafe.Pointer(localName)),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&bufLen)),
		)
		if r1 != ERROR_SUCCESS {
			return "", false
		}
		unc := windows.UTF16ToString(buf)
		if len(unc) >= 2 && unc[:2] == `\\` {
			return strings.TrimSuffix(unc, `\`), true
		}
		return "", false
	case ERROR_NOT_CONNECTED, ERROR_BAD_DEVICE:
		return "", false
	default:
		return "", false
	}
}
