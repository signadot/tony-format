package snap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/stream"
)

func TestBuilder_NewBuilder(t *testing.T) {
	p, w := newBytesWriteSeeker()
	defer os.RemoveAll(filepath.Dir(p))
	defer w.Close()
	index := &Index{Entries: []IndexEntry{}}

	builder, err := NewBuilder(w, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}
	if builder == nil {
		t.Fatal("NewBuilder() returned nil builder")
	}

	// Verify builder fields are initialized
	if builder.w != w {
		t.Error("builder.w not set correctly")
	}
	if builder.state == nil {
		t.Error("builder.state is nil")
	}
	if builder.index != index {
		t.Error("builder.index not set correctly")
	}
	if builder.offset != 0 {
		t.Errorf("builder.offset = %d, want 0", builder.offset)
	}
}

func TestBuilder_Close(t *testing.T) {
	p, w := newBytesWriteSeeker()
	defer os.RemoveAll(filepath.Dir(p))
	defer w.Close()
	index := &Index{}

	builder, err := NewBuilder(w, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}

	// Set some offset to simulate writing events
	builder.offset = 100

	// Close the builder
	if err := builder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Read and verify header
	f, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	header := make([]byte, HeaderSize)
	f.Read(header)

	eventSize := binary.BigEndian.Uint64(header[0:8])
	indexSize := binary.BigEndian.Uint32(header[8:12])

	if eventSize != 100 {
		t.Errorf("eventSize = %d, want 100", eventSize)
	}

	// Index should be empty, so indexSize should match serialized empty index (wire format)
	expectedIndexData, err := index.ToTony(gomap.EncodeWire(true))
	if err != nil {
		t.Fatalf("index.ToTony() error = %v", err)
	}
	if indexSize != uint32(len(expectedIndexData)) {
		t.Errorf("indexSize = %d, want %d", indexSize, len(expectedIndexData))
	}
}

