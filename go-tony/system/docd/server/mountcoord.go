package server

import (
	"sync"
	"time"
)

// mountCoord serializes mount/unmount writers against active watch readers on
// overlapping kpaths, with writer priority.
//
// Two kpaths overlap when one is a field-prefix of the other: a writer changing
// the mount set at path P conflicts with a reader (watch) at path R whenever R is
// composed from P (R an ancestor of P) or nested under it (R at/below P). A read
// with no overlapping writer, or on a disjoint subtree, proceeds freely.
//
// Writer priority: once a writer at P is registered, new readers on overlapping
// paths block until it finishes, so a stream of arriving readers cannot starve
// the writer ("new reads don't block mounts"). The writer then waits for the
// readers that were already active to drain; if they do not within forceAfter
// (0 = wait forever), it force-cancels the stragglers via their teardown hooks.
//
// This is the mechanism that lets a composed watch treat its mount membership as
// fixed for its lifetime: membership only changes while no overlapping watch is
// active, or by forcibly ending the watches that would span the change.
type mountCoord struct {
	mu      sync.Mutex
	cond    *sync.Cond
	nextID  uint64
	readers map[uint64]*coordReader
	writers map[uint64][]string // writer id -> path fields
}

type coordReader struct {
	fields []string
	cancel func() // force-teardown, invoked when a writer forces this reader
}

func newMountCoord() *mountCoord {
	c := &mountCoord{
		readers: make(map[uint64]*coordReader),
		writers: make(map[uint64][]string),
	}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// coordOverlap reports whether two kpaths (as field slices) overlap — one is a
// field-prefix of the other.
func coordOverlap(a, b []string) bool {
	return hasFieldPrefix(a, b) || hasFieldPrefix(b, a)
}

// beginRead registers an active watch at path with a force-teardown hook,
// blocking while any writer overlapping path is pending (writer priority). It
// returns a token to release with endRead, and ok=false for an invalid path.
//
// The teardown hook must be idempotent: a reader can be ended either normally
// (endRead) or by a writer forcing it, and both may race.
func (c *mountCoord) beginRead(path string, cancel func()) (uint64, bool) {
	fields, err := pathFields(path)
	if err != nil {
		return 0, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.writerOverlaps(fields) {
		c.cond.Wait()
	}
	c.nextID++
	id := c.nextID
	c.readers[id] = &coordReader{fields: fields, cancel: cancel}
	return id, true
}

// endRead releases a reader token. Safe to call for a token a writer already
// force-cancelled (it is a no-op then).
func (c *mountCoord) endRead(id uint64) {
	c.mu.Lock()
	delete(c.readers, id)
	c.mu.Unlock()
	c.cond.Broadcast()
}

// beginWrite registers a mount/unmount writer at path — immediately blocking new
// overlapping readers — then waits for the readers already active on overlapping
// paths to drain. If they do not within forceAfter (0 = wait forever), it
// force-cancels the stragglers via their teardown hooks and proceeds. It returns
// a release func to call when the mount/unmount completes, and ok=false for an
// invalid path.
func (c *mountCoord) beginWrite(path string, forceAfter time.Duration) (func(), bool) {
	fields, err := pathFields(path)
	if err != nil {
		return nil, false
	}

	c.mu.Lock()
	c.nextID++
	wid := c.nextID
	c.writers[wid] = fields // writer priority now in effect for new readers

	var timedOut bool
	var timer *time.Timer
	if forceAfter > 0 {
		timer = time.AfterFunc(forceAfter, func() {
			c.mu.Lock()
			timedOut = true
			c.mu.Unlock()
			c.cond.Broadcast()
		})
	}

	var toForce []func()
	for {
		strag := c.readersOverlapping(fields)
		if len(strag) == 0 {
			break
		}
		if timedOut {
			for _, id := range strag {
				if r := c.readers[id]; r != nil {
					toForce = append(toForce, r.cancel)
					delete(c.readers, id)
				}
			}
			break
		}
		c.cond.Wait()
	}
	if timer != nil {
		timer.Stop()
	}
	c.mu.Unlock()

	// Cancel outside the lock: a teardown hook may call back into the coordinator
	// (e.g. endRead), which would deadlock under c.mu.
	for _, cancel := range toForce {
		if cancel != nil {
			cancel()
		}
	}

	release := func() {
		c.mu.Lock()
		delete(c.writers, wid)
		c.mu.Unlock()
		c.cond.Broadcast()
	}
	return release, true
}

// writerOverlaps reports whether any pending writer overlaps fields. Caller holds
// c.mu.
func (c *mountCoord) writerOverlaps(fields []string) bool {
	for _, wf := range c.writers {
		if coordOverlap(wf, fields) {
			return true
		}
	}
	return false
}

// readersOverlapping returns the ids of active readers overlapping fields. Caller
// holds c.mu.
func (c *mountCoord) readersOverlapping(fields []string) []uint64 {
	var ids []uint64
	for id, r := range c.readers {
		if coordOverlap(r.fields, fields) {
			ids = append(ids, id)
		}
	}
	return ids
}
