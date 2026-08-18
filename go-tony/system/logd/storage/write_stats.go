package storage

import (
	"sync/atomic"
	"time"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// slowCommit is the commit duration worth a line in the log on its own. A write is
// milliseconds when nothing is wrong, so this is far above the ordinary and far below
// a client's patience -- the point is to name the phase before the client gives up,
// not after.
const slowCommit = 250 * time.Millisecond

// writeStats records what a commit spent its time on. Reads got counters first, and
// the first thing they proved was that the expensive case was not the suspected one
// (ap8ddvp2h12krd43gdn0); writes were still being reasoned about from the outside,
// where a slow write and a queued write look the same.
//
// The phases are the ones a commit can actually be slow in:
//
//	state    getting the document the patch applies to -- the stepped head, or a
//	         FULL READ when the head is not there to step (see head.go). A store
//	         missing its head pays a whole read PER WRITE, which is the thing this
//	         exists to make visible.
//	apply    folding the patch onto it, which also verifies the write applies
//	appendLog  appending the entry, including the fsync under DurabilitySync
//	index    indexing the patch at every path inside it
type writeStats struct {
	commits   atomic.Int64
	headHits  atomic.Int64
	headMiss  atomic.Int64
	slow      atomic.Int64
	state     atomic.Int64
	apply     atomic.Int64
	appendLog atomic.Int64
	index     atomic.Int64
	total     atomic.Int64
}

// WriteStats is a snapshot of the counters, for a report.
type WriteStats struct {
	Commits  int64
	HeadHits int64
	HeadMiss int64
	Slow     int64
	State    time.Duration
	Apply    time.Duration
	Append   time.Duration
	Index    time.Duration
	Total    time.Duration
}

// noteHead records where the document a write applies to came from. A miss carries
// what the read cost, because that read is on the write's own path.
func (w *writeStats) noteHead(hit bool, took time.Duration) {
	if hit {
		w.headHits.Add(1)
	} else {
		w.headMiss.Add(1)
	}
	w.state.Add(int64(took))
}

// noteCommit records one commit's phases, and says so when it was slow or when
// O_DEBUG_WRITE is set.
func (s *Storage) noteCommit(path string, apply, appendLog, index, total time.Duration) {
	w := &s.writeStats
	w.commits.Add(1)
	w.apply.Add(int64(apply))
	w.appendLog.Add(int64(appendLog))
	w.index.Add(int64(index))
	w.total.Add(int64(total))

	slow := total >= slowCommit
	if slow {
		w.slow.Add(1)
	}
	if !slow && !debug.Write() {
		return
	}
	if s.logger == nil {
		return
	}
	// The state phase is per-commit only in the miss case, where it dominates; the
	// hit case is a pointer copy. Report the remainder so the line adds up.
	s.logger.Warn("slow commit",
		"path", path,
		"took", total.Round(time.Millisecond),
		"apply", apply.Round(time.Millisecond),
		"append", appendLog.Round(time.Millisecond),
		"index", index.Round(time.Millisecond),
		"other", (total - apply - appendLog - index).Round(time.Millisecond),
		"headMisses", w.headMiss.Load(),
		"commits", w.commits.Load())
}

// patchPathHint names a commit in a log line: the deepest path the patch is a single
// chain down to, which for the one-path writes a reconciler makes is the path it
// wrote. A patch touching several branches stops at the fork, which is honest -- it
// is not one path.
func patchPathHint(patch *ir.Node) string {
	path := ""
	for n := ir.Uncomment(patch); n != nil && n.Type == ir.ObjectType && len(n.Fields) == 1; n = ir.Uncomment(n.Values[0]) {
		f := n.Fields[0]
		if f.Type != ir.StringType {
			break
		}
		path = kpath.ChildField(path, f.String)
	}
	if path == "" {
		return "(root)"
	}
	return path
}

// WriteStats answers what commits have spent their time on so far.
func (s *Storage) WriteStats() WriteStats {
	w := &s.writeStats
	return WriteStats{
		Commits:  w.commits.Load(),
		HeadHits: w.headHits.Load(),
		HeadMiss: w.headMiss.Load(),
		Slow:     w.slow.Load(),
		State:    time.Duration(w.state.Load()),
		Apply:    time.Duration(w.apply.Load()),
		Append:   time.Duration(w.appendLog.Load()),
		Index:    time.Duration(w.index.Load()),
		Total:    time.Duration(w.total.Load()),
	}
}

// Report renders the counters for an operator, in the terms the question is asked in:
// how long a write takes, and which phase has it. writes.head.missed is the one to
// read first -- a miss means that write did a full document read to find out what it
// was patching.
func (w WriteStats) Report() map[string]any {
	m := map[string]any{
		"writes.commits":     w.Commits,
		"writes.slow":        w.Slow,
		"writes.head.hit":    w.HeadHits,
		"writes.head.missed": w.HeadMiss,
	}
	if w.Commits > 0 {
		n := time.Duration(w.Commits)
		m["writes.avg"] = (w.Total / n).Round(time.Microsecond).String()
		m["writes.avg.apply"] = (w.Apply / n).Round(time.Microsecond).String()
		m["writes.avg.append"] = (w.Append / n).Round(time.Microsecond).String()
		m["writes.avg.index"] = (w.Index / n).Round(time.Microsecond).String()
	}
	if w.HeadMiss > 0 {
		m["writes.head.missed.avg"] = (w.State / time.Duration(w.HeadMiss)).Round(time.Microsecond).String()
	}
	return m
}

// StatsReport is what the admin listener shows: reads and writes together, since the
// question an operator arrives with -- why is this slow -- does not come labelled with
// which of the two it is.
func (s *Storage) StatsReport() map[string]any {
	m := s.ReadStats().Report()
	for k, v := range s.WriteStats().Report() {
		m[k] = v
	}
	return m
}
