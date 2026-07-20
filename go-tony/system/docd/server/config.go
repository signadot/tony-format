package server

import (
	"log/slog"
)

// Spec holds the runtime specification for the docd server.
type Spec struct {
	Config   *Config
	LogdAddr string // Address of logd server to connect to
	Log      *slog.Logger

	// PatchTagFilter classifies which tag heads block static cross-mount patch
	// decomposition (see TagFilter). Optional; defaults to defaultTagFilter.
	PatchTagFilter TagFilter
}
