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
// WHEN one is taken is decided by the reads. A read that folded more than the policy's
// tail of records at a path, and emitted no more than its byte budget, schedules a
// snapshot of that path as of the commit it read; the next read there folds from it.
// Reads are where the tail is measured for free, and a path nobody reads costs nothing,
// which is the right gradient: the tail a snapshot shortens is a read cost. The byte
// budget keeps a shallow path with a large subtree -- the root itself, or a path just
// beneath it -- from being rewritten every few dozen commits; those are the switch's. A
// read that did not finish schedules nothing: its bytes are not the subtree's, and a
// caller that took Presence and closed has not paid for a long tail either.
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
	// DefaultPathSnapshotBytes is the largest subtree, in emitted bytes, a read schedules
	// a snapshot of. Larger ones are served by the root snapshot the switch takes.
	DefaultPathSnapshotBytes int64 = 1 << 20
)

// pathSnapshotPolicy says when a read schedules a snapshot at its path. tail < 0 turns
// it off.
type pathSnapshotPolicy struct {
	tail  int64
	bytes int64
}

func (p pathSnapshotPolicy) wants(kp string, tail, bytes int64, complete bool) bool {
	return p.tail >= 0 && kp != "" && complete && tail > p.tail && bytes <= p.bytes
}

// SetPathSnapshotPolicy configures when a read schedules a snapshot at its path: after
// folding more than tail records of a subtree of at most bytes. Zero keeps a default; a
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
// path before it snapshots there, and the largest subtree it snapshots. A negative tail
// is per-path snapshots off.
func (s *Storage) PathSnapshotPolicy() (tail, bytes int64) {
	return s.pathSnap.tail, s.pathSnap.bytes
}

// afterRead is what a read reports to when it is done: how many records it folded and how
// many bytes it emitted, and whether it ran to the end.
func (s *Storage) afterRead(at int64, kp string) func(tail, bytes int64, complete bool) {
	return func(tail, bytes int64, complete bool) {
		if s.pathSnap.wants(kp, tail, bytes, complete) {
			s.schedulePathSnapshot(at, kp)
		}
	}
}

// schedulePathSnapshot takes a snapshot of kp as of at, off the caller, unless one is
// already in flight -- the next long read at the path asks again -- or the store is
// closing.
func (s *Storage) schedulePathSnapshot(at int64, kp string) {
	if s.closing.Load() || !s.pathSnapBusy.CompareAndSwap(false, true) {
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
// snapshot in the inactive log, another snapshot holds the log, or the subtree turns out
// larger than the budget. None is an error; each is a snapshot not worth taking.
func (s *Storage) snapshotPath(at int64, kp string) error {
	s.snapMu.Lock()
	defer s.snapMu.Unlock()

	if root, ok := s.index.SnapshotAtOrAbove("", math.MaxInt64); ok && at < root.StartCommit {
		return nil
	}
	if have, ok := s.index.SnapshotAtOrAbove(kp, at); ok && have.StartCommit == at {
		return nil
	}

	c, err := s.openRead(at, nil, kp, time.Now(), false)
	if err != nil {
		return err
	}
	defer c.Close()
	if c.Presence() == Absent {
		return nil
	}

	timestamp := time.Now().UTC().Format(time.RFC3339)
	w, err := s.dLog.NewSnapshotWriter(at, timestamp)
	if errors.Is(err, dlog.ErrSnapshotInProgress) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("snapshot of %q: %w", kp, err)
	}
	w.SetScopeID(nil)
	w.SetPath(kp)

	builder, err := snap.NewBuilder(w, &snap.Index{}, nil)
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
		if emitted += eventSize(ev); emitted > s.pathSnap.bytes {
			w.Abandon()
			return nil
		}
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
