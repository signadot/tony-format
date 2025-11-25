package server

import (
	"log/slog"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

type Config struct {
	Storage       *storage.Storage
	Log           *slog.Logger
	MaxTxDuration time.Duration
}
