package storage

import (
	"fmt"
	"os"
	"path/filepath"
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

// A persist of a region written to since its last record appends the region again and
// leaves the old record, so a store that stays open and keeps writing grows the regions
// file without bound; a compaction's re-index dirties every survivor's region, which on
// staging appended an index of the survivors a cycle (1xa86mrnh12krzswvdn0). The
// persister rewrites the file once its garbage outgrows the current records, and the store
// reads the same after the rewrites, open and reopened on the rewritten file.
func TestPersist_RewritesTheRegionsFileWhenGarbageOutgrowsIt(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	regionsSize := func() int64 {
		st, err := os.Stat(filepath.Join(dir, "index.regions"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		return st.Size()
	}
	rewrites := 0
	for cycle := 0; cycle < 12; cycle++ {
		for i := 0; i < 20; i++ {
			casWrite(t, s, fmt.Sprintf(`{k: "c%dw%d", n: %d}`, cycle, i, i)) // the same regions, dirty again
		}
		before := regionsSize()
		if err := s.indexPersister.Persist(); err != nil {
			t.Fatalf("persist %d: %v", cycle, err)
		}
		garbage, live := s.index.RegionsGarbage()
		if garbage > live {
			t.Fatalf("persist %d left %d bytes of garbage beside %d of records", cycle, garbage, live)
		}
		if after := regionsSize(); after != garbage+live {
			t.Fatalf("persist %d: the file is %d bytes, the index says %d garbage + %d live", cycle, after, garbage, live)
		} else if after < before {
			rewrites++
		}
	}
	if rewrites == 0 {
		t.Fatalf("12 persists of dirty regions and the file never shrank")
	}
	want := readAll(t, s)
	// No Close: the file the open store rewrote is what the next open pages from.

	s2, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got := readAll(t, s2); !got.DeepEqual(want) {
		t.Fatalf("after reopen on the rewritten regions file:\n want %v\n got  %v", want, got)
	}
	casWrite(t, s2, `{k: "after"}`)
}
