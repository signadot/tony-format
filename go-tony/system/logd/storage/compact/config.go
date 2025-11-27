package compact

import (
	"log/slog"
)

type Config struct {
	// Configuration
	Root    string
	Divisor int
	Remove  func(int, int) bool
	Log     *slog.Logger
}
