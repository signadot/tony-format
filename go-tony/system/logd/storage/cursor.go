package storage

import (
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
)

// The read interface (read_write_interface.md).
//
// There is one read. It is at a path, and the root is a path, so a read of the whole
// document is Read(at, view, "") and not a different function. It answers with a CURSOR
// over the event stream of the subtree at the path -- the substrate's own currency -- and
// not with a document, because a signature that returns a document whose size grows with
// the store cannot express the bound a read is held to.
//
// A read is three steps, and the bound is a property of each:
//
//	seek     the nearest snapshot at or below `at`, opened AT kp through the snapshot's
//	         own path index (findSubtreeBaseReader); a snapshot at the root serves a
//	         bounded base read at any path in it.
//	project  each write that can reach kp since that snapshot -- and no other, which is
//	         what index.Segments answers -- read from the log one record at a time and cut
//	         down to what it says about kp as it is read. What is held is that list, whose
//	         size is what changed under kp since the snapshot.
//	fold     one streaming pass: the base events with the projected writes applied over
//	         them, events out, consumed by the cursor as they are produced.
//
// So the resident terms are the projected writes and one record, and neither is a count of
// how much has happened under the path. Both large terms of the read the forensics caught
// in flight -- a slice of every segment in range, and every patch in range decoded and held
// whole -- are signatures this file does not have.
//
// The one thing a subtree cannot say about itself is what an OPERATOR above it did: a
// !replace or !delete of an ancestor states the whole value there, and a read below it
// cannot be composed from the subtree's own writes. Such a read is taken at that ancestor
// and navigated down. That intermediate is the operand the write installed, which is the
// term the bound explicitly admits.

// Presence is what a read found at its path, before any event.
type Presence int

const (
	// Absent: nothing is at the path. No events follow.
	Absent Presence = iota
	// Null: the path holds a null. One event follows.
	Null
	// Present: the path holds a value.
	Present
)

func (p Presence) String() string {
	switch p {
	case Absent:
		return "absent"
	case Null:
		return "null"
	}
	return "present"
}

// Cursor is a read in progress: the events of the subtree at a path, in document order.
// Presence answers before the first event. Close releases what the read holds and may be
// called at any point; a cursor holds no lock between calls to Next.
type Cursor interface {
	Presence() Presence
	Next() (*stream.Event, error) // io.EOF at the end
	Close() error
}

// ErrBudget is what Collect answers when the node it is asked for is larger than the
// caller said it was prepared to hold.
var ErrBudget = errors.New("read exceeds its budget")

// Read opens a cursor over the subtree at kp as of commit `at`, in the view scopeID names:
// baseline when nil, and otherwise baseline with that scope's writes folded last.
//
// A read at a path the index can prove was never written opens no log file at all: the
// spine proves it, and Presence answers Absent from that proof.
func (s *Storage) Read(at int64, scopeID *string, kp string) (Cursor, error) {
	if _, err := kpath.Parse(kp); err != nil {
		return nil, fmt.Errorf("read at %q: %w", kp, err)
	}
	started := time.Now()
	if kp != "" && scopeID == nil && s.provenAbsent(kp) {
		s.readStats.note(ReadNarrowAbsent, kp, time.Since(started))
		s.readStats.noteBound(0, 0, true)
		return absentCursor{}, nil
	}
	return s.openRead(at, scopeID, kp, started)
}

// provenAbsent says the index shows kp was never written: every step of the written
// prefix is an object, and kp leaves it. A path with a keyed or indexed segment is not
// proven here; the document answers those.
func (s *Storage) provenAbsent(kp string) bool {
	segs := kpath.SplitAll(kp)
	for _, seg := range segs {
		if _, isField := kpath.SegmentFieldName(seg); !isField {
			return false
		}
	}
	written, ok := s.index.UnwrittenBelow(kp)
	return ok && written < len(segs)
}

// errBlocked says a write above kp cannot be projected onto it: an operator, or a value
// kp descends into that is not a container kp's next segment can step into.
var errBlocked = errors.New("blocked above the path")

