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

func TestKPathStateArrayInitialization(t *testing.T) {
	// Test KPathState behavior for array indices and compare with actual stream processing
	// KPathState positions state ONE BEFORE the target for leaf array elements

	// Test 1: Verify KPathState creates correct state
	state, err := stream.KPathState("users[3]")
	if err != nil {
		t.Fatalf("KPathState error = %v", err)
	}

	// For leaf array elements, KPathState positions at n-1
	if state.CurrentPath() != "users[2]" {
		t.Errorf("KPathState('users[3]').CurrentPath() = %q, want %q (positioned one before)", state.CurrentPath(), "users[2]")
	}

	// Test 2: Create actual document with array and process events to users[3]
	// Document: {users: ["a", "b", "c", "d", "e", "f"]}
	var buf bytes.Buffer
	enc, err := stream.NewEncoder(&buf, stream.WithWire())
	if err != nil {
		t.Fatalf("NewEncoder error = %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteKey("users"); err != nil {
		t.Fatal(err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatal(err)
	}
	for _, val := range []string{"a", "b", "c", "d", "e", "f"} {
		if err := enc.WriteString(val); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.EndArray(); err != nil {
		t.Fatal(err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatal(err)
	}

	// Process events up to and including users[3]
	dec, err := stream.NewDecoder(bytes.NewReader(buf.Bytes()), stream.WithWire())
	if err != nil {
		t.Fatalf("NewDecoder error = %v", err)
	}

	actualState := stream.NewState()
	eventCount := 0
	var pathBeforeElement3, pathAfterElement3 string

	for {
		ev, err := dec.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadEvent error = %v", err)
		}

		currentPath := actualState.CurrentPath()

		// Log each event for debugging
		t.Logf("Event %d: Type=%v, Path before ProcessEvent=%q", eventCount, ev.Type, currentPath)

		// Capture path right before we would process the event that makes us users[3]
		if currentPath == "users[2]" && ev.Type != stream.EventEndArray && ev.Type != stream.EventEndObject {
			pathBeforeElement3 = currentPath
			t.Logf("  -> Capturing pathBeforeElement3 = %q (about to process next leaf)", pathBeforeElement3)
		}

		if err := actualState.ProcessEvent(ev); err != nil {
			t.Fatalf("ProcessEvent error = %v", err)
		}

		currentPath = actualState.CurrentPath()
		t.Logf("  -> Path after ProcessEvent=%q", currentPath)

		// Capture path right after we reach users[3]
		if pathBeforeElement3 != "" && pathAfterElement3 == "" && currentPath == "users[3]" {
			pathAfterElement3 = currentPath
			t.Logf("  -> Capturing pathAfterElement3 = %q", pathAfterElement3)
			// Continue for one more event to see what happens
			eventCount++
			continue
		}

		// Stop after we've captured both paths
		if pathAfterElement3 != "" {
			break
		}

		eventCount++
	}

	t.Logf("Summary:")
	t.Logf("  Path before processing event that creates users[3]: %q", pathBeforeElement3)
	t.Logf("  Path after processing event that creates users[3]: %q", pathAfterElement3)
	t.Logf("  KPathState('users[3]').CurrentPath(): %q", state.CurrentPath())

	// Compare: Does KPathState match the state BEFORE or AFTER processing the event?
	if state.CurrentPath() == pathBeforeElement3 {
		t.Logf("KPathState matches state BEFORE processing event at users[3]")
	} else if state.CurrentPath() == pathAfterElement3 {
		t.Logf("KPathState matches state AFTER processing event at users[3]")
	} else {
		t.Errorf("KPathState doesn't match either before (%q) or after (%q) state", pathBeforeElement3, pathAfterElement3)
	}
}

func TestPathFinderWithArrayIndex(t *testing.T) {
	// Test PathFinder when index entry is at an array position
	// Document: {users: ["a", "b", "c", "d", "e", "f"]}

	var buf bytes.Buffer
	enc, err := stream.NewEncoder(&buf, stream.WithWire())
	if err != nil {
		t.Fatalf("NewEncoder error = %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteKey("users"); err != nil {
		t.Fatal(err)
	}
	if err := enc.BeginArray(); err != nil {
		t.Fatal(err)
	}
	for _, val := range []string{"a", "b", "c", "d", "e", "f"} {
		if err := enc.WriteString(val); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.EndArray(); err != nil {
		t.Fatal(err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatal(err)
	}

	// Create snapshot file with manually crafted index entry at users[3]
	tmpFile, err := newTestFile()
	if err != nil {
		t.Fatalf("newTestFile() error = %v", err)
	}
	defer os.RemoveAll(filepath.Dir(tmpFile.path))

	// Build snapshot using Builder (which properly formats events with newlines)
	index := &Index{Entries: []IndexEntry{}}
	builder, err := NewBuilder(tmpFile, index, nil)
	if err != nil {
		t.Fatalf("NewBuilder error = %v", err)
	}

	// Feed events to builder and track offset at event 6
	dec, err := stream.NewDecoder(bytes.NewReader(buf.Bytes()), stream.WithWire())
	if err != nil {
		t.Fatalf("NewDecoder error = %v", err)
	}

	eventNum := 0
	var offsetToUsers3 int64
	for {
		ev, err := dec.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("ReadEvent error = %v", err)
		}

		// Capture offset before writing event 6 (users[3])
		if eventNum == 6 {
			offsetToUsers3 = builder.offset
			t.Logf("Offset to users[3] (event #%d): %d", eventNum, offsetToUsers3)
		}

		if err := builder.WriteEvent(ev); err != nil {
			t.Fatalf("WriteEvent error = %v", err)
		}
		eventNum++
	}

	// Don't call builder.Close() yet - manually finish writing
	// Add custom index entry for users[3]
	index.Entries = append(index.Entries, IndexEntry{
		Path:   mustParsePath(t, "users[3]"),
		Offset: offsetToUsers3,
	})

	// Write index
	indexData, err := index.ToTony()
	if err != nil {
		t.Fatalf("ToTony error = %v", err)
	}
	if _, err := tmpFile.Write(indexData); err != nil {
		t.Fatalf("Write index error = %v", err)
	}

	// Update header
	if _, err := tmpFile.Seek(builder.origOffset, io.SeekStart); err != nil {
		t.Fatalf("Seek to header error = %v", err)
	}
	header := make([]byte, 12)
	binary.BigEndian.PutUint64(header[0:8], uint64(builder.offset))
	binary.BigEndian.PutUint32(header[8:12], uint32(len(indexData)))
	if _, err := tmpFile.Write(header); err != nil {
		t.Fatalf("Write header error = %v", err)
	}

	// Now close
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Close error = %v", err)
	}

	// Open snapshot and try to read users[3]
	readFile, err := os.Open(tmpFile.path)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	defer readFile.Close()

	snapshot, err := Open(readFile)
	if err != nil {
		t.Fatalf("Open error = %v", err)
	}
	defer snapshot.Close()

	// Read users[3] - should return "d"
	node, err := snapshot.ReadPath("users[3]")
	if err != nil {
		t.Fatalf("ReadPath('users[3]') error = %v", err)
	}

	if node == nil {
		t.Fatalf("ReadPath('users[3]') returned nil")
	}

	if node.Type != ir.StringType {
		t.Errorf("node.Type = %v, want %v", node.Type, ir.StringType)
	}

	if node.String != "d" {
		t.Errorf("ReadPath('users[3]') returned %q, want %q", node.String, "d")
	}

	// Also test reading users[4] and users[5] to see if index helps
	node, err = snapshot.ReadPath("users[4]")
	if err != nil {
		t.Fatalf("ReadPath('users[4]') error = %v", err)
	}
	if node == nil {
		t.Fatalf("ReadPath('users[4]') returned nil")
	}
	if node.String != "e" {
		t.Errorf("ReadPath('users[4]') returned %q, want %q", node.String, "e")
	}
}
