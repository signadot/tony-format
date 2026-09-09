package storage

import (
	"sync/atomic"
	"time"

	"github.com/signadot/tony-format/go-tony/debug"
)

// ReadKind is what a read at a path did: it read the subtree, or it read the whole
// document because something stopped it narrowing.
type ReadKind string

const (
	ReadNarrow           ReadKind = "narrow"            // read the subtree
	ReadNarrowAbsent     ReadKind = "narrow-absent"     // never written, answered from the index
	ReadWideRoot         ReadKind = "wide-root"         // the read is at the root, which is not a narrowing
	ReadWideBadPath      ReadKind = "wide-bad-path"     // the path does not parse; the wide read reports it
	ReadWideOperator     ReadKind = "wide-operator"     // an operator above the path
	ReadWideAbsent       ReadKind = "wide-absent"       // nothing at the path; the wide read says which kind of nothing
	ReadWideNonFieldPath ReadKind = "wide-keyed-or-idx" // a keyed or indexed segment, which the wide read answers
)

// ReadStats counts what reads at a path did, since a store cannot be asked
// afterwards and the answer is the difference between a fix that engaged and one that
// did not. Reported with each snapshot, so a long-running store says so in its log
// without being restarted under a flag; O_DEBUG_READ says it per read.
type ReadStats struct {
	Narrow         int64
	NarrowAbsent   int64
	WideRoot       int64
	WideBadPath    int64
	WideOperator   int64
	WideAbsent     int64
	WideNonField   int64
	NarrowDuration time.Duration
	WideDuration   time.Duration

	// The bound, in the terms read_write_interface.md states it: what reads emitted,
	// the largest single record any read had to hold to do so, and whether the seek
	// found a snapshot at or above the path. A bound that is not measured is a comment;
	// these are what says the working set of a read is the answer's size and one record,
	// and not the history beneath the path.
	BytesEmitted  int64
	LargestRecord int64
	SeekHit       int64
	SeekMiss      int64
	// SeekPath counts the hits that landed on a snapshot of a path rather than the root;
	// Folded is the records folded after every seek, and LongestTail the most one read
	// folded; PathSnapshots is how many snapshots reads have taken (path_snapshot.go).
	SeekPath      int64
	Folded        int64
	LongestTail   int64
	PathSnapshots int64

	// The SCOPE term of a scoped read (scope_plan.md). Scope counts the scoped reads and
	// ScopeWide those at the root; ScopeFolded is the scope's entries reads have folded
	// after baseline; ScopeFootprint is the live statements the footprint answered for
	// them, which is the bound the fold is held to; ScopeSkipped the statements a read was
	// handed and did not fold (zero; a non-zero is a defect in the footprint's
	// maintenance); ScopeHistoric the scoped reads at a commit behind a statement that has
	// since retired others, served from the history instead (cursor.go, projectScope).
	Scope          int64
	ScopeWide      int64
	ScopeFolded    int64
	ScopeFootprint int64
	ScopeSkipped   int64
	ScopeHistoric  int64

	// Why a wanted path snapshot was not taken; see readStats.
	SnapNotYetWorth      int64
	SnapWantedIncomplete int64
	SnapBusy             int64
	SnapRootAhead        int64
	SnapAlreadyHave      int64
	SnapAbsent           int64
	SnapInProgress       int64
}

// seekKind is what a read's seek found: nothing, the root snapshot, a snapshot of a path
// at or above the read's, or the index's own proof that the path was never written.
type seekKind int

const (
	seekMiss seekKind = iota
	seekRoot
	seekPath
	seekProven
)