func (s *Storage) openRead(at int64, scopeID *string, kp string, started time.Time) (Cursor, error) {
	base, startCommit, err := s.findSubtreeBaseReader(at, kp)
	if err != nil {
		return nil, err
	}
	seekHit := startCommit > 0

	var projected []*ir.Node
	var largest int64
	blockedAt := -1
	project := func(seg index.LogSegment) error {
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			return nil
		}
		node, depth, ok := projectAt(entry.Patch, kp)
		if !ok {
			blockedAt = depth
			return errBlocked
		}
		if node == nil {
			return nil // this write says nothing about kp
		}
		projected = append(projected, node)
		if n := nodeSize(node); n > largest {
			largest = n
		}
		return nil
	}
	// Baseline from the snapshot forward, then the scope over its whole history, in that
	// order: the snapshot is baseline's, so baseline needs only what came after it and a
	// scope needs everything. Applying the scope last is what makes its writes shadow
	// baseline's.
	for seg := range s.index.Segments(kp, &startCommit, &at, nil) {
		if seg.StartCommit == seg.EndCommit {
			continue // a snapshot
		}
		if err = project(seg); err != nil {
			break
		}
	}
	if err == nil && scopeID != nil {
		for seg := range s.index.Segments(kp, nil, &at, scopeID) {
			if seg.StartCommit == seg.EndCommit || seg.ScopeID == nil || *seg.ScopeID != *scopeID || isOverlaySegment(seg) {
				continue
			}
			if err = project(seg); err != nil {
				break
			}
		}
	}
	if errors.Is(err, errBlocked) {
		base.Close()
		return s.readThroughAncestor(at, scopeID, kp, blockedAt, started)
	}
	if err != nil {
		base.Close()
		return nil, err
	}
	kind := ReadNarrow
	if kp == "" {
		kind = ReadWideRoot
	}
	return newFoldCursor(base, projected, &s.readStats, kind, kp, started, largest, seekHit), nil
}

// readThroughAncestor is the read at kp when a write above it states the whole value
// there. The ancestor at `depth` segments is read and navigated down: what results is
// exactly what a read at kp means, and the ancestor's value is the intermediate the
// bound admits, being what that write installed.
func (s *Storage) readThroughAncestor(at int64, scopeID *string, kp string, depth int, started time.Time) (Cursor, error) {
	segs := kpath.SplitAll(kp)
	prefix := joinSegments(segs[:depth])
	c, err := s.openRead(at, scopeID, prefix, started)
	if err != nil {
		return nil, err
	}
	node, err := collectAll(c)
	if err != nil {
		return nil, err
	}
	s.readStats.note(ReadWideOperator, kp, time.Since(started))
	if node == nil {
		return absentCursor{}, nil
	}
	sub, err := node.GetKPathWith(joinSegments(segs[depth:]), ir.WithComments(true))
	if err != nil || sub == nil {
		return absentCursor{}, nil
	}
	events, err := stream.NodeToEvents(sub)
	if err != nil {
		return nil, err
	}
	return &sliceCursor{events: events}, nil
}

// projectAt answers what patch writes at or below kp, re-rooted at kp; nil when it says
// nothing about kp. ok is false when the patch cannot be seen from kp -- an operator above
// it, or a value kp descends into that is not a container the next segment can step into
// -- and depth is then how many of kp's segments were descended before the block, which
// is the ancestor a read has to be taken at instead.
func projectAt(patch *ir.Node, kp string) (at *ir.Node, depth int, ok bool) {
	if patch == nil {
		return nil, 0, true
	}
	segs := kpath.SplitAll(kp)
	n := patch
	for depth = range segs {
		n = ir.Uncomment(n)
		if n == nil {
			return nil, depth, true
		}
		if hasOperator(n.Tag) {
			return nil, depth, false
		}
		if n.Type != ir.ObjectType {
			// A scalar or a list where kp descends: the write replaces the node kp is
			// inside, which is a statement about the ancestor and not about kp.
			return nil, depth, false
		}
		name, isField := kpath.SegmentFieldName(segs[depth])
		if !isField {
			return nil, depth, false // an index: the array is the unit
		}
		next := ir.Get(n, name)
		if next == nil {
			return nil, depth, true // the patch does not reach kp
		}
		n = next
	}
	return n, len(segs), true
}

// hasOperator reports whether a tag names a merge operation, which is what makes a node's
// subtree unable to speak for it. Presentation and data tags are not operations: they
// travel with the value and say nothing about how it merges.
func hasOperator(tag string) bool {
	for t := tag; t != ""; {
		head, _, rest := ir.TagArgs(t)
		if head == "" {
			return false
		}
		if mergeop.Lookup(head[1:]) != nil {
			return true
		}
		if rest == t {
			return false
		}
		t = rest
	}
	return false
}

