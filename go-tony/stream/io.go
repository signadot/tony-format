package stream

import (
	"bytes"
	"io"
)

// EventReader provides events from a source (snapshot, empty stream, etc.).
type EventReader interface {
	ReadEvent() (*Event, error)
}

// EventSink receives events (builder, writer, etc.).
type EventSink interface {
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
