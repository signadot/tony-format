package storage

import (
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
)

// Per-path snapshots (rebuild_plan.md phase 7).
//
// A snapshot is OF a path: its event stream is the subtree there, its own index seeks
// within it, and its segment sits at that path with StartCommit == EndCommit. The root
// snapshot the switch takes is the case kp == "". A read at kp seeks to the nearest
// snapshot at or above kp with the greatest commit (index.SnapshotAtOrAbove) and folds
// the writes since, so what a read at a path holds is bounded by the writes to that path
// since ITS last snapshot, and not by the switch cadence, which is the whole document's.
//
// WHEN one is taken is decided by the reads. A read that folded more records at a path
// than the policy prices a subtree of its size at -- the tail for each byte unit of what
// it emitted, a rate and not a ceiling (pathSnapshotPolicy.wants) -- schedules a snapshot
// of that path as of the commit it read; the next read there folds from it. Reads are
// where the tail is measured for free, and a path nobody reads costs nothing, which is
// the right gradient: the tail a snapshot shortens is a read cost. The rate keeps a
// shallow path with a large subtree from being rewritten every few dozen commits, and
// still takes one once its fold has grown in proportion; the root itself is the
// switch's. A read that did not finish schedules nothing: its bytes are not the
// subtree's, and a caller that took Presence and closed has not paid for a long tail
// either.
//
// One at a time, off the reader, and never concurrent with a switch: the snapshot is
// written to the inactive log, as the root snapshot is, and the switch and the compaction
// that follows it rewrite that log. s.snapMu serializes them. A snapshot is taken only at
// or after the root snapshot in that log, so the log's commits stay ordered within it.
//
// Retention treats a per-path snapshot as it treats the writes it stands in for: kept
// within the cutoff, dropped beyond it. It takes no slot in the root snapshots' tiers,
// which are what history beyond the cutoff is read at.

const (
	// DefaultPathSnapshotTail is the number of records a read may fold at a path before
	// it schedules a snapshot there.
	DefaultPathSnapshotTail int64 = 64
	// DefaultPathSnapshotBytes is the subtree size a snapshot is priced in: a path whose
	// subtree is this big must fold DefaultPathSnapshotTail records to be worth one, and
	// a subtree N times larger must fold N times as many. It is a RATE, not a ceiling --
	// see pathSnapshotPolicy.wants.
	DefaultPathSnapshotBytes int64 = 1 << 20
)

// pathSnapshotPolicy says when a read schedules a snapshot at its path. tail < 0 turns
// it off.
type pathSnapshotPolicy struct {
	tail  int64
	bytes int64
}

// wants prices a snapshot against what it saves. Folding `tail` records happens on EVERY
// later read of the path; writing the subtree happens once. So the fold a path must reach
// before it is snapshotted SCALES with the subtree's size: p.bytes is the size at which
// p.tail records are worth it, and a subtree N times larger has to fold N times as many.
//
// It is deliberately not a ceiling. A flat byte budget -- "a subtree bigger than this is
// served by the root snapshot the switch takes" -- excluded precisely the paths whose
// folds cost the most, so between switches their folds grew without bound: measured, a
// subtree over the budget took ZERO snapshots while its fold passed 800 records and kept
// going, and every read of it paid the whole fold. Whatever else is true, the fold has to
// be bounded; how big the bound is, is what size buys.
func (p pathSnapshotPolicy) wants(kp string, tail, bytes int64, complete bool) bool {
	if p.tail < 0 || kp == "" || !complete {
		return false
	}
	return tail > p.needs(bytes)
}

// needs is the fold a subtree of this size must reach to be worth a snapshot.
func (p pathSnapshotPolicy) needs(bytes int64) int64 {
	if bytes <= p.bytes || p.bytes <= 0 {
		return p.tail
	}
	// Rounded up, so a subtree just over the unit costs more than one just under it.
	return p.tail * ((bytes + p.bytes - 1) / p.bytes)
}

// SetPathSnapshotPolicy configures when a read schedules a snapshot at its path: after
// folding more than tail records for each bytes of the subtree it read, rounded up -- a
// subtree N times bytes must fold more than N times tail. Zero keeps a default; a
// negative tail turns per-path snapshots off.
func (s *Storage) SetPathSnapshotPolicy(tail, bytes int64) {
	if tail == 0 {
		tail = DefaultPathSnapshotTail
	}
	if bytes <= 0 {
		bytes = DefaultPathSnapshotBytes
	}
	s.pathSnap = pathSnapshotPolicy{tail: tail, bytes: bytes}
}

// PathSnapshotPolicy answers the policy in force: how many records a read may fold at a
// path before it snapshots there, and the subtree size that many records is priced for.
// A negative tail is per-path snapshots off.
func (s *Storage) PathSnapshotPolicy() (tail, bytes int64) {
	return s.pathSnap.tail, s.pathSnap.bytes
}

