package storage

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// A narrow read is only narrow if it reads a narrow amount. Every patch is indexed at
// every path inside it, root included, so the lookup on the way DOWN to the read path
// collected one segment per commit in the store -- a read of one small path replayed
// every write anyone had made. Staging measured narrow reads averaging three seconds on
// a store of fifty thousand entries, which is what sent the fix looking here
// (ap8ddvp2h12krd43gdn0).
//
// A write to a sibling must not cost a read of this path anything.
func TestNarrowReadIgnoresSiblingWrites(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer s.Close()

	blob := strings.Repeat("x", 200)
	for i := 0; i < 300; i++ {
		id := "e" + strconv.Itoa(i)
		subtreeWrite(t, s, "verse.entities."+id, "{id: "+id+", blob: "+blob+"}")
	}
	subtreeWrite(t, s, "verse.meta.rev", "{n: 1}")
	if err := s.SwitchDLog(); err != nil {
		t.Fatalf("snapshot: %s", err)
	}

	read := func() time.Duration {
		commit, _ := s.GetCurrentCommit()
		start := time.Now()
		if _, ok, err := s.ReadSubtreeAt("verse.meta.rev", commit, nil); err != nil || !ok {
			t.Fatalf("narrow read: ok=%v err=%v", ok, err)
		}
		return time.Since(start)
	}

	quiet := read()
	// A thousand writes to other paths, none of them at or under what is read.
	for i := 0; i < 1000; i++ {
		id := "e" + strconv.Itoa(i%300)
		subtreeWrite(t, s, "verse.entities."+id, "{n: "+strconv.Itoa(i)+"}")
	}
	busy := read()

	t.Logf("read of verse.meta.rev: %s after a snapshot, %s after 1000 sibling writes",
		quiet.Round(time.Microsecond), busy.Round(time.Microsecond))

	// Some growth is fair -- the lookup still walks a bigger index -- but the read must
	// not take on the siblings' deltas. Twenty times the quiet read plus a five
	// millisecond floor is far above what a busy machine adds and far below replaying a
	// thousand patches, which was ninety times.
	if budget := 20*quiet + 5*time.Millisecond; busy > budget {
		t.Errorf("the read costs %s after writes to other paths (was %s, budget %s): it is replaying them",
			busy.Round(time.Microsecond), quiet.Round(time.Microsecond), budget.Round(time.Microsecond))
	}
}
