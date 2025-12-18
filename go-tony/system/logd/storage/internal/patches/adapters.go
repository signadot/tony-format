package patches

import (
	"bytes"
	"io"

	"github.com/signadot/tony-format/go-tony/stream"
)

// EventReadCloser extends stream.EventReader with Close for managing resources.
// This is specific to storage layer needs, not part of the general stream package.
type EventReadCloser interface {
	stream.EventReader
	io.Closer
}

// EventWriteCloser extends stream.EventSink with Close for managing resources.
// This is specific to storage layer needs, not part of the general stream package.
type EventWriteCloser interface {
	stream.EventSink
	io.Closer
}

// eventReaderCloser wraps a stream.EventReader with an optional closer.
type eventReaderCloser struct {
	reader stream.EventReader
	closer io.Closer
}

func (r *eventReaderCloser) ReadEvent() (*stream.Event, error) {
	return r.reader.ReadEvent()
}

func (r *eventReaderCloser) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// NewEmptyEventReader creates an empty event reader.
// Returns an EventReadCloser with a no-op Close method.
func NewEmptyEventReader() EventReadCloser {
	return &eventReaderCloser{
		reader: stream.NewEmptyEventReader(),
		closer: nil,
	}
}

// NewSnapshotEventReader creates an event reader from a closable reader positioned at snapshot events.
// Takes ownership of the closer and will close it when Close() is called.
func NewSnapshotEventReader(r io.ReadCloser) EventReadCloser {
	return &eventReaderCloser{
		reader: stream.NewBinaryEventReader(r),
		closer: r,
	}
}

// eventWriteCloser wraps a stream.EventSink with an optional closer.
type eventWriteCloser struct {
	sink   stream.EventSink
	closer io.Closer
}

func (w *eventWriteCloser) WriteEvent(ev *stream.Event) error {
	return w.sink.WriteEvent(ev)
}

func (w *eventWriteCloser) Close() error {
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

// NewBufferEventSink creates an event sink that writes to a byte buffer.
// Returns an EventWriteCloser with a no-op Close since buffers don't need closing.
func NewBufferEventSink(buf *bytes.Buffer) EventWriteCloser {
	return &eventWriteCloser{
		sink:   stream.NewBufferEventSink(buf),
		closer: nil,
	}
}

// NewFileEventSink creates an event sink that writes to a file or other closable writer.
// Takes ownership of the closer and will close it when Close() is called.
func NewFileEventSink(w io.WriteCloser) EventWriteCloser {
	return &eventWriteCloser{
		sink:   stream.NewBinaryEventWriter(w),
		closer: w,
	}
}
