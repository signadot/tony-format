package snap

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
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
	rc, err := newTestFile()
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(filepath.Dir(rc.path))
	defer rc.Close()
	if _, err = rc.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}

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

	rc, err := newTestFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(rc.path))
	if _, err = rc.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}

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

	rc, err := newTestFile()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(filepath.Dir(rc.path))

	_, err = Open(rc)
	if err == nil {
		t.Error("Open() should return error for invalid header")
	}
}

func TestSnapshotCreateAndRead(t *testing.T) {
	// Create a simple document: {age: 30, name: "alice", status: "active"}
	var inputBuf bytes.Buffer
	enc, err := stream.NewEncoder(&inputBuf, stream.WithWire())
	if err != nil {
		t.Fatalf("NewEncoder() error = %v", err)
	}

	// Build the document - keys must be in sorted order
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("BeginObject() error = %v", err)
	}
	// Write keys in alphabetical order: age, name, status
	if err := enc.WriteKey("age"); err != nil {
		t.Fatalf("WriteKey('age') error = %v", err)
	}
	if err := enc.WriteInt(30); err != nil {
		t.Fatalf("WriteInt(30) error = %v", err)
	}
	if err := enc.WriteKey("name"); err != nil {
		t.Fatalf("WriteKey('name') error = %v", err)
	}
	if err := enc.WriteString("alice"); err != nil {
		t.Fatalf("WriteString('alice') error = %v", err)
	}
	if err := enc.WriteKey("status"); err != nil {
		t.Fatalf("WriteKey('status') error = %v", err)
	}
	if err := enc.WriteString("active"); err != nil {
		t.Fatalf("WriteString('active') error = %v", err)
	}
	if err := enc.WriteKey("z"); err != nil {
		t.Fatalf("WriteKey('z') error = %v", err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatalf("BeginArray() error = %v", err)
	}
	if err := enc.WriteString("zoo"); err != nil {
		t.Fatalf("WriteString('zoo') error = %v", err)
	}
	if err := enc.EndArray(); err != nil {
		t.Fatalf("BeginArray() error = %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("EndObject() error = %v", err)
	}

	// Create snapshot file
	tmpFile, err := newTestFile()
	if err != nil {
		t.Fatalf("newTestFile() error = %v", err)
	}
	defer os.RemoveAll(filepath.Dir(tmpFile.path))
	defer tmpFile.Close()

	// Build the snapshot
	index := &Index{Entries: []IndexEntry{}}
	builder, err := NewBuilder(tmpFile, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}

	// Feed events to builder
	dec, err := stream.NewDecoder(bytes.NewReader(inputBuf.Bytes()), stream.WithWire())
	if err != nil {
		t.Fatalf("NewDecoder() error = %v", err)
	}

	for {
		ev, err := dec.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadEvent() error = %v", err)
		}
		if err := builder.WriteEvent(ev); err != nil {
			t.Fatalf("WriteEvent() error = %v", err)
		}
	}

	if err := builder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Open the snapshot
	readFile, err := os.Open(tmpFile.path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer readFile.Close()

	snapshot, err := Open(readFile)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer snapshot.Close()

	// Read a specific path
	node, err := snapshot.ReadPath("name")
	if err != nil {
		t.Fatalf("ReadPath('name') error = %v", err)
	}

	if node == nil {
		t.Fatalf("ReadPath('name') returned nil (expected to find name='alice')")
	}

	if node.Type != ir.StringType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.StringType)
	}

	if node.String != "alice" {
		t.Errorf("node.String = %q, want %q", node.String, "alice")
	}

	// Read another path
	node, err = snapshot.ReadPath("age")
	if err != nil {
		t.Fatalf("ReadPath('age') error = %v", err)
	}

	if node == nil {
		t.Fatal("ReadPath('age') returned nil")
	}

	if node.Type != ir.NumberType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.NumberType)
	}

	if node.Int64 == nil || *node.Int64 != 30 {
		var got int64
		if node.Int64 != nil {
			got = *node.Int64
		}
		t.Errorf("node.Int64 = %d, want %d", got, 30)
	}
	node, err = snapshot.ReadPath("z[0]")
	if err != nil {
		t.Fatalf("ReadPath('z[0]') error = %v", err)
	}

	if node == nil {
		t.Fatal("ReadPath('z[0]') returned nil")
	}

	if node.Type != ir.StringType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.StringType)
	}

	if node.String != "zoo" {
		t.Errorf("node.String = %q, want %q", node.String, "zoo")
	}

	// Read status path
	node, err = snapshot.ReadPath("status")
	if err != nil {
		t.Fatalf("ReadPath('status') error = %v", err)
	}

	if node == nil {
		t.Fatal("ReadPath('status') returned nil")
	}

	if node.Type != ir.StringType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.StringType)
	}

	if node.String != "active" {
		t.Errorf("node.String = %q, want %q", node.String, "active")
	}

	// Read entire z array
	node, err = snapshot.ReadPath("z")
	if err != nil {
		t.Fatalf("ReadPath('z') error = %v", err)
	}

	if node == nil {
		t.Fatal("ReadPath('z') returned nil")
	}

	if node.Type != ir.ArrayType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.ArrayType)
	}

	if len(node.Values) != 1 {
		t.Errorf("len(node.Values) = %d, want 1", len(node.Values))
	}

	if len(node.Values) > 0 {
		if node.Values[0].Type != ir.StringType {
			t.Errorf("node.Values[0].Type = %v, want %v", node.Values[0].Type, ir.StringType)
		}
		if node.Values[0].String != "zoo" {
			t.Errorf("node.Values[0].String = %q, want %q", node.Values[0].String, "zoo")
		}
	}

	// Test nonexistent path
	node, err = snapshot.ReadPath("nonexistent")
	if err != nil {
		t.Fatalf("ReadPath('nonexistent') error = %v", err)
	}

	if node != nil {
		t.Errorf("ReadPath('nonexistent') returned non-nil node: %+v", node)
	}
}

// testFile implements RC interface for testing
type testFile struct {
	*os.File
	path string
}

func newTestFile() (*testFile, error) {
	d, err := os.MkdirTemp(".", "snap-test-*")
	if err != nil {
		return nil, err
	}
	p := filepath.Join(d, "snap-test")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	return &testFile{
		File: f,
		path: p,
	}, nil
}