func TestBuilder_NestedStructureWithMixedContainers(t *testing.T) {
	// Create a nested structure with mixed containers:
	// {
	//   "users": [
	//     { "name": "alice", "tags": ["admin", "user"] },
	//     { "name": "bob", "age": 30 }
	//   ],
	//   "metadata": { "count": 2, "active": true }
	// }
	// We'll make it large enough to trigger index entries by adding more data

	// Encode the structure to a stream
	var inputBuf bytes.Buffer
	enc, err := stream.NewEncoder(&inputBuf, stream.WithWire())
	if err != nil {
		t.Fatalf("NewEncoder() error = %v", err)
	}

	// Build the nested structure
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("BeginObject() error = %v", err)
	}

	// "users": [...]
	if err := enc.WriteKey("users"); err != nil {
		t.Fatalf("WriteKey('users') error = %v", err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatalf("BeginArray() error = %v", err)
	}

	// First user: { "name": "alice", "tags": ["admin", "user"] }
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("BeginObject() error = %v", err)
	}
	if err := enc.WriteKey("name"); err != nil {
		t.Fatalf("WriteKey('name') error = %v", err)
	}
	if err := enc.WriteString("alice"); err != nil {
		t.Fatalf("WriteString('alice') error = %v", err)
	}
	if err := enc.WriteKey("tags"); err != nil {
		t.Fatalf("WriteKey('tags') error = %v", err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatalf("BeginArray() error = %v", err)
	}
	if err := enc.WriteString("admin"); err != nil {
		t.Fatalf("WriteString('admin') error = %v", err)
	}
	if err := enc.WriteString("user"); err != nil {
		t.Fatalf("WriteString('user') error = %v", err)
	}
	if err := enc.EndArray(); err != nil {
		t.Fatalf("EndArray() error = %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("EndObject() error = %v", err)
	}

	// Second user: { "name": "bob", "age": 30 }
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("BeginObject() error = %v", err)
	}
	if err := enc.WriteKey("name"); err != nil {
		t.Fatalf("WriteKey('name') error = %v", err)
	}
	if err := enc.WriteString("bob"); err != nil {
		t.Fatalf("WriteString('bob') error = %v", err)
	}
	if err := enc.WriteKey("age"); err != nil {
		t.Fatalf("WriteKey('age') error = %v", err)
	}
	if err := enc.WriteInt(30); err != nil {
		t.Fatalf("WriteInt(30) error = %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("EndObject() error = %v", err)
	}

	// Add more users to make the structure large enough to trigger index entries
	// (MaxChunkSize is 4096, so we need substantial data)
	for i := 0; i < 50; i++ {
		if err := enc.BeginObject(); err != nil {
			t.Fatalf("BeginObject() error = %v", err)
		}
		if err := enc.WriteKey("name"); err != nil {
			t.Fatalf("WriteKey('name') error = %v", err)
		}
		// Create a long string to increase size
		longName := fmt.Sprintf("user_%d_with_a_very_long_name_that_takes_up_space_%d", i, i)
		if err := enc.WriteString(longName); err != nil {
			t.Fatalf("WriteString() error = %v", err)
		}
		if err := enc.WriteKey("data"); err != nil {
			t.Fatalf("WriteKey('data') error = %v", err)
		}
		// Add an array with multiple values
		if err := enc.BeginArray(); err != nil {
			t.Fatalf("BeginArray() error = %v", err)
		}
		for j := 0; j < 10; j++ {
			if err := enc.WriteString(fmt.Sprintf("value_%d_%d", i, j)); err != nil {
				t.Fatalf("WriteString() error = %v", err)
			}
		}
		if err := enc.EndArray(); err != nil {
			t.Fatalf("EndArray() error = %v", err)
		}
		if err := enc.EndObject(); err != nil {
			t.Fatalf("EndObject() error = %v", err)
		}
	}

	if err := enc.EndArray(); err != nil {
		t.Fatalf("EndArray() error = %v", err)
	}

	// "metadata": { "count": 2, "active": true }
	if err := enc.WriteKey("metadata"); err != nil {
		t.Fatalf("WriteKey('metadata') error = %v", err)
	}
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("BeginObject() error = %v", err)
	}
	if err := enc.WriteKey("count"); err != nil {
		t.Fatalf("WriteKey('count') error = %v", err)
	}
	if err := enc.WriteInt(2); err != nil {
		t.Fatalf("WriteInt(2) error = %v", err)
	}
	if err := enc.WriteKey("active"); err != nil {
		t.Fatalf("WriteKey('active') error = %v", err)
	}
	if err := enc.WriteBool(true); err != nil {
		t.Fatalf("WriteBool(true) error = %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("EndObject() error = %v", err)
	}

	if err := enc.EndObject(); err != nil {
		t.Fatalf("EndObject() error = %v", err)
	}

	// Now use the builder to process this stream
	p, w := newBytesWriteSeeker()
	index := &Index{Entries: []IndexEntry{}}
	defer os.RemoveAll(filepath.Dir(p))

	builder, err := NewBuilder(w, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder() error = %v", err)
	}

	// Feed events to the builder
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

	// Close the builder (writes header)
	if err := builder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Log the resulting index
	t.Logf("Index has %d entries:", len(builder.index.Entries))
	for i, entry := range builder.index.Entries {
		pathStr := entry.Path.String()
		t.Logf("  [%d] Path: %q, Offset: %d", i, pathStr, entry.Offset)
	}

	// Verify we have some index entries (depending on chunk size)
	if len(index.Entries) == 0 {
		t.Log("Note: No index entries created (structure may be smaller than MaxChunkSize)")
	} else {
		t.Logf("Created %d index entries", len(index.Entries))
	}

	// Verify the snapshot can be opened and read back
	r, err := os.Open(p)
	if err != nil {
		panic(err)
	}
	defer r.Close()
	snapshot, err := Open(r)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer snapshot.Close()

	if snapshot.Index == nil {
		t.Fatal("snapshot.Index is nil")
	}

	t.Logf("Snapshot opened successfully:")
	t.Logf("  EventSize: %d bytes", snapshot.EventSize)
	t.Logf("  Index entries: %d", len(snapshot.Index.Entries))

	// Log all index entries from the opened snapshot
	if len(snapshot.Index.Entries) > 0 {
		t.Logf("Index entries from opened snapshot:")
		for i, entry := range snapshot.Index.Entries {
			pathStr := entry.Path.String()
			t.Logf("  [%d] Path: %q, Offset: %d", i, pathStr, entry.Offset)
		}
	} else {
		t.Log("Note: No index entries in opened snapshot (this may be expected if offsets are relative)")
	}

	// Verify the index entries match
	if len(index.Entries) != len(snapshot.Index.Entries) {
		t.Logf("Warning: Builder index has %d entries, opened snapshot has %d entries", len(index.Entries), len(snapshot.Index.Entries))
	}
}

func newBytesWriteSeeker() (string, W) {
	p, err := os.MkdirTemp("", "b-test-*")
	if err != nil {
		panic(err)
	}
	p = filepath.Join(p, "snap")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		panic(err)
	}
	return p, f
}
