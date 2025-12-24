package server

import (
	"log/slog"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// Spec holds the runtime specification for the server.
// Config contains the serializable settings loaded from a file.
type Spec struct {
	Config  *Config
	Storage *storage.Storage
	Log     *slog.Logger
}
