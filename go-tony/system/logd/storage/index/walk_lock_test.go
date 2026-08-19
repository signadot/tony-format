package index

import (
	"strconv"
	"sync"
	"testing"
	"time"
)

// A walk of the index must not stop writes for its own length. Every lookup held the
// node's lock across its recursion, so a walk of the whole index -- which is what
// compaction does, and what a read at the root does -- held the ROOT's read lock for the
// duration, and a commit's Add, which needs the root's write lock, waited it out. Staging
// measured a single commit paying 5.819s in its index phase, inside a snapshot window
// (kds4sx3bh12krdrkghn0).
func TestAWalkDoesNotStopWrites(t *testing.T) {
	idx := NewIndex("")
	const paths, revs = 3000, 8
	for p := 0; p < paths; p++ {
		kp := "verse.entities.e" + strconv.Itoa(p)
		for r := 0; r < revs; r++ {
			c := int64(p*revs + r)
			idx.Add(&LogSegment{StartCommit: c, EndCommit: c, KindedPath: kp, LogFile: "A", LogPosition: c})
		}
	}

	// What a whole-index walk costs, measured rather than assumed.
	start := time.Now()
	if n := len(idx.AllSegments()); n == 0 {
		t.Fatal("the walk found nothing")
	}
	walkTook := time.Since(start)
	if walkTook < 5*time.Millisecond {
		t.Skipf("a walk of this index takes %s; too quick here to tell a stall from noise", walkTook)
	}

	// Write while walking, and time the writes.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				idx.AllSegments()
			}
		}
	}()

	worst := time.Duration(0)
	deadline := time.Now().Add(2 * walkTook)
	for i := 0; time.Now().Before(deadline); i++ {
		c := int64(10_000_000 + i)
		s := time.Now()
		idx.Add(&LogSegment{StartCommit: c, EndCommit: c, KindedPath: "verse.meta.rev", LogFile: "A", LogPosition: c})
		if took := time.Since(s); took > worst {
			worst = took
		}
	}
	close(stop)
	wg.Wait()

	t.Logf("walk %s, worst write during it %s", walkTook.Round(time.Millisecond), worst.Round(time.Microsecond))
	// A write is microseconds. Half a walk is far above that and far below the
	// whole-walk stall this replaces.
	if worst > walkTook/2 {
		t.Errorf("a write took %s while the index was walked (%s): it waited for the walk",
			worst.Round(time.Millisecond), walkTook.Round(time.Millisecond))
	}
}
