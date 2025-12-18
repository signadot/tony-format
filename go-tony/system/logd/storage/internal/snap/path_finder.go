package snap

import (
	"bytes"
	"io"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// PathFinder seeks to an indexed offset and extracts events for a target path.
//
// Uses stream.KPathState to initialize state for the indexed path. For leaf
// array elements, KPathState positions one element before, so processing the
// first event at the offset advances to the correct position.
type PathFinder struct {
	idxPath      *kpath.KPath
	desPath      *kpath.KPath
	R            io.ReadSeekCloser
	index        *Index // Snapshot index for chunk boundaries
	indexEntryID int    // Current position in index for efficient chunk lookup
	initOffset   int64
	eventSize    int64 // Total size of event stream (boundary to prevent reading into index)

	state  *stream.State
	events []stream.Event
}

// NewPathFinder creates a PathFinder starting at offset off (indexed at idxPath) to find desPath.
//
// Initializes state using stream.KPathState(idxPath), which positions correctly
// for reading events starting at off. For field and sparse array entries, advances
// state past the key by processing a dummy null event.
// index is the snapshot index, used to determine chunk boundaries for buffering.
// eventSize is the total size of the event stream, used to prevent reading past into the index section.
func NewPathFinder(r io.ReadSeekCloser, index *Index, off int64, idxPath, desPath *kpath.KPath, eventSize int64) (*PathFinder, error) {
	st, err := stream.KPathState(idxPath.String())
	if err != nil {
		return nil, err
	}

	if idxPath != nil {
		last := idxPath.LastSegment()
		switch last.EntryKind() {
		case kpath.FieldEntry, kpath.SparseArrayEntry:
			st.ProcessEvent(&stream.Event{Type: stream.EventNull})
		}
	}

	// Find the index entry for the initial offset
	indexEntryID := 0
	for i := range index.Entries {
		if index.Entries[i].Offset <= off {
			indexEntryID = i
		} else {
			break
		}
	}

	return &PathFinder{
		idxPath:      idxPath,
		desPath:      desPath,
		R:            r,
		index:        index,
		indexEntryID: indexEntryID,
		initOffset:   off,
		eventSize:    eventSize,
		state:        st,
	}, nil
}

// FindEvents extracts events for the desired path from the snapshot.
// Buffers chunks for efficient I/O, reading additional chunks as needed.
func (pf *PathFinder) FindEvents() ([]stream.Event, error) {
	desPathStr := pf.desPath.String()
	collecting := false
	depth := 0
	events := []stream.Event{}

	fileOffset := pf.initOffset // Position in event stream (relative to start of events)
	var chunkBuf *bytes.Reader
	var chunkStartOffset int64 // Where current chunk started in the file

	for {
		// If no buffered chunk or exhausted, read next chunk
		if chunkBuf == nil || chunkBuf.Len() == 0 {
			var err error
			chunkBuf, err = pf.readNextChunk(fileOffset)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			chunkStartOffset = fileOffset
		}

		// Read event from buffered chunk
		evt := &stream.Event{}
		if err := evt.ReadBinary(chunkBuf); err != nil {
			if err == io.EOF {
				// Chunk exhausted, advance fileOffset and read next chunk
				afterPos := chunkBuf.Size() - int64(chunkBuf.Len())
				fileOffset = chunkStartOffset + afterPos
				continue
			}
			return nil, err
		}
		// Update fileOffset to reflect bytes consumed from chunk
		afterPos := chunkBuf.Size() - int64(chunkBuf.Len())
		fileOffset = chunkStartOffset + afterPos

		if err := pf.state.ProcessEvent(evt); err != nil {
			return nil, err
		}

		currentPath := pf.state.CurrentPath()

		// If we were collecting and moved past the target path, stop
		if collecting {
			switch evt.Type {
			case stream.EventBeginObject, stream.EventBeginArray:
				depth++
			case stream.EventEndObject, stream.EventEndArray:
				depth--
			}
			if depth >= 0 {
				events = append(events, *evt)
			}
			if depth <= 0 {
				break
			}
		} else if currentPath == desPathStr {
			switch evt.Type {
			case stream.EventIntKey, stream.EventKey:
				collecting = true
			case stream.EventBeginArray, stream.EventBeginObject:
				collecting = true
				depth++
				events = append(events, *evt)
			case stream.EventEndArray, stream.EventEndObject:
				collecting = true
			default:
				events = append(events, *evt)
				return events, nil
			}
		}
	}
	return events, nil
}

// readNextChunk reads the next chunk from the file at the given offset.
// Returns the chunk buffer and updates indexEntryID for next read.
func (pf *PathFinder) readNextChunk(fileOffset int64) (*bytes.Reader, error) {
	// Check if we've reached the end of the event stream
	if fileOffset >= pf.eventSize {
		return nil, io.EOF
	}

	// Determine chunk size using tracked index position
	// Start from current index entry and scan forward
	var chunkSize int64
	for i := pf.indexEntryID; i < len(pf.index.Entries); i++ {
		entry := &pf.index.Entries[i]
		if entry.Offset == fileOffset {
			// Found exact match - use this chunk's size
			chunkSize = entry.Size
			pf.indexEntryID = i
			break
		} else if entry.Offset > fileOffset {
			// We're between chunks - read to next chunk boundary
			chunkSize = entry.Offset - fileOffset
			pf.indexEntryID = i
			break
		}
	}

	// If no entry found (fileOffset is after all index entries), read to end
	if chunkSize == 0 {
		chunkSize = pf.eventSize - fileOffset
	}

	if chunkSize == 0 {
		return nil, io.EOF
	}

	// Seek and read chunk
	absOffset := int64(HeaderSize) + fileOffset
	if _, err := pf.R.Seek(absOffset, io.SeekStart); err != nil {
		return nil, err
	}

	buf := make([]byte, chunkSize)
	n, err := io.ReadFull(pf.R, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	return bytes.NewReader(buf[:n]), nil
}