type readStats struct {
	narrow         atomic.Int64
	narrowAbsent   atomic.Int64
	wideRoot       atomic.Int64
	wideBadPath    atomic.Int64
	wideOperator   atomic.Int64
	wideAbsent     atomic.Int64
	wideNonField   atomic.Int64
	narrowDuration atomic.Int64
	wideDuration   atomic.Int64
	bytesEmitted   atomic.Int64
	largestRecord  atomic.Int64
	seekHit        atomic.Int64
	seekMiss       atomic.Int64
	seekPath       atomic.Int64
	folded         atomic.Int64
	longestTail    atomic.Int64
	pathSnapshots  atomic.Int64

	// Why a read that wanted a snapshot at its path did not get one. A policy that
	// declines silently is a policy nobody can debug: the fold grows, reads slow, and
	// nothing says which gate is closed. One counter per branch, reported.
	snapNotYetWorth      atomic.Int64 // the fold is not yet long enough for a subtree this size
	snapWantedIncomplete atomic.Int64 // the read did not run to its end
	snapBusy             atomic.Int64 // another path snapshot was already in flight
	snapRootAhead        atomic.Int64 // the read's commit precedes the root snapshot
	snapAlreadyHave      atomic.Int64 // a snapshot at or above the path already stands there
	snapAbsent           atomic.Int64 // the path holds nothing
	snapInProgress       atomic.Int64 // the log was busy with another snapshot

	// The scope term of a scoped read; see ReadStats.
	scope          atomic.Int64
	scopeWide      atomic.Int64
	scopeFolded    atomic.Int64
	scopeFootprint atomic.Int64
	scopeSkipped   atomic.Int64
	scopeHistoric  atomic.Int64
}

// noteScope records the scope term of one scoped read: how many of the scope's entries it
// folded after baseline, how many live statements the footprint answered for it, whether
// it was served from the history rather than the footprint, and whether it was at the
// root. A footprint read that folded fewer entries than the statements it was handed
// skipped some, which is a defect and is counted so it is seen.
func (r *readStats) noteScope(kp string, folded, live int64, historic bool) {
	r.scope.Add(1)
	if kp == "" {
		r.scopeWide.Add(1)
	}
	r.scopeFolded.Add(folded)
	if historic {
		r.scopeHistoric.Add(1)
		return
	}
	r.scopeFootprint.Add(live)
	if live > folded {
		r.scopeSkipped.Add(live - folded)
	}
}

// noteBound records one read against the bound: the bytes it emitted, the largest record
// it buffered, what its seek found, and how many records it folded after it.
func (r *readStats) noteBound(emitted, largest int64, seek seekKind, tail int64) {
	r.bytesEmitted.Add(emitted)
	raiseTo(&r.largestRecord, largest)
	raiseTo(&r.longestTail, tail)
	r.folded.Add(tail)
	switch seek {
	case seekMiss:
		r.seekMiss.Add(1)
	case seekPath:
		r.seekHit.Add(1)
		r.seekPath.Add(1)
	default:
		r.seekHit.Add(1)
	}
}

// raiseTo sets a gauge to v if v is larger.
func raiseTo(g *atomic.Int64, v int64) {
	for {
		cur := g.Load()
		if v <= cur || g.CompareAndSwap(cur, v) {
			return
		}
	}
}

// note records one read at a path, and says so when O_DEBUG_READ is set.
func (r *readStats) note(kind ReadKind, path string, took time.Duration) {
	switch kind {
	case ReadNarrow:
		r.narrow.Add(1)
		r.narrowDuration.Add(int64(took))
	case ReadNarrowAbsent:
		r.narrowAbsent.Add(1)
	case ReadWideRoot:
		r.wideRoot.Add(1)
	case ReadWideBadPath:
		r.wideBadPath.Add(1)
	case ReadWideOperator:
		r.wideOperator.Add(1)
	case ReadWideAbsent:
		r.wideAbsent.Add(1)
	case ReadWideNonFieldPath:
		r.wideNonField.Add(1)
	}
	if kind != ReadNarrow && kind != ReadNarrowAbsent {
		r.wideDuration.Add(int64(took))
	}
	if debug.Read() {
		debug.Logf("logd read path=%q %s in %s\n", path, kind, took.Round(time.Microsecond))
	}
}

