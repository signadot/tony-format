package server

import (
	"log/slog"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type Config struct {
	Storage *storage.Storage
	Log     *slog.Logger
}
