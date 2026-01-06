package server

import (
	"log/slog"
)

// Spec holds the runtime specification for the docd server.
type Spec struct {
	Config   *Config
	LogdAddr string // Address of logd server to connect to
	Log      *slog.Logger
}
