//go:build windows

package schedule

import (
	"os/exec"
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

// getUNCForDrive returns the UNC root for the given drive (e.g. "N:" or "N:\\" -> "\\server\share").
// Uses WNetGetConnectionW; returns ("", false) on error or if the drive is not a network drive.
// Tries "N:" first, then "N:\" as fallback (some contexts expect the trailing backslash).
func getUNCForDrive(drive string) (string, bool) {
	drive = strings.TrimSpace(drive)
	if drive == "" {
		return "", false
	}
	drive = strings.ToUpper(drive[:1]) + drive[1:]
	if unc, ok := getUNCForDriveOnce(drive); ok {
		return unc, true
	}
	// Some contexts expect "N:\" instead of "N:"
	if len(drive) == 2 && drive[1] == ':' {
		if unc, ok := getUNCForDriveOnce(drive + `\`); ok {
			return unc, true
		}
	}
	// Fallback: WMI ProviderName (works when WNetGetConnection fails, e.g. session differences)
	driveLetter := drive
	if len(driveLetter) > 2 {
		driveLetter = driveLetter[:2]
	}
	if unc, ok := getUNCForDriveViaPowerShell(driveLetter); ok {
		return unc, true
	}
	return "", false
}

func getUNCForDriveOnce(localName string) (string, bool) {
	ptr, err := windows.UTF16PtrFromString(localName)
	if err != nil {
		return "", false
	}
	bufLen := uint32(256)
	buf := make([]uint16, bufLen)
	r1, _, _ := wnetGetConnectionW.Call(
		uintptr(unsafe.Pointer(ptr)),
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
		if bufLen == 0 || bufLen > 32768 {
			return "", false
		}
		buf = make([]uint16, bufLen)
		r1, _, _ = wnetGetConnectionW.Call(
			uintptr(unsafe.Pointer(ptr)),
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

// getUNCForDriveViaPowerShell returns UNC root via WMI (Win32_LogicalDisk.ProviderName).
// Used when WNetGetConnectionW returns NOT_CONNECTED (e.g. different logon session).
func getUNCForDriveViaPowerShell(drive string) (string, bool) {
	if len(drive) != 2 || drive[1] != ':' {
		return "", false
	}
	script := `$d = '` + drive + `'; (Get-CimInstance -ClassName Win32_LogicalDisk -Filter "DeviceID='$d'" -ErrorAction SilentlyContinue).ProviderName`
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	unc := strings.TrimSpace(string(out))
	if len(unc) >= 2 && unc[:2] == `\\` {
		return strings.TrimSuffix(unc, `\`), true
	}
	return "", false
}