func joinSegments(segs []string) string {
	if len(segs) == 0 {
		return ""
	}
	result := segs[len(segs)-1]
	for i := len(segs) - 2; i >= 0; i-- {
		result = kpath.Join(segs[i], result)
	}
	return result
}

// nodeSize is a cheap measure of a node's size in memory: what the bound reports as the
// largest record a read held.
func nodeSize(n *ir.Node) int64 {
	if n == nil {
		return 0
	}
	size := int64(64 + len(n.String) + len(n.Tag))
	for _, f := range n.Fields {
		size += nodeSize(f)
	}
	for _, v := range n.Values {
		size += nodeSize(v)
	}
	return size
}

// eventSize is what one event costs, for the bytes-emitted term of the bound.
func eventSize(ev *stream.Event) int64 {
	size := int64(16 + len(ev.Key) + len(ev.String) + len(ev.Tag))
	for _, l := range ev.CommentLines {
		size += int64(len(l))
	}
	return size
}

// absentCursor is a read that found nothing.
type absentCursor struct{}

func (absentCursor) Presence() Presence           { return Absent }
func (absentCursor) Next() (*stream.Event, error) { return nil, io.EOF }
func (absentCursor) Close() error                 { return nil }

// sliceCursor replays events already in hand.
type sliceCursor struct {
	events []stream.Event
	i      int
}

func (c *sliceCursor) Presence() Presence { return presenceOf(c.events) }

func (c *sliceCursor) Next() (*stream.Event, error) {
	if c.i >= len(c.events) {
		return nil, io.EOF
	}
	ev := &c.events[c.i]
	c.i++
	return ev, nil
}

func (c *sliceCursor) Close() error { return nil }

// presenceOf reads presence off the first events: none is Absent, a null past any head
// comment is Null, anything else is Present.
func presenceOf(events []stream.Event) Presence {
	for i := range events {
		switch events[i].Type {
		case stream.EventHeadComment, stream.EventLineComment:
			continue
		case stream.EventNull:
			return Null
		}
		return Present
	}
	return Absent
}

// errCursorClosed stops the fold when the cursor is closed before the end.
var errCursorClosed = errors.New("cursor closed")

// foldCursor is a read being folded: the streaming processor runs beside the reader,
// writing events into a small channel, and Next takes them. The channel's capacity is the
// whole of what the fold holds ahead of the reader.
type foldCursor struct {
	events chan stream.Event
	errc   chan error
	done   chan struct{}
	once   sync.Once

	peeked   []stream.Event // events read for Presence and not yet handed out
	peekErr  error
	presence Presence
	known    bool

	stats   *readStats
	kind    ReadKind
	kp      string
	started time.Time
	largest int64
	bytes   int64
	seekHit bool
	noted   bool
}

// chanSink is the processor's sink: an event writer that hands each event to the cursor.
type chanSink struct{ c *foldCursor }

func (s chanSink) WriteEvent(ev *stream.Event) error {
	e := *ev // the processor may reuse the event it hands over
	select {
	case s.c.events <- e:
		return nil
	case <-s.c.done:
		return errCursorClosed
	}
}

func newFoldCursor(base patches.EventReadCloser, projected []*ir.Node, stats *readStats, kind ReadKind, kp string, started time.Time, largest int64, seekHit bool) *foldCursor {
	c := &foldCursor{
		events:  make(chan stream.Event, 64),
		errc:    make(chan error, 1),
		done:    make(chan struct{}),
		stats:   stats,
		kind:    kind,
		kp:      kp,
		started: started,
		largest: largest,
		seekHit: seekHit,
	}
	go func() {
		err := patches.NewStreamingProcessor().ApplyPatches(base, projected, chanSink{c})
		base.Close()
		c.errc <- err
		close(c.events)
	}()
	return c
}

