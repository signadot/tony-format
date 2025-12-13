package snap

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

type Snapshot struct {
	RC        R
	Index     *Index
	EventSize uint64 // Size of event stream in bytes
}

func Open(rc R) (*Snapshot, error) {
	// Read header: [8 bytes: event stream size][4 bytes: index size]
	header := make([]byte, 12)
	if _, err := rc.ReadAt(header, 0); err != nil {
		return nil, err
	}

	eventSize := binary.BigEndian.Uint64(header[0:8])
	indexSize := binary.BigEndian.Uint32(header[8:12])

	// Calculate offsets
	eventOffset := int64(HeaderSize)
	indexOffset := eventOffset + int64(eventSize)

	// Read index from the calculated offset
	indexReader := io.NewSectionReader(rc, indexOffset, int64(indexSize))
	index, err := OpenIndex(indexReader, int(indexSize))
	if err != nil {
		return nil, err
	}

	return &Snapshot{
		RC:        rc,
		Index:     index,
		EventSize: eventSize,
	}, nil
}

func (s *Snapshot) Close() error {
	return s.RC.Close()
}

// readEventsUntilComplete reads events from the given offset and processes them with state
// until the state stack size (depth) reaches 0, indicating a complete value/node has been read.
// Uses do...while pattern: processes at least one event, then checks if depth is 0.
// The offset is relative to the start of the event stream (after the header).
// Reads only what it needs - the decoder will naturally stop at EOF if boundaries are reached.
func (s *Snapshot) readEventsUntilComplete(offset int64, state *stream.State) error {
	// Calculate absolute offset (header + event stream offset)
	absoluteOffset := int64(HeaderSize) + offset

	// Create a decoder for reading from this offset
	// Use a very large size - the decoder will naturally stop at EOF or when depth reaches 0
	// We don't know how many bytes we need, so we read until depth is 0 or EOF
	// Using max int64 to effectively read until EOF
	dec, err := stream.NewDecoder(io.NewSectionReader(s.RC, absoluteOffset, 1<<62), stream.WithWire())
	if err != nil {
		return err
	}

	// Do...while pattern: process at least one event, then check depth
	for {
		ev, err := dec.ReadEvent()
		if err != nil {
			if err == io.EOF {
				// If we hit EOF but depth is still > 0, that's an error
				if state.Depth() > 0 {
					return io.ErrUnexpectedEOF
				}
				break
			}
			return err
		}

		if err := state.ProcessEvent(ev); err != nil {
			return err
		}

		// Check if we've completed reading a full value (depth == 0)
		if state.Depth() == 0 {
			break
		}
	}

	return nil
}

// NodeAt reads events from the given offset and decodes them into an ir.Node.
// The offset is relative to the start of the event stream (after the header).
// Reads events until a complete value/node is read (depth reaches 0).
//
// Events are stored as Event struct serializations (written via Event.ToTony()).
// Each Event is written as a complete Tony object. We use a decoder to read
// the wire format events that represent Event struct objects, track their boundaries,
// reconstruct the original Events, and convert to ir.Node.
func (s *Snapshot) NodeAt(offset int64) (*ir.Node, error) {
	absoluteOffset := int64(HeaderSize) + offset
	reader := io.NewSectionReader(s.RC, absoluteOffset, 1<<62)
	
	dec, err := stream.NewDecoder(reader, stream.WithWire())
	if err != nil {
		return nil, err
	}
	
	var events []stream.Event
	state := stream.NewState()
	var eventStructEvents []stream.Event
	var eventStructDepth int
	
	for {
		ev, err := dec.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if state.Depth() > 0 {
					return nil, io.ErrUnexpectedEOF
				}
				break
			}
			return nil, fmt.Errorf("read event: %w", err)
		}
		
		// Track Event struct object boundaries
		if ev.Type == stream.EventBeginObject {
			if eventStructDepth == 0 {
				eventStructEvents = nil
			}
			eventStructEvents = append(eventStructEvents, *ev)
			eventStructDepth++
		} else if ev.Type == stream.EventEndObject {
			eventStructEvents = append(eventStructEvents, *ev)
			eventStructDepth--
			if eventStructDepth == 0 {
				// Complete Event struct object - convert to Event
				eventNode, err := stream.EventsToNode(eventStructEvents)
				if err != nil {
					return nil, fmt.Errorf("convert event struct events to node: %w", err)
				}
				
				originalEvent := &stream.Event{}
				if err := originalEvent.FromTonyIR(eventNode); err != nil {
					return nil, fmt.Errorf("reconstruct event: %w", err)
				}
				
				events = append(events, *originalEvent)
				
				if err := state.ProcessEvent(originalEvent); err != nil {
					return nil, err
				}
				
				if state.Depth() == 0 {
					break
				}
			}
		} else {
			eventStructEvents = append(eventStructEvents, *ev)
		}
	}
	
	return stream.EventsToNode(events)
}
