package snap

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/stream"
)

// TestSnapshotLookupAndRead tests lookup and read operations across different
// container kinds, nesting levels, and chunk boundaries.
func TestSnapshotLookupAndRead(t *testing.T) {

	// Use a small chunk size for testing chunk boundaries
	originalEnv := os.Getenv("SNAP_MAX_CHUNK_SIZE")
	defer os.Setenv("SNAP_MAX_CHUNK_SIZE", originalEnv)
	os.Setenv("SNAP_MAX_CHUNK_SIZE", "256") // Small chunk size to trigger multiple chunks

	tests := []struct {
		name        string
		buildDoc    func(*stream.Encoder) error
		testPaths   []pathTest
		description string
		keepFile    bool // If true, don't remove the snapshot file (for debugging)
	}{
		{
			name: "simple_object",
			buildDoc: func(enc *stream.Encoder) error {
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("name"); err != nil {
					return err
				}
				if err := enc.WriteString("test"); err != nil {
					return err
				}
				if err := enc.WriteKey("value"); err != nil {
					return err
				}
				if err := enc.WriteInt(42); err != nil {
					return err
				}
				return enc.EndObject()
			},
			testPaths: []pathTest{
				{path: "name", wantFound: true, wantType: ir.StringType, wantValue: "test"},
				{path: "value", wantFound: true, wantType: ir.NumberType, wantValue: int64(42)},
				{path: "nonexistent", wantFound: false},
			},
			description: "Simple object with string and int fields",
		},
		{
			name: "nested_objects",
			buildDoc: func(enc *stream.Encoder) error {
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("level1"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("level2"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("level3"); err != nil {
					return err
				}
				if err := enc.WriteString("deep"); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				return enc.EndObject()
			},
			testPaths: []pathTest{
				{path: "level1", wantFound: true, wantType: ir.ObjectType},
				{path: "level1.level2", wantFound: true, wantType: ir.ObjectType},
				{path: "level1.level2.level3", wantFound: true, wantType: ir.StringType, wantValue: "deep"},
				{path: "level1.level2.nonexistent", wantFound: false},
			},
			description: "Deeply nested objects",
		},
		{
			name: "simple_array",
			buildDoc: func(enc *stream.Encoder) error {
				if err := enc.BeginArray(); err != nil {
					return err
				}
				if err := enc.WriteString("first"); err != nil {
					return err
				}
				if err := enc.WriteString("second"); err != nil {
					return err
				}
				if err := enc.WriteInt(3); err != nil {
					return err
				}
				return enc.EndArray()
			},
			testPaths: []pathTest{
				{path: "[0]", wantFound: true, wantType: ir.StringType, wantValue: "first"},
				{path: "[1]", wantFound: true, wantType: ir.StringType, wantValue: "second"},
				{path: "[2]", wantFound: true, wantType: ir.NumberType, wantValue: int64(3)},
				{path: "[3]", wantFound: false},
			},
			description: "Simple array with mixed types",
		},
		{
			name: "nested_arrays",
			buildDoc: func(enc *stream.Encoder) error {
				if err := enc.BeginArray(); err != nil {
					return err
				}
				// First element: array
				if err := enc.BeginArray(); err != nil {
					return err
				}
				if err := enc.WriteInt(1); err != nil {
					return err
				}
				if err := enc.WriteInt(2); err != nil {
					return err
				}
				if err := enc.EndArray(); err != nil {
					return err
				}
				// Second element: array
				if err := enc.BeginArray(); err != nil {
					return err
				}
				if err := enc.WriteString("a"); err != nil {
					return err
				}
				if err := enc.WriteString("b"); err != nil {
					return err
				}
				if err := enc.EndArray(); err != nil {
					return err
				}
				return enc.EndArray()
			},
			testPaths: []pathTest{
				{path: "[0]", wantFound: true, wantType: ir.ArrayType},
				{path: "[0][0]", wantFound: true, wantType: ir.NumberType, wantValue: int64(1)},
				{path: "[0][1]", wantFound: true, wantType: ir.NumberType, wantValue: int64(2)},
				{path: "[1][0]", wantFound: true, wantType: ir.StringType, wantValue: "a"},
				{path: "[1][1]", wantFound: true, wantType: ir.StringType, wantValue: "b"},
			},
			description: "Nested arrays",
		},
		{
			name: "mixed_containers",
			buildDoc: func(enc *stream.Encoder) error {
				if err := enc.BeginObject(); err != nil {
					return err
				}
				// "metadata": object
				if err := enc.WriteKey("metadata"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("count"); err != nil {
					return err
				}
				if err := enc.WriteInt(2); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				// "users": array of objects
				if err := enc.WriteKey("users"); err != nil {
					return err
				}
				if err := enc.BeginArray(); err != nil {
					return err
				}
				// First user
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("name"); err != nil {
					return err
				}
				if err := enc.WriteString("alice"); err != nil {
					return err
				}
				if err := enc.WriteKey("tags"); err != nil {
					return err
				}
				if err := enc.BeginArray(); err != nil {
					return err
				}
				if err := enc.WriteString("admin"); err != nil {
					return err
				}
				if err := enc.WriteString("user"); err != nil {
					return err
				}
				if err := enc.EndArray(); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				// Second user
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("age"); err != nil {
					return err
				}
				if err := enc.WriteInt(30); err != nil {
					return err
				}
				if err := enc.WriteKey("name"); err != nil {
					return err
				}
				if err := enc.WriteString("bob"); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				if err := enc.EndArray(); err != nil {
					return err
				}
				return enc.EndObject()
			},
			testPaths: []pathTest{
				{path: "metadata.count", wantFound: true, wantType: ir.NumberType, wantValue: int64(2)},
				{path: "users", wantFound: true, wantType: ir.ArrayType},
				{path: "users[0]", wantFound: true, wantType: ir.ObjectType},
				{path: "users[0].name", wantFound: true, wantType: ir.StringType, wantValue: "alice"},
				{path: "users[0].tags", wantFound: true, wantType: ir.ArrayType},
				{path: "users[0].tags[0]", wantFound: true, wantType: ir.StringType, wantValue: "admin"},
				{path: "users[1].age", wantFound: true, wantType: ir.NumberType, wantValue: int64(30)},
				{path: "users[1].name", wantFound: true, wantType: ir.StringType, wantValue: "bob"},
			},
			description: "Mixed containers: objects containing arrays containing objects",
		},
		{
			name: "large_document_chunk_boundaries",
			buildDoc: func(enc *stream.Encoder) error {
				// Create a large document that will span multiple chunks
				if err := enc.BeginObject(); err != nil {
					return err
				}
				// Add many fields with long values to trigger chunk boundaries
				// Insert in sorted order (alphabetical) as document order should be sorted
				keys := make([]string, 20)
				for i := 0; i < 20; i++ {
					keys[i] = makeLongKey(i)
				}
				sort.Strings(keys) // Sort keys alphabetically
				// Write keys in sorted order
				for _, key := range keys {
					if err := enc.WriteKey(key); err != nil {
						return err
					}
					// Extract index from key for value
					var idx int
					fmt.Sscanf(key, "field_%d_with_long_name_that_should_trigger_chunks", &idx)
					longValue := makeLongString(idx)
					if err := enc.WriteString(longValue); err != nil {
						return err
					}
				}
				return enc.EndObject()
			},
			testPaths: []pathTest{
				{path: makeLongKey(0), wantFound: true, wantType: ir.StringType},
				{path: makeLongKey(5), wantFound: true, wantType: ir.StringType},
				{path: makeLongKey(10), wantFound: true, wantType: ir.StringType},
				{path: makeLongKey(19), wantFound: true, wantType: ir.StringType},
			},
			description: "Large document that spans chunk boundaries",
			keepFile:    false, // Keep file for inspection
		},
		{
			name: "deep_nesting_chunk_boundaries",
			buildDoc: func(enc *stream.Encoder) error {
				// Create deeply nested structure with large values to trigger chunks
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("level1"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				// Add large values at each level
				for i := 0; i < 5; i++ {
					if err := enc.WriteKey(makeLongKey(i)); err != nil {
						return err
					}
					if err := enc.WriteString(makeLongString(i)); err != nil {
						return err
					}
				}
				if err := enc.WriteKey("level2"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				for i := 0; i < 5; i++ {
					if err := enc.WriteKey(makeLongKey(i)); err != nil {
						return err
					}
					if err := enc.WriteString(makeLongString(i)); err != nil {
						return err
					}
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				return enc.EndObject()
			},
			testPaths: []pathTest{
				{path: "level1", wantFound: true, wantType: ir.ObjectType},
				{path: "level1.level2", wantFound: true, wantType: ir.ObjectType},
				{path: "level1.level2.field_0_with_long_name_that_should_trigger_chunks", wantFound: true, wantType: ir.StringType},
			},
			description: "Deep nesting with chunk boundaries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the document
			var inputBuf bytes.Buffer
			enc, err := stream.NewEncoder(&inputBuf, stream.WithWire())
			if err != nil {
				t.Fatalf("NewEncoder() error = %v", err)
			}

			if err := tt.buildDoc(enc); err != nil {
				t.Fatalf("buildDoc() error = %v", err)
			}

			// Build snapshot
			p, w := newBytesWriteSeeker()
			if !tt.keepFile {
				defer os.RemoveAll(filepath.Dir(p))
			} else {
				t.Logf("KEEPING snapshot file for inspection: %s", p)
			}
			defer w.Close()
			index := &Index{Entries: []IndexEntry{}}

			builder, err := NewBuilder(w, index, nil)
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

			// Log index entries for debugging
			t.Logf("Index has %d entries:", len(index.Entries))
			for i, entry := range index.Entries {
				t.Logf("  [%d] Path: %q, Offset: %d", i, entry.Path.String(), entry.Offset)
			}

			// Open snapshot
			r, err := os.Open(p)
			if err != nil {
				t.Fatalf("Open file error = %v", err)
			}
			defer r.Close()

			snapshot, err := Open(r)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer snapshot.Close()

			// Test lookups and reads for each path
			for _, pathTest := range tt.testPaths {
				t.Run(pathTest.path, func(t *testing.T) {
					// Test lookup (may return nil for small documents that don't have index entries)
					j, err := snapshot.Index.Lookup(pathTest.path)
					if err != nil {
						t.Fatalf("Lookup(%q) error = %v", pathTest.path, err)
					}
					if j >= len(snapshot.Index.Entries) {
						t.Fatalf("index lookup out of bounds %d/%d", j, len(snapshot.Index.Entries))
					}
					entry := &snapshot.Index.Entries[j]
					exact := entry.Path.String() == pathTest.path

					hasIndexEntries := len(snapshot.Index.Entries) > 0
					if pathTest.wantFound && hasIndexEntries {
						// Only check lookup if we have index entries
						if entry == nil {
							t.Logf("Lookup(%q) returned nil (no index entries for this path, but document is small)", pathTest.path)
						} else {
							t.Logf("Lookup(%q): exact=%v, offset=%d", pathTest.path, exact, entry.Offset)
						}
					} else if !pathTest.wantFound {
						// For paths that don't exist, lookup may return an ancestor
						// or nil, both are acceptable
						if entry != nil {
							t.Logf("Lookup(%q) returned ancestor: %q at offset %d", pathTest.path, entry.Path.String(), entry.Offset)
						}
					}

					// Test ReadPath
					node, err := snapshot.ReadPath(pathTest.path)
					if err != nil {
						t.Fatalf("ReadPath(%q) error = %v", pathTest.path, err)
					}

					if pathTest.wantFound {
						if node == nil {
							t.Errorf("ReadPath(%q) returned nil, expected node", pathTest.path)
							return
						}

						if node.Type != pathTest.wantType {
							t.Errorf("ReadPath(%q) Type = %v, want %v", pathTest.path, node.Type, pathTest.wantType)
						}

						if pathTest.wantValue != nil {
							switch pathTest.wantType {
							case ir.StringType:
								if node.String != pathTest.wantValue {
									t.Errorf("ReadPath(%q) String = %q, want %q", pathTest.path, node.String, pathTest.wantValue)
								}
							case ir.NumberType:
								if node.Int64 == nil || *node.Int64 != pathTest.wantValue {
									var got int64
									if node.Int64 != nil {
										got = *node.Int64
									}
									t.Errorf("ReadPath(%q) Int64 = %d, want %d", pathTest.path, got, pathTest.wantValue)
								}
							}
						}
					} else {
						if node != nil {
							t.Errorf("ReadPath(%q) returned node, expected nil", pathTest.path)
						}
					}
				})
			}
		})
	}
}

type pathTest struct {
	path      string
	wantFound bool
	wantType  ir.Type
	wantValue interface{}
}

// Helper functions for creating test data

func makeLongKey(i int) string {
	return fmt.Sprintf("field_%d_with_long_name_that_should_trigger_chunks", i)
}

func makeLongString(i int) string {
	// Create a string that's ~50 bytes
	return fmt.Sprintf("value_%d_with_sufficient_length_to_trigger_chunk_boundaries_", i)
}
