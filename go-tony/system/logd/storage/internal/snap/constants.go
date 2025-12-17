package snap

import (
	"os"
	"strconv"
)

const (
	DefaultChunkSize = 4096
	HeaderSize       = 12
)

// GetChunkSize returns the maximum chunk size for indexing.
// Defaults to DefaultMaxChunkSize (4096) if SNAP_MAX_CHUNK_SIZE environment variable is not set.
// This allows tests to use smaller chunk sizes to exercise chunk boundary conditions.
func GetChunkSize() int {
	if envSize := os.Getenv("SNAP_MAX_CHUNK_SIZE"); envSize != "" {
		if size, err := strconv.Atoi(envSize); err == nil && size > 0 {
			return size
		}
	}
	return DefaultChunkSize
}
