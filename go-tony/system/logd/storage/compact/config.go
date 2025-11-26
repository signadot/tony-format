package compact

import (
	"log/slog"
)

type Config struct {
	// Configuration
	Divisor int

	Log *slog.Logger
}
