package snap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/parse"
)

// Open reads a snapshot from the reader.
// The index node is read into memory, and data chunks are loaded lazily via Load().
//
// On-disk format:
//
//	[4 bytes: uint32 index length][index node bytes][data chunks...]
//
// The index length is big-endian uint32. After reading the index, the remaining
// data contains chunks at offsets specified in the index (offsets are relative
// to the start of the data section, i.e., after the index).
//
// The reader must implement io.ReaderAt for random access to data chunks.
// This allows efficient lazy loading without reading the entire snapshot into memory.
// If the reader also implements io.Closer, Close() will be called when the snapshot is closed.
func Open(r io.ReaderAt) (Snapshot, error) {
	// Read index length (4 bytes)
	lengthBytes := make([]byte, 4)
	if _, err := r.ReadAt(lengthBytes, 0); err != nil {
		return nil, fmt.Errorf("failed to read index length: %w", err)
	}

	indexLength := int64(binary.BigEndian.Uint32(lengthBytes))
	if indexLength < 0 {
		return nil, fmt.Errorf("invalid index length: %d", indexLength)
	}

	// Read index node bytes
	indexBytes := make([]byte, indexLength)
	if _, err := r.ReadAt(indexBytes, 4); err != nil {
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	// Parse index node
	indexNode, err := parse.Parse(indexBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse index node: %w", err)
	}

	// Data section starts after the index
	dataStart := int64(4 + indexLength)

	return &snapshotImpl{
		indexNode: indexNode,
		reader:    r,
		readerPos: dataStart, // Data offsets are relative to start of data section
	}, nil
}
