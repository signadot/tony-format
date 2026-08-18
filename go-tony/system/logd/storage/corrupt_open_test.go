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
// Whatever lies past a bad frame is unreachable however the store reacts, since framing
// from there on cannot be trusted. Refusing to open recovers none of it. So the walk
// stops, the store opens with what it could read, and it says so for as long as it runs.
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
	before, err := s.ReadStateAt("", commit, nil)
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

	// And it reads, because the index no longer points past the bad record: the state
	// it serves is the state it can actually produce.
	commit, _ = s2.GetCurrentCommit()
	if _, err := s2.ReadStateAt("", commit, nil); err != nil {
		t.Errorf("reading what survived: %s", err)
	}
}
