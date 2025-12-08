package index

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

func TestBuild(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := dlog.NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	idx := NewIndex("")

	// Create some test entries
	entries := []*dlog.Entry{
		{
			Commit:     1,
			Timestamp:  "2024-01-01T00:00:00Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"foo": ir.FromString("bar")}),
			LastCommit: p64(0),
		},
		{
			Commit:     2,
			Timestamp:  "2024-01-01T00:00:01Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"baz": ir.FromInt(42)}),
			LastCommit: p64(1),
		},
		{
			Commit:    3,
			Timestamp: "2024-01-01T00:00:02Z",
			Patch: ir.FromMap(map[string]*ir.Node{
				"nested": ir.FromMap(map[string]*ir.Node{"key": ir.FromString("value")}),
			}),
			LastCommit: p64(2),
		},
	}

	// Write entries to dlog
	for _, entry := range entries {
		_, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Build index from dlog starting at commit 0
	if err := Build(idx, dl, -1); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Query for "foo" path
	// Entry has Commit=1, LastCommit=0, so segment should be [0, 1]
	segs := idx.LookupRange("foo", nil, nil)
	if len(segs) == 0 {
		t.Error("expected segments for 'foo' path")
	}
	found := false
	for _, seg := range segs {
		if seg.KindedPath == "foo" && seg.StartCommit == 0 && seg.EndCommit == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find segment for 'foo' with StartCommit=0, EndCommit=1")
	}

	// Query for "baz" path
	// Entry has Commit=2, LastCommit=1, so segment should be [1, 2]
	segs = idx.LookupRange("baz", nil, nil)
	if len(segs) == 0 {
		t.Error("expected segments for 'baz' path")
	}
	found = false
	for _, seg := range segs {
		if seg.KindedPath == "baz" && seg.StartCommit == 1 && seg.EndCommit == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find segment for 'baz' with StartCommit=1, EndCommit=2")
	}

	// Query for nested path
	// Entry has Commit=3, LastCommit=2, so segment should be [2, 3]
	segs = idx.LookupRange("nested.key", nil, nil)
	if len(segs) == 0 {
		t.Error("expected segments for 'nested.key' path")
	}
	found = false
	for _, seg := range segs {
		if seg.KindedPath == "nested.key" && seg.StartCommit == 2 && seg.EndCommit == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Error("did not find segment for 'nested.key' with StartCommit=2, EndCommit=3")
	}

	// Query all segments
	allSegs := idx.LookupRange("", nil, nil)
	if len(allSegs) < 3 {
		t.Errorf("expected at least 3 segments, got %d", len(allSegs))
	}
}

func TestBuild_Incremental(t *testing.T) {
	tmpDir := t.TempDir()

	dl, err := dlog.NewDLog(tmpDir, nil)
	if err != nil {
		t.Fatalf("NewDLog() error = %v", err)
	}
	defer dl.Close()

	idx := NewIndex("")

	// Create initial entries
	entries := []*dlog.Entry{
		{
			Commit:     1,
			Timestamp:  "2024-01-01T00:00:00Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"first": ir.FromString("value")}),
			LastCommit: p64(0),
		},
		{
			Commit:     2,
			Timestamp:  "2024-01-01T00:00:01Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"second": ir.FromString("value")}),
			LastCommit: p64(1),
		},
	}

	for _, entry := range entries {
		_, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Build index up to commit 2
	if err := Build(idx, dl, -1); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Add more entries
	moreEntries := []*dlog.Entry{
		{
			Commit:     3,
			Timestamp:  "2024-01-01T00:00:02Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"third": ir.FromString("value")}),
			LastCommit: p64(2),
		},
		{
			Commit:     4,
			Timestamp:  "2024-01-01T00:00:03Z",
			Patch:      ir.FromMap(map[string]*ir.Node{"fourth": ir.FromString("value")}),
			LastCommit: p64(3),
		},
	}

	for _, entry := range moreEntries {
		_, _, err := dl.AppendEntry(entry)
		if err != nil {
			t.Fatalf("AppendEntry() error = %v", err)
		}
	}

	// Build incrementally from commit 2
	if err := Build(idx, dl, 2); err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Verify all paths are indexed
	paths := []string{"first", "second", "third", "fourth"}
	for _, path := range paths {
		segs := idx.LookupRange(path, nil, nil)
		if len(segs) == 0 {
			t.Errorf("expected segments for path %q", path)
		}
	}
}

func p64(i int64) *int64 {
	return &i
}
