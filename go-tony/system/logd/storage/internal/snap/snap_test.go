package snap

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestSnapshotOpen(t *testing.T) {
	// Create a test snapshot with:
	// - Event stream size: 100 bytes
	// - Index size: calculated from serialized index
	// - Event stream: 100 bytes of dummy data
	// - Index: valid index data

	eventSize := uint64(100)

	// Create test index
	testIndex := &Index{
		Entries: []IndexEntry{
			{
				Path:   mustParsePath(t, "a"),
				Offset: 0,
			},
			{
				Path:   mustParsePath(t, "a.b"),
				Offset: 50,
			},
		},
	}

	// Serialize index
	indexData, err := testIndex.ToTony()
	if err != nil {
		t.Fatalf("ToTony() error = %v", err)
	}

	// Build snapshot file
	var buf bytes.Buffer

	// Write header
	header := make([]byte, 12)
	binary.BigEndian.PutUint64(header[0:8], eventSize)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(indexData)))
	if _, err := buf.Write(header); err != nil {
		t.Fatalf("Write header error = %v", err)
	}

	// Write event stream (dummy data)
	eventData := make([]byte, int(eventSize))
	for i := range eventData {
		eventData[i] = byte(i % 256)
	}
	if _, err := buf.Write(eventData); err != nil {
		t.Fatalf("Write event stream error = %v", err)
	}

	// Write index
	if _, err := buf.Write(indexData); err != nil {
		t.Fatalf("Write index error = %v", err)
	}

	// Create RC from buffer
	rc := &bytesReaderAt{buf: buf.Bytes()}

	// Open snapshot
	snapshot, err := Open(rc)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer snapshot.Close()

	// Verify snapshot fields
	if snapshot.EventSize != eventSize {
		t.Errorf("EventSize = %d, want %d", snapshot.EventSize, eventSize)
	}

	// Verify index was read correctly
	if snapshot.Index == nil {
		t.Fatal("Index is nil")
	}
	if len(snapshot.Index.Entries) != len(testIndex.Entries) {
		t.Fatalf("Index.Entries length = %d, want %d", len(snapshot.Index.Entries), len(testIndex.Entries))
	}

	for i, wantEntry := range testIndex.Entries {
		gotEntry := snapshot.Index.Entries[i]
		if gotEntry.Path.KPath.Compare(&wantEntry.Path.KPath) != 0 {
			t.Errorf("Entry[%d].Path = %q, want %q", i, gotEntry.Path.String(), wantEntry.Path.String())
		}
		if gotEntry.Offset != wantEntry.Offset {
			t.Errorf("Entry[%d].Offset = %d, want %d", i, gotEntry.Offset, wantEntry.Offset)
		}
	}
}

func TestSnapshotOpen_EmptyIndex(t *testing.T) {
	// Test opening a snapshot with an empty index
	eventSize := uint64(50)

	emptyIndex := &Index{
		Entries: []IndexEntry{},
	}

	indexData, err := emptyIndex.ToTony()
	if err != nil {
		t.Fatalf("ToTony() error = %v", err)
	}

	var buf bytes.Buffer

	// Write header
	header := make([]byte, 12)
	binary.BigEndian.PutUint64(header[0:8], eventSize)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(indexData)))
	if _, err := buf.Write(header); err != nil {
		t.Fatalf("Write header error = %v", err)
	}

	// Write event stream
	eventData := make([]byte, int(eventSize))
	if _, err := buf.Write(eventData); err != nil {
		t.Fatalf("Write event stream error = %v", err)
	}

	// Write index
	if _, err := buf.Write(indexData); err != nil {
		t.Fatalf("Write index error = %v", err)
	}

	rc := &bytesReaderAt{buf: buf.Bytes()}

	snapshot, err := Open(rc)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer snapshot.Close()

	if snapshot.Index == nil {
		t.Fatal("Index is nil")
	}
	if len(snapshot.Index.Entries) != 0 {
		t.Errorf("Index.Entries length = %d, want 0", len(snapshot.Index.Entries))
	}
}

func TestSnapshotOpen_InvalidHeader(t *testing.T) {
	// Test with invalid header (too short)
	var buf bytes.Buffer
	buf.Write([]byte{1, 2, 3}) // Only 3 bytes, need 12

	rc := &bytesReaderAt{buf: buf.Bytes()}

	_, err := Open(rc)
	if err == nil {
		t.Error("Open() should return error for invalid header")
	}
}

// bytesReaderAt implements RC interface for testing
type bytesReaderAt struct {
	buf []byte
}

func (r *bytesReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, io.EOF
	}
	if off >= int64(len(r.buf)) {
		return 0, io.EOF
	}
	n = copy(p, r.buf[off:])
	if n < len(p) {
		err = io.EOF
	}
	return n, err
}

func (r *bytesReaderAt) Close() error {
	return nil
}
