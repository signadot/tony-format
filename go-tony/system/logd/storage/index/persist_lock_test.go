package index

import (
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// Persisting the index used to hold the root's lock across the whole encode -- and a
// write takes that lock to Add a segment, so every write during a persist waited for
// the entire index to be serialized. On a store of tens of thousands of commits that is
// seconds, it recurs every persist interval, and it falls on whichever write is in
// flight: unrelated clients all reporting "context deadline exceeded" at the same
// moment, on paths with nothing wrong with them (v552mdbqh12kr7dtgdn0).
//
// A write must not wait for a persist.
func TestAddDoesNotWaitForPersist(t *testing.T) {
	idx := NewIndex("")
	const paths, revs = 2000, 20
	for p := 0; p < paths; p++ {
		kp := "verse.entities.e" + strconv.Itoa(p)
		for r := 0; r < revs; r++ {
			c := int64(p*revs + r)
			idx.Add(&LogSegment{
				StartCommit: c, EndCommit: c, KindedPath: kp,
				LogFile: "A", LogPosition: c * 64,
			})
		}
	}

	// What a persist of this index costs, so the bound below is measured, not guessed.
	path := filepath.Join(t.TempDir(), "index.gob")
	start := time.Now()
	if err := StoreIndexWithMetadata(path, idx, int64(paths*revs)); err != nil {
		t.Fatalf("persist: %s", err)
	}
	persistTook := time.Since(start)
	if persistTook < 10*time.Millisecond {
		t.Skipf("a persist of this index takes %s; too quick here to tell a stall from noise", persistTook)
	}

	// Now write while it persists.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := StoreIndexWithMetadata(path, idx, int64(paths*revs)); err != nil {
			t.Errorf("persist: %s", err)
		}
	}()

	worst := time.Duration(0)
	deadline := time.Now().Add(persistTook)
	for i := 0; time.Now().Before(deadline); i++ {
		c := int64(1_000_000 + i)
		start := time.Now()
		idx.Add(&LogSegment{
			StartCommit: c, EndCommit: c, KindedPath: "verse.meta.rev",
			LogFile: "A", LogPosition: c,
		})
		if took := time.Since(start); took > worst {
			worst = took
		}
		time.Sleep(time.Millisecond)
	}
	wg.Wait()

	t.Logf("persist %s, worst write during it %s", persistTook.Round(time.Millisecond), worst.Round(time.Microsecond))
	// A write is microseconds. A tenth of the persist is far above that and far below
	// the whole-persist stall this replaces.
	if worst > persistTook/10 {
		t.Errorf("a write took %s while the index persisted (%s): it waited for the persist",
			worst.Round(time.Millisecond), persistTook.Round(time.Millisecond))
	}
}
