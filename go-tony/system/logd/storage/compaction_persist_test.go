package storage

import (
	"testing"
	"time"
)

// A manifest written between a compaction's swap and the end of its re-index names the
// file's new generation over the survivors' old positions, and the next open trusts it:
// every read then fails "read interrupted by compaction", so every write's verify read
// fails, nothing commits, and nothing repairs it (05d8w3cjh12kswb1msn0, item 5). A
// persist that arrives in that window waits for the re-index to end.
func TestCompact_PersistDuringReindexWaitsForIt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	casWrite(t, s, `{k: "old", other: "x"}`) // beyond the cutoff below: what makes the compaction a rewrite
	time.Sleep(1200 * time.Millisecond)      // entry times are whole seconds
	for i := 0; i < 5; i++ {
		casWrite(t, s, `{k: "new", n: `+string(rune('0'+i))+`}`) // within it: survivors, re-indexed where the rewrite puts them
	}
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("SwitchDLog: %v", err)
	}
	want := readAll(t, s)

	persisted := make(chan error, 1)
	s.afterCompactSwap = func() {
		go func() { persisted <- s.indexPersister.Persist() }()
		time.Sleep(100 * time.Millisecond) // long enough for a persist that does not wait to land
	}
	if err := s.Compact(cutoffOf(time.Second)); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if err := <-persisted; err != nil {
		t.Fatalf("persist: %v", err)
	}
	// No Close: the manifest the persist wrote is what the next open trusts.

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	got := readAll(t, s2)
	if !got.DeepEqual(want) {
		t.Fatalf("after reopen on the manifest a persist wrote during compaction:\n want %v\n got  %v", want, got)
	}
	casWrite(t, s2, `{k: "after"}`)
}
