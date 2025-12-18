package stream

import (
	"bytes"
	"io"
	"testing"
)

func TestBinaryEventReader_ReadEvent(t *testing.T) {
	// Create a proper binary event (null with empty tag)
	buf := &bytes.Buffer{}
	evt := &Event{Type: EventNull}
	if err := evt.WriteBinary(buf); err != nil {
		t.Fatalf("WriteBinary failed: %v", err)
	}

	reader := NewBinaryEventReader(buf)

	// Read an event
	readEvt, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}
	if readEvt.Type != EventNull {
		t.Errorf("Expected EventNull, got %v", readEvt.Type)
	}

	// Reading again should return EOF
	_, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected EOF on second read, got %v", err)
	}
}

func TestBinaryEventReader_WithBuffer(t *testing.T) {
	// BinaryEventReader should work with non-closable readers like buffers
	buf := &bytes.Buffer{}
	evt := &Event{Type: EventNull}
	if err := evt.WriteBinary(buf); err != nil {
		t.Fatalf("WriteBinary failed: %v", err)
	}

	reader := NewBinaryEventReader(buf)

	// Should be able to read the event
	readEvt, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}
	if readEvt.Type != EventNull {
		t.Errorf("Expected EventNull, got %v", readEvt.Type)
	}
}

func TestEmptyEventReader_ReadEvent(t *testing.T) {
	reader := NewEmptyEventReader()

	// Should return EOF immediately
	evt, err := reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected io.EOF, got %v", err)
	}
	if evt != nil {
		t.Errorf("Expected nil event, got %v", evt)
	}

	// Second read should also return EOF
	evt, err = reader.ReadEvent()
	if err != io.EOF {
		t.Errorf("Expected io.EOF on second read, got %v", err)
	}
	if evt != nil {
		t.Errorf("Expected nil event on second read, got %v", evt)
	}
}

func TestEventReader_Interface(t *testing.T) {
	// Verify that our types implement EventReader
	var _ EventReader = (*EmptyEventReader)(nil)
	var _ EventReader = (*BinaryEventReader)(nil)
}

func TestBinaryEventWriter_WriteEvent(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewBinaryEventWriter(buf)

	// Write an event
	evt := &Event{Type: EventNull}
	if err := writer.WriteEvent(evt); err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Read it back
	reader := NewBinaryEventReader(buf)
	readEvt, err := reader.ReadEvent()
	if err != nil {
		t.Fatalf("ReadEvent failed: %v", err)
	}
	if readEvt.Type != EventNull {
		t.Errorf("Expected EventNull, got %v", readEvt.Type)
	}
}

func TestEventSink_Interface(t *testing.T) {
	// Verify that our types implement EventSink
	var _ EventSink = (*BinaryEventWriter)(nil)
	var _ EventSink = (*BufferEventSink)(nil)
}

