package snap

import (
	"fmt"
	"os"
)

// debugEnabled returns true if debug logging is enabled via SNAP_DEBUG env var
func debugEnabled() bool {
	return os.Getenv("SNAP_DEBUG") != ""
}

// debugLog prints debug messages if SNAP_DEBUG is enabled
func debugLog(format string, args ...interface{}) {
	if debugEnabled() {
		fmt.Printf("[SNAP_DEBUG] "+format+"\n", args...)
	}
}
