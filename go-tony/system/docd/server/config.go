package server

import (
	"log/slog"
	"time"
)

// Spec holds the runtime specification for the docd server.
type Spec struct {
	Config   *Config
	LogdAddr string // Address of logd server to connect to
	Log      *slog.Logger

	// PatchTagFilter classifies which tag heads block static cross-mount patch
	// decomposition (see TagFilter). Optional; defaults to defaultTagFilter.
	PatchTagFilter TagFilter

	// MountForceAfter is how long a mount/unmount waits for overlapping watch
	// readers to drain before force-ending them (see mountCoord). A zero value
	// uses defaultMountForceAfter; to wait forever, callers pass force_after "0" on
	// the wire (which maps to the coordinator's 0 = infinity).
	MountForceAfter time.Duration
}

// defaultMountForceAfter bounds how long a mount/unmount blocks on overlapping
// watches when the spec does not set MountForceAfter, so a stuck reader cannot
// wedge mounts indefinitely by default.
const defaultMountForceAfter = 5 * time.Second
