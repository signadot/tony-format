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
	ReadWideRoot         ReadKind = "wide-root"         // the read is at the root, which is not a narrowing
	ReadWideScope        ReadKind = "wide-scope"        // a scoped read, which is one op-preserving pass
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
	WideRoot       int64
	WideScope      int64
	WideBadPath    int64
	WideOperator   int64
	WideAbsent     int64
	WideNonField   int64
	NarrowDuration time.Duration
	WideDuration   time.Duration
}

type readStats struct {
	narrow         atomic.Int64
	wideRoot       atomic.Int64
	wideScope      atomic.Int64
	wideBadPath    atomic.Int64
	wideOperator   atomic.Int64
	wideAbsent     atomic.Int64
	wideNonField   atomic.Int64
	narrowDuration atomic.Int64
	wideDuration   atomic.Int64
}

// note records one read at a path, and says so when O_DEBUG_READ is set.
func (r *readStats) note(kind ReadKind, path string, took time.Duration) {
	switch kind {
	case ReadNarrow:
		r.narrow.Add(1)
		r.narrowDuration.Add(int64(took))
	case ReadWideRoot:
		r.wideRoot.Add(1)
	case ReadWideScope:
		r.wideScope.Add(1)
	case ReadWideBadPath:
		r.wideBadPath.Add(1)
	case ReadWideOperator:
		r.wideOperator.Add(1)
	case ReadWideAbsent:
		r.wideAbsent.Add(1)
	case ReadWideNonFieldPath:
		r.wideNonField.Add(1)
	}
	if kind != ReadNarrow {
		r.wideDuration.Add(int64(took))
	}
	if debug.Read() {
		debug.Logf("logd read path=%q %s in %s\n", path, kind, took.Round(time.Microsecond))
	}
}

func (r *readStats) snapshot() ReadStats {
	return ReadStats{
		Narrow:         r.narrow.Load(),
		WideRoot:       r.wideRoot.Load(),
		WideScope:      r.wideScope.Load(),
		WideBadPath:    r.wideBadPath.Load(),
		WideOperator:   r.wideOperator.Load(),
		WideAbsent:     r.wideAbsent.Load(),
		WideNonField:   r.wideNonField.Load(),
		NarrowDuration: time.Duration(r.narrowDuration.Load()),
		WideDuration:   time.Duration(r.wideDuration.Load()),
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
	wide := r.WideRoot + r.WideScope + r.WideBadPath + r.WideOperator + r.WideAbsent + r.WideNonField
	m := map[string]any{
		"reads.narrow":            r.Narrow,
		"reads.wide":              wide,
		"reads.wide.root":         r.WideRoot,
		"reads.wide.scope":        r.WideScope,
		"reads.wide.operator":     r.WideOperator,
		"reads.wide.absent":       r.WideAbsent,
		"reads.wide.keyed-or-idx": r.WideNonField,
		"reads.wide.bad-path":     r.WideBadPath,
	}
	if r.Narrow > 0 {
		m["reads.narrow.avg"] = (r.NarrowDuration / time.Duration(r.Narrow)).Round(time.Microsecond).String()
	}
	if wide > 0 {
		m["reads.wide.avg"] = (r.WideDuration / time.Duration(wide)).Round(time.Microsecond).String()
	}
	return m
}
