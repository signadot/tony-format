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
	R io.ReadSeekCloser
	// Index is the chunk index: loaded by Open only for a snapshot without a directory,
	// which is the only kind a path is sought through it in (seek.go). ChunkIndex loads
	// it for any snapshot.
	Index     *Index
	EventSize uint64 // Size of event stream in bytes

	eventsAt  int64      // where the events begin in R: after the header, whichever header
	dir       *directory // the directory, or nil for a snapshot written before there was one
	indexAt   int64      // where the chunk index is in R
	indexSize int
}

// EventsAt is where the events begin in R.
func (s *Snapshot) EventsAt() int64 { return s.eventsAt }

// Open reads a snapshot from rc.
// The index is loaded into memory; events are read on demand.
//
// Two headers are read: the one a snapshot is written with (HeaderSize, directory.go),
// and the legacy one (LegacyHeaderSize) of a snapshot written before the directory,
// told apart by the magic.
func Open(rc R) (*Snapshot, error) {
	_, err := rc.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(rc, header[:LegacyHeaderSize]); err != nil {
		return nil, err
	}
	var eventSize, dirSize, dirRoot uint64
	var indexSize uint32
	eventOffset := int64(LegacyHeaderSize)
	var dir *directory
	if [4]byte(header[0:4]) == headerMagic {
		if _, err := io.ReadFull(rc, header[LegacyHeaderSize:]); err != nil {
			return nil, err
		}
		eventSize = binary.BigEndian.Uint64(header[4:12])
		dirSize = binary.BigEndian.Uint64(header[12:20])
		dirRoot = binary.BigEndian.Uint64(header[20:28])
		indexSize = binary.BigEndian.Uint32(header[28:32])
		eventOffset = int64(HeaderSize)
		dir = &directory{r: rc, base: eventOffset + int64(eventSize), size: int64(dirSize), root: dirRoot}
	} else {
		eventSize = binary.BigEndian.Uint64(header[0:8])
		indexSize = binary.BigEndian.Uint32(header[8:12])
	}

	// Sanity check: index can't be larger than 1GB
	const maxIndexSize = 1 << 30
	if indexSize > maxIndexSize {
		return nil, fmt.Errorf("snapshot index size %d exceeds maximum %d", indexSize, maxIndexSize)
	}

	s := &Snapshot{
		R:         rc,
		EventSize: eventSize,
		eventsAt:  eventOffset,
		dir:       dir,
		// The index follows the events and the directory (Builder.Close).
		indexAt:   eventOffset + int64(eventSize) + int64(dirSize),
		indexSize: int(indexSize),
	}
	if dir == nil {
		if _, err := s.ChunkIndex(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// ChunkIndex answers the chunk index, reading it on first use.
func (s *Snapshot) ChunkIndex() (*Index, error) {
	if s.Index != nil {
		return s.Index, nil
	}
	if _, err := s.R.Seek(s.indexAt, io.SeekStart); err != nil {
		return nil, err
	}
	index, err := OpenIndex(s.R, s.indexSize)
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
		entry.Size = int64(s.EventSize) - entry.Offset
	}
	s.Index = index
	return index, nil
}

func (s *Snapshot) Close() error {
	return s.R.Close()
}

// ReadPath reads the IR node at path p.
// Returns nil if path not found.
func (s *Snapshot) ReadPath(p string) (*ir.Node, error) {
	if s.dir != nil {
		e, found, err := s.entryAt(p)
		if err != nil || !found {
			return nil, err
		}
		return s.ReadEntry(parentOf(p), e)
	}
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

	pathFinder, err := NewPathFinder(s.R, s.Index, offset, startPath, desPath, int64(s.EventSize), s.eventsAt)
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
func (s *Snapshot) ReadPathEventReader(p string) (EventReader, error) {
	if s.dir != nil {
		e, found, err := s.entryAt(p)
		if err != nil {
			return nil, err
		}
		if !found {
			e = Entry{} // absent: no events
		}
		return s.window(e)
	}
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

	return NewPathEventReader(s.R, s.Index, offset, startPath, desPath, int64(s.EventSize), s.eventsAt)
}
