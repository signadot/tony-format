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
//
// The window is the value AND the comments that are its own. A head comment
// precedes the value it describes and a line comment follows it, so collecting
// from the value's first event to its last answered with a node stripped of
// exactly the comments that belong to it while keeping every comment inside it.
// Head comments are held until it is known whose they are; the window stays open
// one event past the end for a line comment (3cdjz00jh12krns4g1n0).
func (pf *PathFinder) FindEvents() ([]stream.Event, error) {
	desPathStr := pf.desPath.String()
	collecting := false
	started := false
	closing := false
	depth := 0
	events := []stream.Event{}
	var pendingHead []stream.Event

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

		// The value is complete; only a line comment of its own may still follow.
		if closing {
			if evt.Type == stream.EventLineComment {
				events = append(events, *evt)
			}
			break
		}

		// If we were collecting and moved past the target path, stop
		if collecting {
			switch evt.Type {
			case stream.EventHeadComment, stream.EventLineComment:
				// A comment ends nothing. Collection begins at the KEY, and a head
				// comment stands between the key and the value it introduces, so
				// at depth 0 this read as "the value is complete" and the window
				// closed on a comment with no value in it -- the read answered
				// with nothing at all (3cdjz00jh12krns4g1n0).
				events = append(events, *evt)
				continue
			case stream.EventBeginObject, stream.EventBeginArray:
				depth++
				started = true
			case stream.EventEndObject, stream.EventEndArray:
				depth--
			default:
				started = true
			}
			if depth >= 0 {
				events = append(events, *evt)
			}
			if started && depth <= 0 {
				closing = true
			}
			continue
		}

		// A comment before the value it belongs to: held, since whose it is is not
		// known until the value arrives. A line comment here follows a value that
		// was not ours, so it is not ours either.
		switch evt.Type {
		case stream.EventHeadComment:
			pendingHead = append(pendingHead, *evt)
			continue
		case stream.EventLineComment:
			continue
		}

		if currentPath == desPathStr {
			switch evt.Type {
			case stream.EventIntKey, stream.EventKey:
				collecting = true
				events = append(events, pendingHead...)
			case stream.EventBeginArray, stream.EventBeginObject:
				collecting = true
				started = true
				depth++
				events = append(events, pendingHead...)
				events = append(events, *evt)
			case stream.EventEndArray, stream.EventEndObject:
				collecting = true
				started = true
			default:
				events = append(events, pendingHead...)
				events = append(events, *evt)
				closing = true
			}
		}
		// Whatever was held belonged to the value just passed, ours or not.
		pendingHead = nil
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

// PathEventReader is a streaming version of PathFinder that yields events one at a time.
// Implements stream.EventReader interface for use with StreamingProcessor.
type PathEventReader struct {
	pf               *PathFinder
	desPathStr       string
	collecting       bool
	started          bool
	closing          bool
	depth            int
	chunkBuf         *bytes.Reader
	chunkStartOffset int64
	fileOffset       int64
	done             bool

	// out holds events ready to hand back: a value's head comments arrive before
	// it is known whose they are, so they are held and then released ahead of it.
	out         []stream.Event
	pendingHead []stream.Event
}

// emit queues events to hand back in order.
func (r *PathEventReader) emit(evs ...stream.Event) {
	r.out = append(r.out, evs...)
}

// next takes the event at the head of the queue, or nil when it is empty.
func (r *PathEventReader) next() *stream.Event {
	if len(r.out) == 0 {
		return nil
	}
	ev := r.out[0]
	r.out = r.out[1:]
	return &ev
}

// NewPathEventReader creates a streaming event reader for the given path.
func NewPathEventReader(r io.ReadSeekCloser, index *Index, off int64, idxPath, desPath *kpath.KPath, eventSize int64) (*PathEventReader, error) {
	pf, err := NewPathFinder(r, index, off, idxPath, desPath, eventSize)
	if err != nil {
		return nil, err
	}
	return &PathEventReader{
		pf:         pf,
		desPathStr: desPath.String(),
		fileOffset: off,
	}, nil
}

// ReadEvent returns the next event for the target path.
// Returns io.EOF when all events have been read.
//
// The window is FindEvents', one event at a time: a value's own head comments
// come out ahead of it and its own line comment after it, and a comment inside
// the window ends nothing. See FindEvents.
func (r *PathEventReader) ReadEvent() (*stream.Event, error) {
	if ev := r.next(); ev != nil {
		return ev, nil
	}
	if r.done {
		return nil, io.EOF
	}

	for {
		// If no buffered chunk or exhausted, read next chunk
		if r.chunkBuf == nil || r.chunkBuf.Len() == 0 {
			var err error
			r.chunkBuf, err = r.pf.readNextChunk(r.fileOffset)
			if err == io.EOF {
				r.done = true
				return nil, io.EOF
			}
			if err != nil {
				return nil, err
			}
			r.chunkStartOffset = r.fileOffset
		}

		// Read event from buffered chunk
		evt := &stream.Event{}
		if err := evt.ReadBinary(r.chunkBuf); err != nil {
			if err == io.EOF {
				// Chunk exhausted, advance fileOffset and read next chunk
				afterPos := r.chunkBuf.Size() - int64(r.chunkBuf.Len())
				r.fileOffset = r.chunkStartOffset + afterPos
				continue
			}
			return nil, err
		}
		// Update fileOffset to reflect bytes consumed from chunk
		afterPos := r.chunkBuf.Size() - int64(r.chunkBuf.Len())
		r.fileOffset = r.chunkStartOffset + afterPos

		if err := r.pf.state.ProcessEvent(evt); err != nil {
			return nil, err
		}

		currentPath := r.pf.state.CurrentPath()

		// The value is complete; only a line comment of its own may still follow.
		if r.closing {
			r.done = true
			if evt.Type == stream.EventLineComment {
				return evt, nil
			}
			return nil, io.EOF
		}

		// If we were collecting and moved past the target path, stop
		if r.collecting {
			switch evt.Type {
			case stream.EventHeadComment, stream.EventLineComment:
				return evt, nil // a comment inside the window ends nothing
			case stream.EventBeginObject, stream.EventBeginArray:
				r.depth++
				r.started = true
			case stream.EventEndObject, stream.EventEndArray:
				r.depth--
			default:
				r.started = true
			}
			if r.started && r.depth <= 0 {
				r.closing = true
			}
			if r.depth >= 0 {
				return evt, nil
			}
			continue
		}

		switch evt.Type {
		case stream.EventHeadComment:
			r.pendingHead = append(r.pendingHead, *evt)
			continue
		case stream.EventLineComment:
			continue // it follows a value that was not ours
		}

		if currentPath == r.desPathStr {
			switch evt.Type {
			case stream.EventIntKey, stream.EventKey:
				r.collecting = true
				r.emit(r.pendingHead...) // the comments above the value the key introduces
			case stream.EventBeginArray, stream.EventBeginObject:
				r.collecting = true
				r.started = true
				r.depth++
				r.emit(r.pendingHead...)
				r.emit(*evt)
			case stream.EventEndArray, stream.EventEndObject:
				r.collecting = true
				r.started = true
			default:
				r.emit(r.pendingHead...)
				r.emit(*evt)
				r.closing = true
			}
			r.pendingHead = nil
			if ev := r.next(); ev != nil {
				return ev, nil
			}
			continue
		}
		// Whatever was held belonged to the value just passed, which was not ours.
		r.pendingHead = nil
	}
}

// Close is a no-op - the underlying reader is owned by the Snapshot.
func (r *PathEventReader) Close() error {
	return nil
}
