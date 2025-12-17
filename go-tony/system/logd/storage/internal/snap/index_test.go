package snap

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

func TestIndexSerialization(t *testing.T) {
	// Create an index with some entries
	idx := &Index{
		Entries: []IndexEntry{
			{
				Path:   mustParsePath(t, "a"),
				Offset: 0,
			},
			{
				Path:   mustParsePath(t, "a.b"),
				Offset: 100,
			},
			{
				Path:   mustParsePath(t, "a.b[0]"),
				Offset: 200,
			},
			{
				Path:   mustParsePath(t, "a.b[1]"),
				Offset: 300,
			},
			{
				Path:   mustParsePath(t, "c"),
				Offset: 400,
			},
		},
	}

	// Serialize to Tony format
	data, err := idx.ToTony()
	if err != nil {
		t.Fatalf("ToTony() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("ToTony() returned empty data")
	}

	// Deserialize from Tony format
	idx2 := &Index{}
	if err := idx2.FromTony(data); err != nil {
		t.Fatalf("FromTony() error = %v", err)
	}

	// Verify entries match
	if len(idx2.Entries) != len(idx.Entries) {
		t.Fatalf("FromTony() entries length = %d, want %d", len(idx2.Entries), len(idx.Entries))
	}

	for i, entry := range idx.Entries {
		got := idx2.Entries[i]
		if entry.Path == nil && got.Path == nil {
			continue
		}
		if entry.Path == nil || got.Path == nil {
			t.Errorf("Entry[%d]: Path nil mismatch", i)
			continue
		}
		if entry.Path.KPath.Compare(&got.Path.KPath) != 0 {
			t.Errorf("Entry[%d]: Path = %q, want %q", i, got.Path.String(), entry.Path.String())
		}
		if got.Offset != entry.Offset {
			t.Errorf("Entry[%d]: Offset = %d, want %d", i, got.Offset, entry.Offset)
		}
	}
}

func TestOpenIndex(t *testing.T) {
	// Create an index
	idx := &Index{
		Entries: []IndexEntry{
			{
				Path:   mustParsePath(t, "a"),
				Offset: 0,
			},
			{
				Path:   mustParsePath(t, "a.b"),
				Offset: 100,
			},
		},
	}

	// Serialize to bytes
	data, err := idx.ToTony()
	if err != nil {
		t.Fatalf("ToTony() error = %v", err)
	}

	// Open from reader
	reader := bytes.NewReader(data)
	idx2, err := OpenIndex(reader, len(data))
	if err != nil {
		t.Fatalf("OpenIndex() error = %v", err)
	}

	// Verify entries match
	if len(idx2.Entries) != len(idx.Entries) {
		t.Fatalf("OpenIndex() entries length = %d, want %d", len(idx2.Entries), len(idx.Entries))
	}

	for i, entry := range idx.Entries {
		got := idx2.Entries[i]
		if entry.Path.KPath.Compare(&got.Path.KPath) != 0 {
			t.Errorf("Entry[%d]: Path = %q, want %q", i, got.Path.String(), entry.Path.String())
		}
		if got.Offset != entry.Offset {
			t.Errorf("Entry[%d]: Offset = %d, want %d", i, got.Offset, entry.Offset)
		}
	}
}

func TestIndexLookup(t *testing.T) {
	// Create an index with entries in offset order (which should match kpath order)
	idx := &Index{
		Entries: []IndexEntry{
			{
				Path:   mustParsePath(t, "a"),
				Offset: 0,
			},
			{
				Path:   mustParsePath(t, "a.b"),
				Offset: 100,
			},
			{
				Path:   mustParsePath(t, "a.b[0]"),
				Offset: 200,
			},
			{
				Path:   mustParsePath(t, "a.b[1]"),
				Offset: 300,
			},
			{
				Path:   mustParsePath(t, "c"),
				Offset: 400,
			},
		},
	}

	tests := []struct {
		name       string
		kpath      string
		wantFound  bool
		wantExact  bool
		wantKPath  string
		wantOffset int64
	}{
		{
			name:       "find first entry",
			kpath:      "a",
			wantFound:  true,
			wantExact:  true,
			wantKPath:  "a",
			wantOffset: 0,
		},
		{
			name:       "find nested field",
			kpath:      "a.b",
			wantFound:  true,
			wantExact:  true,
			wantKPath:  "a.b",
			wantOffset: 100,
		},
		{
			name:       "find array element",
			kpath:      "a.b[0]",
			wantFound:  true,
			wantExact:  true,
			wantKPath:  "a.b[0]",
			wantOffset: 200,
		},
		{
			name:       "find second array element",
			kpath:      "a.b[1]",
			wantFound:  true,
			wantExact:  true,
			wantKPath:  "a.b[1]",
			wantOffset: 300,
		},
		{
			name:       "find later entry",
			kpath:      "c",
			wantFound:  true,
			wantExact:  true,
			wantKPath:  "c",
			wantOffset: 400,
		},
		{
			name:       "not found - returns max <= target",
			kpath:      "x",
			wantFound:  true,
			wantExact:  false,
			wantKPath:  "c", // "c" is the max element <= "x"
			wantOffset: 400,
		},
		{
			name:       "not found nested - returns max <= target",
			kpath:      "a.x",
			wantFound:  true,
			wantExact:  false,
			wantKPath:  "a.b[1]", // "a.b[1]" is the max element <= "a.x"
			wantOffset: 300,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i, err := idx.Lookup(tt.kpath)
			_ = err
			entry := &idx.Entries[i]

			if (entry != nil) != tt.wantFound {
				t.Errorf("Lookup(%q) found = %v, want %v", tt.kpath, entry != nil, tt.wantFound)
				return
			}
			p := entry.Path.KPath.String()
			exact := p == tt.kpath
			if entry != nil {
				if exact != tt.wantExact {
					t.Errorf("Lookup(%q) exact = %v, want %v", tt.kpath, exact, tt.wantExact)
				}
				if entry.Path.String() != tt.wantKPath {
					t.Errorf("Lookup(%q) Path = %q, want %q", tt.kpath, entry.Path.String(), tt.wantKPath)
				}
				if entry.Offset != tt.wantOffset {
					t.Errorf("Lookup(%q) Offset = %d, want %d", tt.kpath, entry.Offset, tt.wantOffset)
				}
			}
		})
	}
}

