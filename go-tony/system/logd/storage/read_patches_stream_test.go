package storage

import (
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// collectTheOldWay is the algorithm EachPatchInRange replaced, kept here as the oracle:
// read EVERY entry in the range, dedup by commit, then sort the notifications. If moving
// the dedup and the ordering into segment space changed what a replay sees, this is what
// says so.
func collectTheOldWay(t *testing.T, s *Storage, kp string, from, to int64, scopeID *string) []int64 {
	t.Helper()
	segments := s.index.LookupRange(kp, &from, &to, scopeID)
	seen := map[int64]bool{}
	var out []*CommitNotification
	for _, seg := range segments {
		if seen[seg.EndCommit] {
			continue
		}
		seen[seg.EndCommit] = true
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			t.Fatalf("oracle read at %s:%d: %v", seg.LogFile, seg.LogPosition, err)
		}
		if entry.Patch == nil {
			continue
		}
		out = append(out, &CommitNotification{Commit: entry.Commit})
	}
	slices.SortFunc(out, func(a, b *CommitNotification) int { return int(a.Commit - b.Commit) })
	commits := make([]int64, len(out))
	for i, n := range out {
		commits[i] = n.Commit
	}
	return commits
}

// The streamed walk delivers exactly what the collecting one did: the same commits, in
// the same order, each once. The dedup matters here because a patch is indexed at every
// path inside it, so a read at a shallow path meets one commit several times.
func TestEachPatchInRangeMatchesTheCollectedRange(t *testing.T) {
	s := openTestStorage(t)
	for i := 0; i < 60; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{a: {b: {c%d: %d}}, d: %d}`, i%4, i, i))
	}
	head, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	for _, kp := range []string{"", "a", "a.b", "d"} {
		t.Run("at "+kp, func(t *testing.T) {
			want := collectTheOldWay(t, s, kp, 1, head, nil)

			var got []int64
			if err := s.EachPatchInRange(kp, 1, head, nil, func(n *CommitNotification) error {
				got = append(got, n.Commit)
				return nil
			}); err != nil {
				t.Fatalf("EachPatchInRange: %v", err)
			}

			if !slices.Equal(got, want) {
				t.Errorf("streamed %v\ncollected %v", got, want)
			}
			if !slices.IsSorted(got) {
				t.Errorf("not in commit order: %v", got)
			}
			if len(got) == 0 {
				t.Error("no patches, so the comparison proves nothing")
			}
		})
	}
}

// fn's error abandons the range where it stands, so a caller need not drain what it has
// stopped wanting -- which is what lets a failed watch stop mid-replay.
func TestEachPatchInRangeStopsOnError(t *testing.T) {
	s := openTestStorage(t)
	for i := 0; i < 20; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(`{k: %d}`, i))
	}
	head, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	stop := fmt.Errorf("enough")
	seen := 0
	err = s.EachPatchInRange("", 1, head, nil, func(n *CommitNotification) error {
		seen++
		if seen == 3 {
			return stop
		}
		return nil
	})
	if err != stop {
		t.Fatalf("err = %v, want the caller's own error back", err)
	}
	if seen != 3 {
		t.Errorf("walked %d entries after stopping at 3", seen)
	}
}

// liveHeap is the heap that survives a collection, so it measures what is still
// REFERENCED rather than what has been allocated. Both walks allocate the same entries;
// the difference is how many of them are alive at once.
func liveHeap() float64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return float64(m.HeapInuse) / (1 << 20)
}

// The point of streaming: the peak is one entry, not the range.
//
// Collecting the range held every decoded patch at once, about 65KB per commit, so a
// 20k-commit catch-up moved 1.3GB through the heap and a 225k-commit one drove the
// process to 3.7-7.1GB and got the pod evicted (89my9f0kh12ksqknjhn0).
//
// Measured as LIVE heap at the moment of peak, which for the collector is when it
// returns and for the walk is its last callback. Nothing else runs here -- no server, no
// client, no snapshotting -- because a full round trip's peak is dominated by whatever
// else the process is doing and swings by 3x between runs.
func TestEachPatchInRangeDoesNotHoldTheRange(t *testing.T) {
	const n = 4000
	s := openTestStorage(t)
	for i := 0; i < n; i++ {
		mustCommit(t, s, nil, fmt.Sprintf(
			`{demo: {pr: {e%d: {stage: "open", body: "some payload text that a real entity would carry"}}}}`, i))
	}
	head, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("head: %v", err)
	}

	base := liveHeap()
	var atLast float64
	seen := 0
	if err := s.EachPatchInRange("demo", 1, head, nil, func(no *CommitNotification) error {
		seen++
		if seen == n {
			atLast = liveHeap()
		}
		return nil
	}); err != nil {
		t.Fatalf("EachPatchInRange: %v", err)
	}
	if seen != n {
		t.Fatalf("walked %d of %d commits", seen, n)
	}
	streamed := atLast - base

	base = liveHeap()
	collected, err := s.ReadPatchesInRange("demo", 1, head, nil)
	if err != nil {
		t.Fatalf("ReadPatchesInRange: %v", err)
	}
	held := liveHeap() - base
	if len(collected) != n {
		t.Fatalf("collected %d of %d commits", len(collected), n)
	}
	runtime.KeepAlive(collected)

	t.Logf("%d commits: streamed holds %.1f MB at its last entry, collected holds %.1f MB", n, streamed, held)

	// The collector is the control: if IT is not visibly holding the range, the
	// measurement is not working and the comparison below means nothing.
	if held < 8 {
		t.Fatalf("the collecting read held only %.1f MB for %d commits; the measurement is not measuring", held, n)
	}
	if streamed > held/4 {
		t.Errorf("the streamed walk holds %.1f MB against the collector's %.1f MB; the range is being retained",
			streamed, held)
	}
}
