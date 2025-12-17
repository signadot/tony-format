package snap

import (
	"bufio"
	"io"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

type PathFinder struct {
	idxPath    *kpath.KPath
	desPath    *kpath.KPath
	R          io.ReadSeekCloser
	initOffset int64

	state  *stream.State
	events []stream.Event
}

func NewPathFinder(r io.ReadSeekCloser, off int64, idxPath, desPath *kpath.KPath) (*PathFinder, error) {
	st, err := stream.KPathState(idxPath.String())
	if err != nil {
		return nil, err
	}

	if idxPath != nil {
		last := idxPath.LastSegment()
		switch last.EntryKind() {
		case kpath.FieldEntry:
			st.ProcessEvent(&stream.Event{Type: stream.EventNull})

		case kpath.SparseArrayEntry:
			st.ProcessEvent(&stream.Event{Type: stream.EventNull})
		}
	}

	return &PathFinder{
		idxPath:    idxPath,
		desPath:    desPath,
		R:          r,
		initOffset: off,
		state:      st,
	}, nil
}

// FindEvents reads events from the snapshot and returns only those events
// that correspond to the desired path.
func (pf *PathFinder) FindEvents() ([]stream.Event, error) {
	// Seek to the initial offset (relative to snapshot start)
	absOffset := int64(HeaderSize) + pf.initOffset
	_, err := pf.R.Seek(absOffset, io.SeekStart)
	if err != nil {
		return nil, err
	}

	// Read serialized events line by line and deserialize them
	scanner := bufio.NewScanner(pf.R)

	desPathStr := pf.desPath.String()

	// Read events, tracking state, until we reach and finish the desired path
	collecting := false
	depth := 0
	events := []stream.Event{}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Deserialize the event from wire format
		evt := &stream.Event{}
		if err := evt.FromTony(line); err != nil {
			return nil, err
		}

		if err := pf.state.ProcessEvent(evt); err != nil {
			return nil, err
		}
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
		} else if pf.state.CurrentPath() == desPathStr {
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

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return events, nil
}
