package server

import (
	"sync/atomic"
	"testing"
	"time"
)

// waitReturn runs fn in a goroutine and reports whether it returned within d.
func waitReturn(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() { fn(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

func TestMountCoord_OverlapSemantics(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"a.b", "a", true},     // reader ancestor of writer
		{"a.b", "a.b", true},   // equal
		{"a.b", "a.b.c", true}, // reader nested under writer
		{"a.b", "a.c", false},  // siblings
		{"a.b", "x", false},    // disjoint
	}
	for _, tc := range cases {
		af, _ := pathFields(tc.a)
		bf, _ := pathFields(tc.b)
		if got := coordOverlap(af, bf); got != tc.want {
			t.Errorf("coordOverlap(%q,%q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMountCoord_DisjointReaderDoesNotBlockWriter(t *testing.T) {
	c := newMountCoord()
	rid, ok := c.beginRead("x.y", func() { t.Error("disjoint reader must not be forced") })
	if !ok {
		t.Fatal("beginRead failed")
	}
	defer c.endRead(rid)

	if !waitReturn(200*time.Millisecond, func() {
		rel, ok := c.beginWrite("a.b", 0)
		if !ok {
			t.Error("beginWrite failed")
		}
		rel()
	}) {
		t.Fatal("writer blocked on a disjoint reader")
	}
}

func TestMountCoord_ForceAfterInfinityWaitsThenReleases(t *testing.T) {
	c := newMountCoord()
	var forced atomic.Bool
	rid, _ := c.beginRead("a", func() { forced.Store(true) })

	// forceAfter=0 means never force: the writer must block while the overlapping
	// reader is active.
	if waitReturn(200*time.Millisecond, func() {
		rel, _ := c.beginWrite("a.b", 0)
		rel()
	}) {
		t.Fatal("writer proceeded despite an active overlapping reader (forceAfter=0)")
	}
	if forced.Load() {
		t.Fatal("reader was force-cancelled under forceAfter=0")
	}

	// Once the reader leaves, the writer proceeds.
	released := make(chan struct{})
	go func() {
		rel, _ := c.beginWrite("a.b", 0)
		rel()
		close(released)
	}()
	time.Sleep(50 * time.Millisecond)
	c.endRead(rid)
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("writer did not proceed after reader ended")
	}
}

func TestMountCoord_ForceAfterCancelsStraggler(t *testing.T) {
	c := newMountCoord()
	forced := make(chan struct{})
	c.beginRead("a.b.c", func() { close(forced) }) // nested under the writer's a.b

	rel, ok := c.beginWrite("a.b", 30*time.Millisecond)
	if !ok {
		t.Fatal("beginWrite failed")
	}
	defer rel()

	select {
	case <-forced:
	case <-time.After(time.Second):
		t.Fatal("straggler reader was not force-cancelled after forceAfter elapsed")
	}
	// After forcing, the reader is gone: a fresh overlapping write proceeds at once.
	if !waitReturn(200*time.Millisecond, func() {
		r2, _ := c.beginWrite("a.b", 0)
		r2()
	}) {
		t.Fatal("writer still blocked after straggler was forced")
	}
}

func TestMountCoord_WriterPriorityBlocksNewReaders(t *testing.T) {
	c := newMountCoord()
	// An existing reader keeps a forceAfter=0 writer parked and waiting.
	existing, _ := c.beginRead("a", func() {})

	writerParked := make(chan struct{})
	go func() {
		close(writerParked)
		rel, _ := c.beginWrite("a.b", 0) // waits for `existing` to drain
		time.Sleep(20 * time.Millisecond)
		rel()
	}()
	<-writerParked
	time.Sleep(50 * time.Millisecond) // let the writer register and park

	// A NEW reader overlapping the pending writer must block (writer priority),
	// not slip in ahead of the mount.
	if waitReturn(150*time.Millisecond, func() {
		rid, _ := c.beginRead("a.b.c", func() {})
		c.endRead(rid)
	}) {
		t.Fatal("new overlapping reader proceeded while a writer was pending")
	}

	// Releasing the existing reader lets the writer finish, which then admits the
	// blocked reader.
	c.endRead(existing)
	if !waitReturn(time.Second, func() {
		rid, _ := c.beginRead("a.b.c", func() {})
		c.endRead(rid)
	}) {
		t.Fatal("new reader never admitted after writer finished")
	}
}
