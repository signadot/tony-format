package patches

import (
	"bytes"
	"io"
	"testing"

	"github.com/signadot/tony-format/go-tony/stream"
)

// mockReadCloser tracks whether Close was called
type mockReadCloser struct {
	*bytes.Reader
	closed bool
}

func (m *mockReadCloser) Close() error {
	m.closed = true
	return nil
}

func TestEventReadCloser_SnapshotReader(t *testing.T) {
	// Create a proper binary event
	buf := &bytes.Buffer{}
	evt := &stream.Event{Type: stream.EventNull}
	if err := evt.WriteBinary(buf); err != nil {
		t.Fatalf("WriteBinary failed: %v", err)
	}

	mock := &mockReadCloser{Reader: bytes.NewReader(buf.Bytes())}
	reader := NewSnapshotEventReader(mock)

	// Read an event
	readEvt, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}
	if readEvt.Type != stream.EventNull {
		t.Errorf("Expected EventNull, got %v", readEvt.Type)
	}

	// Close should close the underlying reader
	if err := reader.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !mock.closed {
		t.Error("Close did not close the underlying reader")
	}
}

func TestEventReadCloser_EmptyReader(t *testing.T) {
	reader := NewEmptyEventReader()

	// Should return EOF
	evt, err := reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}
	if evt != nil {
		t.Errorf("Expected nil event, got %v", evt)
	}

	// Close should not error (no-op)
	if err := reader.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestEventReadCloser_Interface(t *testing.T) {
	// Verify our constructors return EventReadCloser
	var _ EventReadCloser = NewEmptyEventReader()
	var _ EventReadCloser = NewSnapshotEventReader(io.NopCloser(&bytes.Buffer{}))
}

// mockWriteCloser tracks whether Close was called
type mockWriteCloser struct {
	buf    *bytes.Buffer
	closed bool
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.closed = true
	return nil
}

func TestEventWriteCloser_FileWriter(t *testing.T) {
	mock := &mockWriteCloser{buf: &bytes.Buffer{}}
	writer := NewFileEventSink(mock)

	// Write an event
	evt := &stream.Event{Type: stream.EventNull}
	if err := writer.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Close should close the underlying writer
	if err := writer.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if !mock.closed {
		t.Error("Close did not close the underlying writer")
	}
}

func TestEventWriteCloser_BufferWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewBufferEventSink(buf)

	// Write an event
	evt := &stream.Event{Type: stream.EventNull}
	if err := writer.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Close should not error (no-op for buffers)
	if err := writer.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}

	// Verify event was written
	if buf.Len() == 0 {
		t.Error("Expected event to be written to buffer")
	}
}

func TestEventWriteCloser_Interface(t *testing.T) {
	// Verify our constructors return EventWriteCloser
	var _ EventWriteCloser = NewBufferEventSink(&bytes.Buffer{})
	var _ EventWriteCloser = NewFileEventSink(&mockWriteCloser{buf: &bytes.Buffer{}})
}