// afterRead is what a read reports to when it is done: how many records it folded and how
// many bytes it emitted, and whether it ran to the end.
func (s *Storage) afterRead(at int64, kp string) func(tail, bytes int64, complete bool) {
	return func(tail, bytes int64, complete bool) {
		if s.pathSnap.tail < 0 || kp == "" || tail <= s.pathSnap.tail {
			return // nowhere near worth one; the ordinary case, and not worth counting
		}
		// Past the base threshold, so it is a read a snapshot might have helped. Every
		// path from here that does not produce one is counted, because a fold that keeps
		// growing is the symptom and the gate that closed is the diagnosis.
		if !complete {
			s.readStats.snapWantedIncomplete.Add(1)
			return
		}
		if tail <= s.pathSnap.needs(bytes) {
			// Not yet worth it for a subtree this size; it will be, as the fold grows.
			s.readStats.snapNotYetWorth.Add(1)
			return
		}
		s.schedulePathSnapshot(at, kp)
	}
}

// schedulePathSnapshot takes a snapshot of kp as of at, off the caller, unless one is
// already in flight -- the next long read at the path asks again -- or the store is
// closing.
func (s *Storage) schedulePathSnapshot(at int64, kp string) {
	if s.closing.Load() {
		return
	}
	if !s.pathSnapBusy.CompareAndSwap(false, true) {
		s.readStats.snapBusy.Add(1)
		return
	}
	s.pathSnapWG.Add(1)
	go func() {
		defer s.pathSnapWG.Done()
		defer s.pathSnapBusy.Store(false)
		if err := s.snapshotPath(at, kp); err != nil {
			s.logger.Warn("path snapshot failed", "path", kp, "commit", at, "error", err)
		}
	}()
}

// waitPathSnapshots waits for a scheduled snapshot to land. For tests.
func (s *Storage) waitPathSnapshots() { s.pathSnapWG.Wait() }

// snapshotPath writes a snapshot of the subtree at kp as of commit at, and indexes it
// there. It is a read at kp piped into the snapshot builder: the same events a reader
// would have been handed, and nothing held that a read does not hold.
//
// It declines rather than fails when there is nothing to do: the path is absent, a
// snapshot at or above it already stands at that commit, the commit precedes the root
// snapshot in the inactive log, or another snapshot holds the log. None is an error; each
// is a snapshot not worth taking.
func (s *Storage) snapshotPath(at int64, kp string) error {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()

	if root, ok := s.index.SnapshotAtOrAbove("", math.MaxInt64); ok && at < root.StartCommit {
		s.readStats.snapRootAhead.Add(1)
		return nil
	}
	if have, ok := s.index.SnapshotAtOrAbove(kp, at); ok && have.StartCommit == at {
		s.readStats.snapAlreadyHave.Add(1)
		return nil
	}

	c, err := s.openRead(at, nil, kp, time.Now(), false)
	if err != nil {
		return err
	}
	defer c.Close()
	if c.Presence() == Absent {
		s.readStats.snapAbsent.Add(1)
		return nil
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	w, err := s.dLog.NewSnapshotWriter(at, timestamp)
	if errors.Is(err, dlog.ErrSnapshotInProgress) {
		s.readStats.snapInProgress.Add(1)
		return nil
	}
	if err != nil {
		return fmt.Errorf("snapshot of %q: %w", kp, err)
	}
	w.SetScopeID(nil)
	w.SetPath(kp)

	builder, err := snap.NewBuilder(w, &snap.Index{})
	if err != nil {
		w.Abandon()
		return fmt.Errorf("snapshot of %q: %w", kp, err)
	}
	var emitted int64
	for {
		ev, err := c.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			w.Abandon()
			return fmt.Errorf("snapshot of %q: %w", kp, err)
		}
		emitted += eventSize(ev)
		if err := builder.WriteEvent(ev); err != nil {
			w.Abandon()
			return fmt.Errorf("snapshot of %q: %w", kp, err)
		}
	}
	if err := builder.Close(); err != nil {
		return fmt.Errorf("snapshot of %q: %w", kp, err)
	}

	generation := s.dLog.GetGeneration(w.LogFileID())
	s.index.Add(index.NewSnapshotSegment(at, kp, string(w.LogFileID()), w.EntryPosition(), generation, nil))
	s.readStats.pathSnapshots.Add(1)
	s.logger.Info("path snapshot created", "path", kp, "commit", at, "bytes", emitted,
		"logFile", w.LogFileID(), "position", w.EntryPosition())
	return nil
}

// pathWithin answers kp relative to its prefix p: the path a read at kp takes inside a
// snapshot of p.
func pathWithin(kp, p string) string {
	if p == "" {
		return kp
	}
	segs := kpath.SplitAll(kp)
	return joinSegments(segs[len(kpath.SplitAll(p)):])
}
