package snap

import (
	"bufio"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
)

// Seeking a path.
//
// A path is found in a snapshot through its directory: one table per segment, each
// binary-searched, and the entry at the end says exactly which bytes of the events are
// the value -- its key, its head comments, the value and its own line comment. A read
// streams those bytes and nothing else. A segment a table does not hold, or a path that
// runs on through a scalar, is absent, and is known to be at that segment: nothing is
// read that is not the path's.
//
// The chunk index, and the scan forward from the chunk before the path, is how a path
// was found before there was a directory. It remains for a snapshot written without one
// and nothing else. It cost, for a path the snapshot does not hold, a read of every event
// from the chunk to the end of the snapshot with the whole path rendered at each: 94 ms
// on a 5 MB snapshot, paid once per leaf of a write that creates what it writes
// (9a4d9jm2h12krj9hp9n0).

// EventReader is a path's events read out of a snapshot, one at a time, ending in
// io.EOF. A path the snapshot does not hold reads as no events.
type EventReader interface {
	ReadEvent() (*stream.Event, error)
	Close() error
}

// entryAt answers the directory's entry for the path p, and whether the snapshot holds
// it. The root is the whole of the events. The snapshot must have a directory.
func (s *Snapshot) entryAt(p string) (Entry, bool, error) {
	kp, err := kpath.Parse(p)
	if err != nil {
		return Entry{}, false, err
	}
	if kp == nil {
		return Entry{Offset: 0, Size: int64(s.EventSize)}, true, nil
	}
	if s.dir.root == dirNone {
		return Entry{}, false, nil // a scalar has no children
	}
	tb, err := s.dir.open(int64(s.dir.root), 0)
	if err != nil {
		return Entry{}, false, err
	}
	for x := kp; ; x = x.Next {
		e, found, err := tb.find(dirSegment(x))
		if err != nil || !found {
			return Entry{}, false, err
		}
		if x.Next == nil {
			return e, true, nil
		}
		if !e.Kind.IsContainer() {
			return Entry{}, false, nil // the path runs on through a scalar
		}
		if tb, err = tb.Child(e); err != nil {
			return Entry{}, false, err
		}
	}
}

// parentOf is the path of p's container, "" for a child of the root or the root.
func parentOf(p string) string {
	kp, err := kpath.Parse(p)
	if err != nil || kp == nil || kp.Parent() == nil {
		return ""
	}
	return kp.Parent().String()
}

// dirSegment is x's segment as a table spells it: a field quoted as kpath quotes one.
func dirSegment(x *kpath.KPath) string {
	if x.Field != nil {
		return kpath.Field(*x.Field).String()
	}
	return x.SegmentString()
}

// windowReader streams one entry's events: the bytes the directory says are the entry's,
// less the key that introduces it, which is the caller's path and not the value.
type windowReader struct {
	r          *bufio.Reader
	introduced bool // past the entry's own key
}

// window opens a reader over e's events. A zero Size is an absent entry, and reads as
// nothing.
func (s *Snapshot) window(e Entry) (*windowReader, error) {
	if e.Offset < 0 || e.Size < 0 || e.Offset+e.Size > int64(s.EventSize) {
		return nil, fmt.Errorf("entry %s: events %d+%d are outside the stream of %d", e.Segment, e.Offset, e.Size, s.EventSize)
	}
	if _, err := s.R.Seek(s.eventsAt+e.Offset, io.SeekStart); err != nil {
		return nil, err
	}
	return &windowReader{r: bufio.NewReaderSize(io.LimitReader(s.R, e.Size), 64<<10)}, nil
}

func (w *windowReader) ReadEvent() (*stream.Event, error) {
	for {
		ev := &stream.Event{}
		if err := ev.ReadBinary(w.r); err != nil {
			return nil, err
		}
		if !w.introduced && (ev.Type == stream.EventKey || ev.Type == stream.EventIntKey) {
			continue
		}
		if ev.IsValueStart() {
			w.introduced = true
		}
		return ev, nil
	}
}

func (w *windowReader) Close() error { return nil }