func (c *foldCursor) next() (*stream.Event, error) {
	ev, ok := <-c.events
	if !ok {
		err := <-c.errc
		c.errc <- err // for a second caller
		if err == nil || errors.Is(err, errCursorClosed) {
			c.note()
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to apply patches: %w", err)
	}
	c.bytes += eventSize(&ev)
	return &ev, nil
}

func (c *foldCursor) Presence() Presence {
	if c.known {
		return c.presence
	}
	c.known = true
	for {
		ev, err := c.next()
		if err != nil {
			c.peekErr = err
			c.presence = presenceOf(c.peeked)
			return c.presence
		}
		c.peeked = append(c.peeked, *ev)
		if ev.Type == stream.EventHeadComment || ev.Type == stream.EventLineComment {
			continue
		}
		c.presence = presenceOf(c.peeked)
		return c.presence
	}
}

func (c *foldCursor) Next() (*stream.Event, error) {
	if len(c.peeked) > 0 {
		ev := c.peeked[0]
		c.peeked = c.peeked[1:]
		return &ev, nil
	}
	if c.peekErr != nil {
		err := c.peekErr
		return nil, err
	}
	return c.next()
}

func (c *foldCursor) Close() error {
	c.once.Do(func() {
		close(c.done)
		for range c.events { // let the fold finish, so the base reader is closed
		}
		c.note()
	})
	return nil
}

// note records the read against the counters, once.
func (c *foldCursor) note() {
	if c.noted || c.stats == nil {
		return
	}
	c.noted = true
	c.stats.note(c.kind, c.kp, time.Since(c.started))
	c.stats.noteBound(c.bytes, c.largest, c.seekHit)
}

// Collect materializes a cursor as a node, and it is the one place a node is built from a
// read. It takes a budget with no default: a caller that wants a node says how large a
// node it is prepared to hold, and is refused past it. A nil node with no error is a read
// that found nothing.
func Collect(c Cursor, budget int64) (*ir.Node, error) {
	if budget <= 0 {
		return nil, fmt.Errorf("%w: a read that builds a node states its budget", ErrBudget)
	}
	return collectWithin(c, budget)
}

// collectAll is the store's own materialization, for the intermediates the bound admits:
// a write's operand read through an ancestor, and the document the stepped head holds.
func collectAll(c Cursor) (*ir.Node, error) {
	return collectWithin(c, math.MaxInt64)
}

func collectWithin(c Cursor, budget int64) (*ir.Node, error) {
	defer c.Close()
	var events []stream.Event
	var size int64
	for {
		ev, err := c.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		size += eventSize(ev)
		if size > budget {
			return nil, fmt.Errorf("%w of %d bytes", ErrBudget, budget)
		}
		events = append(events, *ev)
	}
	if len(events) == 0 {
		return nil, nil
	}
	return stream.EventsToNode(events)
}

// Rooted answers c's events under kp's ancestors: the opens of each field on the way
// down, the subtree, and the closes -- a document of the same shape a read at the root
// would have, with everything the caller did not ask for left out. Nothing is held. An
// absent subtree stays absent; a path with an indexed segment cannot be rooted, since a
// position is not a field, and answers an error on the first call.
func Rooted(c Cursor, kp string) Cursor {
	if kp == "" {
		return c
	}
	segs := kpath.SplitAll(kp)
	names := make([]string, 0, len(segs))
	for _, seg := range segs {
		name, isField := kpath.SegmentFieldName(seg)
		if !isField {
			c.Close()
			return errCursor{fmt.Errorf("cannot root a read at %q: %q is not a field", kp, seg)}
		}
		names = append(names, name)
	}
	return &rootedCursor{inner: c, names: names}
}

type errCursor struct{ err error }

func (e errCursor) Presence() Presence           { return Absent }
func (e errCursor) Next() (*stream.Event, error) { return nil, e.err }
func (e errCursor) Close() error                 { return nil }

type rootedCursor struct {
	inner  Cursor
	names  []string
	opened int // ancestor levels opened so far, two events each
	closed int // ancestor levels closed so far
	inEOF  bool
}

func (r *rootedCursor) Presence() Presence {
	if r.inner.Presence() == Absent {
		return Absent
	}
	return Present
}

func (r *rootedCursor) Next() (*stream.Event, error) {
	if r.inner.Presence() == Absent {
		return nil, io.EOF
	}
	if r.opened < 2*len(r.names) {
		level := r.opened / 2
		r.opened++
		if r.opened%2 == 1 {
			return &stream.Event{Type: stream.EventBeginObject}, nil
		}
		return &stream.Event{Type: stream.EventKey, Key: r.names[level]}, nil
	}
	if !r.inEOF {
		ev, err := r.inner.Next()
		if err == io.EOF {
			r.inEOF = true
		} else {
			return ev, err
		}
	}
	if r.closed < len(r.names) {
		r.closed++
		return &stream.Event{Type: stream.EventEndObject}, nil
	}
	return nil, io.EOF
}

func (r *rootedCursor) Close() error { return r.inner.Close() }