func TestIndexLookupBinarySearch(t *testing.T) {
	// Create a larger index to verify binary search works correctly
	idx := &Index{
		Entries: make([]IndexEntry, 0),
	}

	// Add entries in offset order (which should match kpath order)
	paths := []string{
		"a",
		"a.b",
		"a.b[0]",
		"a.b[1]",
		"a.b[2]",
		"a.c",
		"b",
		"b[0]",
		"b[1]",
		"c",
	}

	for i, path := range paths {
		idx.Entries = append(idx.Entries, IndexEntry{
			Path:   mustParsePath(t, path),
			Offset: int64(i * 100),
		})
	}

	// Test that we can find all entries
	for i, path := range paths {
		j, err := idx.Lookup(path)
		if err != nil {
			t.Fatal(err)
		}
		entry := &idx.Entries[j]
		if entry == nil {
			t.Errorf("Lookup(%q) returned nil, expected entry at offset %d", path, i*100)
			continue
		}
		p := idx.Entries[j].Path.KPath.String()
		exact := path == p
		if !exact {
			t.Errorf("Lookup(%q) exact = false, want true", path)
		}
		if entry.Offset != int64(i*100) {
			t.Errorf("Lookup(%q) Offset = %d, want %d", path, entry.Offset, i*100)
		}
	}

	// Test that non-existent paths return the maximum element <= target
	nonExistent := []struct {
		path       string
		wantBefore string // The path that should be returned (max <= target)
	}{
		{"x", "c"},            // "x" > "c", should return "c"
		{"a.x", "a.c"},        // "a.x" > "a.c", should return "a.c"
		{"a.b[99]", "a.b[2]"}, // "a.b[99]" > "a.b[2]", should return "a.b[2]"
		{"d", "c"},            // "d" > "c", should return "c"
		{"a.a", "a"},          // "a.a" > "a", should return "a" (not found, but "a" is max <= "a.a")
	}
	for _, tt := range nonExistent {
		j, err := idx.Lookup(tt.path)
		if err != nil {
			t.Fatal(err)
		}
		entry := &idx.Entries[j]
		p := entry.Path.KPath.String()
		exact := p == tt.path
		if exact {
			t.Errorf("Lookup(%q) exact = true, want false", tt.path)
		}
		if entry == nil {
			t.Errorf("Lookup(%q) returned nil, expected entry for %q", tt.path, tt.wantBefore)
			continue
		}
		if entry.Path.String() != tt.wantBefore {
			t.Errorf("Lookup(%q) returned %q, want %q", tt.path, entry.Path.String(), tt.wantBefore)
		}
	}
}

func mustParsePath(t *testing.T, kp string) *Path {
	t.Helper()
	p, err := kpath.Parse(kp)
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v", kp, err)
	}
	if p == nil {
		t.Fatalf("ParsePath(%q) returned nil", kp)
	}
	return &Path{*p}
}
