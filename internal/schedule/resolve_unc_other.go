//go:build !windows

package schedule

import (
	"fmt"
)

// getUNCForDrive is only implemented on Windows (WNetGetConnection). On other platforms always returns ("", false).
func getUNCForDrive(drive string) (string, bool) {
	return fmt.Sprintf("Getting UNC for drive %s is not implemented on this platform", drive), false
}