func (r *readStats) snapshot() ReadStats {
	return ReadStats{
		Narrow:         r.narrow.Load(),
		NarrowAbsent:   r.narrowAbsent.Load(),
		WideRoot:       r.wideRoot.Load(),
		WideBadPath:    r.wideBadPath.Load(),
		WideOperator:   r.wideOperator.Load(),
		WideAbsent:     r.wideAbsent.Load(),
		WideNonField:   r.wideNonField.Load(),
		NarrowDuration: time.Duration(r.narrowDuration.Load()),
		WideDuration:   time.Duration(r.wideDuration.Load()),
		BytesEmitted:   r.bytesEmitted.Load(),
		LargestRecord:  r.largestRecord.Load(),
		SeekHit:        r.seekHit.Load(),
		SeekMiss:       r.seekMiss.Load(),
		SeekPath:       r.seekPath.Load(),
		Folded:         r.folded.Load(),
		LongestTail:    r.longestTail.Load(),
		PathSnapshots:  r.pathSnapshots.Load(),

		SnapNotYetWorth:      r.snapNotYetWorth.Load(),
		SnapWantedIncomplete: r.snapWantedIncomplete.Load(),
		SnapBusy:             r.snapBusy.Load(),
		SnapRootAhead:        r.snapRootAhead.Load(),
		SnapAlreadyHave:      r.snapAlreadyHave.Load(),
		SnapAbsent:           r.snapAbsent.Load(),
		SnapInProgress:       r.snapInProgress.Load(),

		Scope:          r.scope.Load(),
		ScopeWide:      r.scopeWide.Load(),
		ScopeFolded:    r.scopeFolded.Load(),
		ScopeFootprint: r.scopeFootprint.Load(),
		ScopeSkipped:   r.scopeSkipped.Load(),
		ScopeHistoric:  r.scopeHistoric.Load(),
	}
}

// ReadStats answers what reads at a path have done so far. A caller wondering
// whether a narrow read is engaging asks this rather than timing from outside,
// where a fast read and a read that never happened look the same.
func (s *Storage) ReadStats() ReadStats {
	return s.readStats.snapshot()
}

// Report renders the counters for an operator: what reads at a path did, and where
// the time went. The names are the question a reader asks -- did it narrow, and if
// not why -- rather than the fields' own.
func (r ReadStats) Report() map[string]any {
	wide := r.WideRoot + r.WideBadPath + r.WideOperator + r.WideAbsent + r.WideNonField
	m := map[string]any{
		"reads.narrow":                      r.Narrow,
		"reads.narrow.absent":               r.NarrowAbsent,
		"reads.wide":                        wide,
		"reads.wide.root":                   r.WideRoot,
		"reads.wide.operator":               r.WideOperator,
		"reads.wide.absent":                 r.WideAbsent,
		"reads.wide.keyed-or-idx":           r.WideNonField,
		"reads.wide.bad-path":               r.WideBadPath,
		"reads.bytes":                       r.BytesEmitted,
		"reads.record.max":                  r.LargestRecord,
		"reads.seek.hit":                    r.SeekHit,
		"reads.seek.miss":                   r.SeekMiss,
		"reads.seek.path":                   r.SeekPath,
		"reads.folded":                      r.Folded,
		"reads.tail.max":                    r.LongestTail,
		"snapshots.path":                    r.PathSnapshots,
		"snapshots.path.no.not-yet-worth":   r.SnapNotYetWorth,
		"snapshots.path.no.incomplete-read": r.SnapWantedIncomplete,
		"snapshots.path.no.busy":            r.SnapBusy,
		"snapshots.path.no.root-ahead":      r.SnapRootAhead,
		"snapshots.path.no.already-have":    r.SnapAlreadyHave,
		"snapshots.path.no.absent":          r.SnapAbsent,
		"snapshots.path.no.log-busy":        r.SnapInProgress,
		"reads.scope":                       r.Scope,
		"reads.scope.wide":                  r.ScopeWide,
		"reads.scope.folded":                r.ScopeFolded,
		"reads.scope.footprint":             r.ScopeFootprint,
		"reads.scope.skipped":               r.ScopeSkipped,
		"reads.scope.historic":              r.ScopeHistoric,
	}
	if r.Narrow > 0 {
		m["reads.narrow.avg"] = (r.NarrowDuration / time.Duration(r.Narrow)).Round(time.Microsecond).String()
	}
	if wide > 0 {
		m["reads.wide.avg"] = (r.WideDuration / time.Duration(wide)).Round(time.Microsecond).String()
	}
	return m
}
