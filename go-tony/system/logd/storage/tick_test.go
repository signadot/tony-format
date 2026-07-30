package storage

import (
	"fmt"
	"sync"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// windowResolver runs fn inside a commit's critical section. doCommit calls GetSchema
// after NextCommit and before WriteAndIndex, which is exactly the window in which the
// commit number exists but the entry does not — the window GetCurrentCommit used to
// leak.
type windowResolver struct{ fn func() }

func (w windowResolver) GetSchema(scopeID *string) *api.Schema {
	if w.fn != nil {
		w.fn()
	}
	return nil
}

// A commit the watermark names must be readable. Reporting the allocated number instead
// meant a watch could take a replay target that was in neither the log nor the index:
// the replay then missed that commit, recorded it as replayed, and dropped its live
// notification as a duplicate.
func TestTick_WatermarkNamesOnlyReadableCommits(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	commitValue(t, s, `{a: 1}`)

	var inWindow int64
	var segments, patches int
	s.SetSchemaResolver(windowResolver{fn: func() {
		inWindow, _ = s.GetCurrentCommit()
		segments = len(s.index.LookupRange("", &inWindow, &inWindow, nil))
		if ns, err := s.ReadPatchesInRange("", inWindow, inWindow, nil); err == nil {
			patches = len(ns)
		}
	}})

	committed := commitValue(t, s, `{b: 2}`)

	if inWindow == committed {
		t.Errorf("watermark reported the in-flight commit %d, which was not yet readable "+
			"(index segments: %d, replayable patches: %d)", committed, segments, patches)
	}
	if inWindow != committed-1 {
		t.Errorf("watermark during commit %d = %d, want %d (the last published commit)",
			committed, inWindow, committed-1)
	}

	// And once the commit returns, the watermark names it and it is readable.
	after, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if after != committed {
		t.Errorf("watermark after commit = %d, want %d", after, committed)
	}
	ns, err := s.ReadPatchesInRange("", after, after, nil)
	if err != nil {
		t.Fatalf("ReadPatchesInRange: %v", err)
	}
	if len(ns) != 1 {
		t.Errorf("commit %d named by the watermark yielded %d replayable patches, want 1", after, len(ns))
	}
}

// Every watermark a reader can observe must be readable, not just the ones observed at
// a convenient moment. Sampled from a concurrent reader while writers commit.
func TestTick_WatermarkAlwaysReadableUnderConcurrency(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	commitValue(t, s, `{seed: 0}`)

	done := make(chan struct{})
	bad := make(chan string, 8)

	go func() {
		defer close(done)
		for range 400 {
			c, err := s.GetCurrentCommit()
			if err != nil || c == 0 {
				continue
			}
			// The watermark claims c is readable: a state read at c must work, and c
			// must not be beyond what the index knows.
			if _, err := s.ReadStateAt("", c, nil); err != nil {
				select {
				case bad <- fmt.Sprintf("ReadStateAt(%d): %v", c, err):
				default:
				}
				return
			}
			if maxIndexed, _ := s.indexWatermarks(); c > maxIndexed {
				select {
				case bad <- fmt.Sprintf("watermark %d is ahead of the highest indexed commit %d", c, maxIndexed):
				default:
				}
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx, err := s.NewTx(1, nil)
			if err != nil {
				return
			}
			data, _ := parse.Parse(fmt.Appendf(nil, `{k%d: %d}`, i, i))
			p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
			if err != nil {
				return
			}
			p.Commit()
		}(i)
	}
	wg.Wait()
	<-done

	select {
	case msg := <-bad:
		t.Error(msg)
	default:
	}
}

// Notifications must arrive in commit order. Firing the fan-out after releasing the
// commit lock left this to the scheduler: a committer descheduled between unlock and
// notify could be overtaken, and a watcher applying deltas in arrival order would move
// its state backwards.
func TestTick_NotificationsInCommitOrder(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var mu sync.Mutex
	var seen []int64
	s.SetCommitNotifier(func(n *CommitNotification) {
		mu.Lock()
		seen = append(seen, n.Commit)
		mu.Unlock()
	})

	const writers = 64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tx, err := s.NewTx(1, nil)
			if err != nil {
				return
			}
			data, _ := parse.Parse(fmt.Appendf(nil, `{k%d: %d}`, i, i))
			p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
			if err != nil {
				return
			}
			p.Commit()
		}(i)
	}
	close(start)
	wg.Wait()
	s.tick.waitDrained()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != writers {
		t.Fatalf("got %d notifications, want %d", len(seen), writers)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("notification %d arrived out of order: %v", i, seen)
		}
	}
}

// A notification's patch must be private to it. The merged patch shares nodes with the
// patcher data, and doCommit strips those nodes as the commit returns — concurrently
// with the dispatcher, now that delivery is asynchronous.
func TestTick_NotificationPatchIsPrivateAndStripped(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var mu sync.Mutex
	var got *CommitNotification
	s.SetCommitNotifier(func(n *CommitNotification) {
		mu.Lock()
		got = n
		mu.Unlock()
	})

	txn, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("NewTx: %v", err)
	}
	data, err := parse.Parse([]byte(`{users: {alice: {name: "Alice"}}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: "", Data: data}})
	if err != nil {
		t.Fatalf("NewPatcher: %v", err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("commit: %v", r.Error)
	}
	s.tick.waitDrained()

	mu.Lock()
	defer mu.Unlock()
	if got == nil || got.Patch == nil {
		t.Fatal("no notification patch delivered")
	}
	if got.Patch == data {
		t.Error("notification patch is the caller's node, not a private copy")
	}
	// Only the internal patch-root marker must be gone; syntax tags like !bracket are
	// part of the value and stay.
	if n := countPatchRootTags(got.Patch); n != 0 {
		t.Errorf("notification patch carries %d %s tags; they must be stripped", n, tx.PatchRootTag)
	}
	// Mutating the delivered patch must not reach the caller's node, which doCommit is
	// free to strip the moment the commit returns.
	got.Patch.Tag = "!mutated"
	if data.Tag == "!mutated" {
		t.Error("notification patch shares nodes with the caller's patch data")
	}
}

func countPatchRootTags(n *ir.Node) int {
	if n == nil {
		return 0
	}
	count := 0
	if tx.HasPatchRootTag(n) {
		count++
	}
	for _, v := range n.Values {
		count += countPatchRootTags(v)
	}
	return count
}

// A commit with no notifier registered still advances the watermark.
func TestTick_PublishesWithoutNotifier(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	last := commitValue(t, s, `{a: 1}`)
	got, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got != last {
		t.Errorf("watermark = %d, want %d", got, last)
	}
}

// Reopening starts the tick at the reconciled watermark, so a reader sees the same
// commit the store closed at rather than 0.
func TestTick_StartsAtReconciledWatermark(t *testing.T) {
	dir := t.TempDir()
	last := seedCommits(t, dir, 3)

	s, err := Open(dir, nil)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()

	got, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatalf("GetCurrentCommit: %v", err)
	}
	if got != last {
		t.Errorf("watermark after reopen = %d, want %d", got, last)
	}
}

// Close delivers what the dispatcher still holds rather than dropping it.
func TestTick_CloseDrainsPendingNotifications(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	var mu sync.Mutex
	delivered := 0
	s.SetCommitNotifier(func(n *CommitNotification) {
		mu.Lock()
		delivered++
		mu.Unlock()
	})

	const n = 8
	for i := range n {
		commitValue(t, s, fmt.Sprintf("{k%d: %d}", i, i))
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if delivered != n {
		t.Errorf("delivered %d notifications, want all %d after Close", delivered, n)
	}
}
