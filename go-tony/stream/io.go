package stream

import (
	"bytes"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// EventReader provides events from a source (snapshot, empty stream, etc.).
type EventReader interface {
	ReadEvent() (*Event, error)
}

// EventWriter receives events (builder, writer, etc.).
type EventWriter interface {
	WriteEvent(*Event) error
}

// EmptyEventReader provides an empty event stream (for null state).
type EmptyEventReader struct{}

// NewEmptyEventReader creates an empty event reader.
func NewEmptyEventReader() *EmptyEventReader {
	return &EmptyEventReader{}
}

// ReadEvent returns io.EOF immediately (empty stream).
func (r *EmptyEventReader) ReadEvent() (*Event, error) {
	return nil, io.EOF
}

// BinaryEventReader reads events from an io.Reader using binary format.
type BinaryEventReader struct {
	r io.Reader
}

// NewBinaryEventReader creates an event reader from a reader positioned at binary events.
func NewBinaryEventReader(r io.Reader) *BinaryEventReader {
	return &BinaryEventReader{r: r}
}

// ReadEvent reads the next event using binary format.
func (r *BinaryEventReader) ReadEvent() (*Event, error) {
	evt := &Event{}
	if err := evt.ReadBinary(r.r); err != nil {
		return nil, err
	}
	return evt, nil
}

// BinaryEventWriter writes events to an io.Writer using binary format.
type BinaryEventWriter struct {
	w io.Writer
}

// NewBinaryEventWriter creates an event writer that writes to w.
func NewBinaryEventWriter(w io.Writer) *BinaryEventWriter {
	return &BinaryEventWriter{w: w}
}

// WriteEvent writes an event in binary format to the writer.
func (w *BinaryEventWriter) WriteEvent(ev *Event) error {
	return ev.WriteBinary(w.w)
}

// BufferEventSink writes events to a buffer using compact binary encoding.
type BufferEventSink struct {
	buf *bytes.Buffer
}

// NewBufferEventSink creates an event sink that writes to a byte buffer.
func NewBufferEventSink(buf *bytes.Buffer) *BufferEventSink {
	return &BufferEventSink{buf: buf}
}

// WriteEvent writes an event in binary format to the buffer.
func (s *BufferEventSink) WriteEvent(ev *Event) error {
	return ev.WriteBinary(s.buf)
}

// ReadDocument reads events until a document is complete and rebuilds it.
//
// A document ends where its structure closes -- depth back to zero -- and a
// comment carries no structure, so a comment cannot end one. Read as though it
// could, a document that OPENS with a comment looked complete at its first
// event, and what was handed on was a document consisting of a comment: a
// request with a leading comment parsed as no request at all.
//
// This loop was copied into six readers -- both logd ends, both docd ends, the
// mount client and the transaction pool -- with the same rule in each, which is
// how six answers to one question drift apart. It lives here now, beside the
// decoder whose events it reads (3cdjz00jh12krns4g1n0).
func ReadDocument(dec *Decoder) (*ir.Node, error) {
	var events []Event
	started := false

	for {
		event, err := dec.ReadEvent()
		if err != nil {
			if err == io.EOF {
				if len(events) > 0 {
					return EventsToNode(events)
				}
				return nil, io.EOF
			}
			return nil, err
		}

		events = append(events, *event)
		if event.Type == EventHeadComment || event.Type == EventLineComment {
			continue // carries no depth, so it completes nothing
		}
		started = true

		if started && dec.Depth() == 0 {
			return EventsToNode(events)
		}
	}
}
