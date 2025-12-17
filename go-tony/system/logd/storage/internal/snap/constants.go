package snap

import (
	"os"
	"strconv"
)

const (
	DefaultChunkSize = 4096
	HeaderSize       = 12
)

// GetChunkSize returns the chunk size for indexing (bytes).
// Defaults to 4096. Override with SNAP_MAX_CHUNK_SIZE env var.
func GetChunkSize() int {
	if envSize := os.Getenv("SNAP_MAX_CHUNK_SIZE"); envSize != "" {
		if size, err := strconv.Atoi(envSize); err == nil && size > 0 {
			return size
		}
	}
	return DefaultChunkSize
}
