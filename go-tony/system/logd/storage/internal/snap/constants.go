package snap

import (
	"os"
	"strconv"
)

const (
	DefaultChunkSize = 4096
	// HeaderSize is the header a snapshot is written with: the magic, the events' size,
	// the directory's size, the root table's offset and the index's size (directory.go).
	HeaderSize = 32
	// LegacyHeaderSize is the header of a snapshot written before the directory: the
	// events' size and the index's size, and the index followed the events directly.
	LegacyHeaderSize = 12
)

// headerMagic opens a snapshot with a directory. A legacy header begins with the events'
// size, which never has these high bytes.
var headerMagic = [4]byte{'S', 'N', 'A', 'P'}

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
