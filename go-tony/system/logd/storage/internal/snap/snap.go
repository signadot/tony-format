package snap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// Snapshot is an opened snapshot file providing random access to paths.
type Snapshot struct {
	R         io.ReadSeekCloser
	Index     *Index
	EventSize uint64 // Size of event stream in bytes

}

// Open reads a snapshot from rc.
// The index is loaded into memory; events are read on demand.
func Open(rc R) (*Snapshot, error) {
	// Read header: [8 bytes: event stream size][4 bytes: index size]
	_, err := rc.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	header := make([]byte, 12)
	if _, err := rc.Read(header); err != nil {
		return nil, err
	}

	eventSize := binary.BigEndian.Uint64(header[0:8])
	indexSize := binary.BigEndian.Uint32(header[8:12])

	// Sanity check: index can't be larger than 1GB
	const maxIndexSize = 1 << 30
	if indexSize > maxIndexSize {
		return nil, fmt.Errorf("snapshot index size %d exceeds maximum %d", indexSize, maxIndexSize)
	}

	// Calculate offsets
	eventOffset := int64(HeaderSize)
	// Index is preceded by a newline separator (not included in indexSize)
	indexOffset := eventOffset + int64(eventSize)

	// Read index from the calculated offset
	if _, err := rc.Seek(indexOffset, io.SeekStart); err != nil {
		return nil, err
	}
	index, err := OpenIndex(rc, int(indexSize))
	if err != nil {
		return nil, err
	}
	n := len(index.Entries)
	for i := range n {
		entry := &index.Entries[i]
		if i < n-1 {
			entry.Size = index.Entries[i+1].Offset - entry.Offset
			continue
		}
		entry.Size = int64(eventSize) - entry.Offset
	}

	return &Snapshot{
		R:         rc,
		Index:     index,
		EventSize: eventSize,
	}, nil
}

func (s *Snapshot) Close() error {
	return s.R.Close()
}

// ReadPath reads the IR node at path p.
// Returns nil if path not found.
func (s *Snapshot) ReadPath(p string) (*ir.Node, error) {
	desPath, err := kpath.Parse(p)
	if err != nil {
		return nil, err
	}

	var offset int64
	var startPath *kpath.KPath

	// If index is empty, start from the beginning (root path)
	if len(s.Index.Entries) == 0 {
		offset = 0
		startPath = nil
	} else {
		// Lookup the path in the index
		i, err := s.Index.Lookup(p)
		if err != nil {
			return nil, fmt.Errorf("lookup path %q: %w", p, err)
		}

		entry := &s.Index.Entries[i]
		offset = entry.Offset
		if entry.Path == nil {
			startPath = nil
		} else {
			startPath = &entry.Path.KPath
		}
	}

	pathFinder, err := NewPathFinder(s.R, s.Index, offset, startPath, desPath, int64(s.EventSize))
	if err != nil {
		return nil, err
	}
	events, err := pathFinder.FindEvents()
	if err != nil {
		return nil, err
	}
	return stream.EventsToNode(events)
}

// ReadPathEventReader returns a streaming event reader for the path p.
// Unlike ReadPath, this does not materialize the full subtree in memory.
// The caller must call Close() on the returned reader when done.
// Note: The Snapshot must remain open while the reader is in use.
func (s *Snapshot) ReadPathEventReader(p string) (*PathEventReader, error) {
	desPath, err := kpath.Parse(p)
	if err != nil {
		return nil, err
	}

	var offset int64
	var startPath *kpath.KPath

	// If index is empty, start from the beginning (root path)
	if len(s.Index.Entries) == 0 {
		offset = 0
		startPath = nil
	} else {
		// Lookup the path in the index
		i, err := s.Index.Lookup(p)
		if err != nil {
			return nil, fmt.Errorf("lookup path %q: %w", p, err)
		}

		entry := &s.Index.Entries[i]
		offset = entry.Offset
		if entry.Path == nil {
			startPath = nil
		} else {
			startPath = &entry.Path.KPath
		}
	}

	return NewPathEventReader(s.R, s.Index, offset, startPath, desPath, int64(s.EventSize))
}
