package storage

import (
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// tick is logd's published commit watermark, and the ordered fan-out that rides on it.
//
// A commit passes through three states, and conflating them is what let a watch lose
// events:
//
//   - ALLOCATED — seq.NextCommit has issued the number. Nothing can read it yet.
//   - DURABLE   — the record is in the log (fsynced, under DurabilitySync).
//   - PUBLISHED — durable AND indexed, and therefore readable.
//
// Only the last may be told to anyone outside the commit path. GetCurrentCommit used to
// report the allocated number, straight off the sequence counter, so during the window
// between NextCommit and WriteAndIndex it named a commit that was in neither the log nor
// the index. A watch that took that number as its replay target replayed a range missing
// its final commit, recorded it as already-replayed, and then discarded the live
// notification for it as a duplicate — the event was lost twice over.
//
// The watermark and the notification queue are both advanced by publish, which runs
// under the caller's commit lock. That single fact gives two properties:
//
//   - a commit is announced only once it is readable, so a client told "the tick is T"
//     can immediately read or replay T; and
//   - notifications are queued in commit order, so watchers observe commits in the
//     order they happened. Firing the fan-out after releasing the commit lock (as
//     doCommit used to) left ordering to the goroutine scheduler: a committer
//     descheduled between unlock and notify could be overtaken, and a watcher applying
//     deltas in arrival order would move its state backwards.
//
// The fan-out itself still runs off the commit path, on the dispatcher goroutine, so a
// slow notifier cannot serialize commits.
type tick struct {
	mu          sync.Mutex
	wake        *sync.Cond // dispatcher waits here for work
	idle        *sync.Cond // waitDrained waits here for the queue to empty
	published   int64
	queue       []*CommitNotification
	dispatching bool // a batch is being delivered right now
	closing     bool
	done        chan struct{}

	notifierMu sync.RWMutex
	notifier   CommitNotifier
}

// newTick starts a tick published at the given watermark (the reconciled commit
// counter — see Storage.reconcileWatermark) and runs its dispatcher.
func newTick(published int64) *tick {
	t := &tick{
		published: published,
		done:      make(chan struct{}),
	}
	t.wake = sync.NewCond(&t.mu)
	t.idle = sync.NewCond(&t.mu)
	go t.dispatch()
	return t
}

// publish makes commit visible and queues its notification, which may be nil for a
// commit with no fan-out (a schema snapshot, say).
//
// MUST be called with the commit lock held, after the entry is in the log and the
// index. The lock is what orders both the watermark and the queue by commit; calling
// this without it reintroduces exactly the reordering it exists to prevent.
func (t *tick) publish(commit int64, n *CommitNotification) {
	t.mu.Lock()
	if commit > t.published {
		t.published = commit
	}
	if n != nil {
		t.queue = append(t.queue, n)
	}
	t.mu.Unlock()
	t.wake.Signal()
}

// current returns the published watermark: the highest commit that is readable.
func (t *tick) current() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.published
}

func (t *tick) setNotifier(n CommitNotifier) {
	t.notifierMu.Lock()
	defer t.notifierMu.Unlock()
	t.notifier = n
}

func (t *tick) getNotifier() CommitNotifier {
	t.notifierMu.RLock()
	defer t.notifierMu.RUnlock()
	return t.notifier
}

// dispatch delivers queued notifications, in order, on its own goroutine.
//
// The queue is unbounded on purpose. Bounding it would force a choice between two
// things this design exists to rule out: dropping a notification breaks "every change,
// in order", and blocking the producer puts a slow consumer back in the commit path.
// What keeps it small instead is the notifier contract — implementations must not block
// (WatchHub.Broadcast is itself non-blocking, failing a watcher that cannot keep up) —
// so the dispatcher drains at roughly the speed commits arrive.
func (t *tick) dispatch() {
	defer close(t.done)

	for {
		t.mu.Lock()
		for len(t.queue) == 0 && !t.closing {
			t.wake.Wait()
		}
		if len(t.queue) == 0 {
			t.mu.Unlock()
			return // closing and drained
		}
		batch := t.queue
		t.queue = nil
		t.dispatching = true
		t.mu.Unlock()

		if notifier := t.getNotifier(); notifier != nil {
			for _, n := range batch {
				notifier(n)
			}
		}

		t.mu.Lock()
		t.dispatching = false
		t.mu.Unlock()
		t.idle.Broadcast()
	}
}

// waitDrained blocks until every notification queued so far has been delivered.
//
// This is the barrier that makes asynchronous delivery testable: a commit returns as
// soon as it is published, so "the commit succeeded" no longer implies "the notifier
// ran". Without a barrier a test would have to sleep and hope.
func (t *tick) waitDrained() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.queue) > 0 || t.dispatching {
		t.idle.Wait()
	}
}

// close drains the queue and stops the dispatcher. Callers must have stopped
// committing first: a publish after this returns is never delivered.
//
// It waits for the dispatcher to finish, so a notifier that blocks forever hangs Close.
// That is the same hazard the non-blocking notifier contract already covers — under the
// old synchronous fan-out such a notifier blocked a commit instead.
func (t *tick) close() {
	t.mu.Lock()
	t.closing = true
	t.mu.Unlock()
	t.wake.Broadcast()
	<-t.done
}

// DeliverablePatch is a stored patch in the form a CLIENT sees: its own copy, with the
// internal patch-root markers removed.
//
// It is one function because it was two, and they drifted. A live watcher went through the
// copy-and-strip below; a REPLAYING watcher got the stored node verbatim, marker and all --
// so a resumed watch saw `!delete.logd-patch-root` where a live one saw `!delete`, and a
// consumer testing for `!delete` read a deletion as an ordinary write. Worse quietly: the
// extra tag makes the folded state differ from the state before it, so the change gate
// which suppresses an identical write stopped suppressing it, and every rewrite on a
// resumed watch looked like a change (xmxt2p85h12ksjp1gsn0).
//
// The marker is deliberately STORED -- the read path uses it to find which subtrees a
// commit patched (tx.TagPatchRoots) -- so what it must never do is leave the store.
func DeliverablePatch(stored *ir.Node) *ir.Node {
	if stored == nil {
		return nil
	}
	patch := stored.DeepCopy()
	tx.StripPatchRootTagRecursive(patch)
	return patch
}

// newCommitNotification builds the notification for a committed patch.
//
// The patch is a stripped deep copy, and that ownership is the point: the merged patch
// shares nodes with the patcher data (MergePatches embeds each participant's node), and
// doCommit strips those nodes as soon as the commit returns, to hand each participant
// back clean data. Delivery is now asynchronous, so that strip would otherwise run
// concurrently with watchers reading the very same nodes. Copying here — on the
// committing goroutine, before the strip can start — means the notification owns its
// patch outright and every reader downstream is working on a node nothing else touches.
func newCommitNotification(commit, txSeq int64, timestamp string, mergedPatch *ir.Node, scopeID *string) *CommitNotification {
	patch := DeliverablePatch(mergedPatch)
	return &CommitNotification{
		Commit:    commit,
		TxSeq:     txSeq,
		Timestamp: timestamp,
		KPaths:    extractTopLevelKPaths(mergedPatch),
		Patch:     patch,
		ScopeID:   scopeID,
	}
}
