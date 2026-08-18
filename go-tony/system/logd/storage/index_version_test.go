package storage

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// An index.gob written before the tree fix parses perfectly and may still describe less
// than the log holds: the old tree dropped half a leaf when a duplicate insert met a
// full one, and a read served from a short index misses patches silently. So a file
// without the current version is not loaded -- it is rebuilt from the logs, which are
// the record. At fifty thousand commits that costs what loading the file costs
// (kds4sx3bh12krdrkghn0).
func TestPreFixIndexIsRebuiltFromTheLogs(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	for i := 0; i < 200; i++ {
		subtreeWrite(t, s, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
	}
	commit, _ := s.GetCurrentCommit()
	want, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	s.Close()

	// An index written the way the old code wrote it: no version, and -- as the old
	// tree could leave it -- missing most of what the log holds.
	indexPath := filepath.Join(dir, "index.gob")
	short := index.NewIndex("")
	short.Add(&index.LogSegment{StartCommit: 0, EndCommit: 1, KindedPath: "", LogFile: "A", LogPosition: 0})
	f, err := os.Create(indexPath)
	if err != nil {
		t.Fatalf("create: %s", err)
	}
	if err := gob.NewEncoder(f).Encode(index.IndexWithMetadata{
		Index:    short,
		Metadata: index.IndexMetadata{MaxCommit: commit}, // Version 0: written before this existed
	}); err != nil {
		t.Fatalf("encode: %s", err)
	}
	f.Close()

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %s", err)
	}
	commit2, _ := s2.GetCurrentCommit()
	got, err := s2.ReadStateAt("", commit2, nil)
	if err != nil {
		t.Fatalf("read after reopen: %s", err)
	}
	if got == nil || want == nil || !got.DeepEqual(want) {
		t.Errorf("the store came back with less than the log holds:\n got %s\nwant %s",
			mustEncode(t, got), mustEncode(t, want))
	}

	// And what it writes on the way out carries the version, so the next open trusts
	// it (Close persists the index).
	if err := s2.Close(); err != nil {
		t.Fatalf("close: %s", err)
	}
	_, meta, err := index.LoadIndexWithMeta(indexPath)
	if err != nil {
		t.Fatalf("reload: %s", err)
	}
	if meta.Version != index.IndexFormatVersion {
		t.Errorf("the index it wrote is version %d, want %d", meta.Version, index.IndexFormatVersion)
	}
}
