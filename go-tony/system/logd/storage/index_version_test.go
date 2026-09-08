package storage

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
)

// An index written by another version of this code parses perfectly and may still
// describe less than the log holds -- the old tree dropped half a leaf when a duplicate
// insert met a full one, and a read served from a short index misses patches silently. So
// a manifest without the current version is not loaded: the store rebuilds from the logs,
// which are the record, and what it writes on the way out carries the version.
func TestAnIndexOfAnotherVersionIsRebuiltFromTheLogs(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	for i := 0; i < 200; i++ {
		subtreeWrite(t, s, "verse.entities.e"+strconv.Itoa(i), "{n: "+strconv.Itoa(i)+"}")
	}
	commit, _ := s.GetCurrentCommit()
	want, err := readStateAt(s, "", commit, nil)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	s.Close()

	// A manifest of the previous version, describing one region that is not there.
	f, err := os.Create(filepath.Join(dir, "index.manifest"))
	if err != nil {
		t.Fatalf("create: %s", err)
	}
	if err := gob.NewEncoder(f).Encode(index.Manifest{Version: index.IndexFormatVersion - 1, MaxCommit: commit}); err != nil {
		t.Fatalf("encode: %s", err)
	}
	f.Close()

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %s", err)
	}
	commit2, _ := s2.GetCurrentCommit()
	got, err := readStateAt(s2, "", commit2, nil)
	if err != nil {
		t.Fatalf("read after reopen: %s", err)
	}
	if got == nil || want == nil || !got.DeepEqual(want) {
		t.Errorf("the store came back with less than the log holds:\n got %s\nwant %s",
			mustEncode(t, got), mustEncode(t, want))
	}
	if err := s2.Close(); err != nil {
		t.Fatalf("close: %s", err)
	}
	f, err = os.Open(filepath.Join(dir, "index.manifest"))
	if err != nil {
		t.Fatalf("reload: %s", err)
	}
	defer f.Close()
	var m index.Manifest
	if err := gob.NewDecoder(f).Decode(&m); err != nil {
		t.Fatalf("decode: %s", err)
	}
	if m.Version != index.IndexFormatVersion {
		t.Errorf("the index it wrote is version %d, want %d", m.Version, index.IndexFormatVersion)
	}
}
