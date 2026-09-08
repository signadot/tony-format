package storage

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// A store which will not open is a system that is down. Staging met exactly that: a log
// with a region no frame walk could cross, and behind it a record which would not
// deserialize -- "failed to initialize storage: failed to rebuild index" -- and docd
// never started (t96b5ejqh12krprjghn0).
//
// Refusing to open recovers nothing. The walk steps over what it cannot read and resumes
// at the next record it can, the store opens with everything it could read -- before the
// bad bytes and behind them -- and it says so for as long as it runs.
func TestStoreOpensOverAnUnreadableRecord(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	for i := 0; i < 40; i++ {
		subtreeWrite(t, s, "verse.entities.e"+strconv.Itoa(i), "{id: e"+strconv.Itoa(i)+"}")
	}
	commit, _ := s.GetCurrentCommit()
	before, err := readStateAt(s, "", commit, nil)
	if err != nil {
		t.Fatalf("read: %s", err)
	}
	if before == nil {
		t.Fatal("wrote nothing")
	}
	s.Close()

	// Corrupt a record's payload in the middle of the log, leaving its length prefix
	// intact -- which is what an interleaved write looks like to the walk.
	path := filepath.Join(dir, "logA")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %s", err)
	}
	mid := len(data) / 2
	for i := mid; i < mid+64 && i < len(data); i++ {
		data[i] = 0xEE
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write log: %s", err)
	}

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("a store with one bad record would not open: %s", err)
	}
	defer s2.Close()

	// And it does not pretend everything is fine.
	rep := s2.StatsReport()
	if _, said := rep["log.unreadable"]; !said {
		t.Errorf("the store opened over a bad record without reporting it: %v", rep)
	}

	// A write still lands, which is the difference between a degraded store and no
	// store at all.
	subtreeWrite(t, s2, "verse.entities.after", "{id: after}")

	// And it reads, because the index no longer points AT the bad record: the state it
	// serves is the state it can actually produce -- which includes what was written
	// behind the bad bytes, since the walk resumed there.
	commit, _ = s2.GetCurrentCommit()
	doc, err := readStateAt(s2, "", commit, nil)
	if err != nil {
		t.Fatalf("reading what survived: %s", err)
	}
	entities, _ := doc.GetKPath("verse.entities")
	if entities == nil || len(entities.Fields) < 39 {
		t.Errorf("the state holds %d entities of 40 written plus one after; one bad record costs one entity, not what followed it", len(entities.Fields))
	}
	if v, _ := doc.GetKPath("verse.entities.after"); v == nil {
		t.Errorf("the write after the reopen is missing")
	}
}
