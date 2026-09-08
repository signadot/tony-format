package index

import (
	"sort"
	"sync"
	"sync/atomic"
)

// Index residency (index_residency.md; rebuild_plan.md phase 6).
//
// THE DURABLE INDEX IS THE TRUTH AND THE RESIDENT TRIE IS A CACHE OF IT. The durable form
// is a regions file (regions_file.go), itself derived from the log and rebuilt from it when
// it is missing; the resident form is the trie skeleton -- every path ever written, which
// is the floor -- with each node's segments held in REGIONS. A region is (node, commit
// range): a slice of one node's history of at most regionCap segments, contiguous in
// StartCommit, and the unit that is admitted, counted, touched and evicted. A node's
// regions partition its commit axis; a segment belongs to the region whose range holds
// its StartCommit.
//
// A region is RESIDENT when its segments are in the node's tree, and COLD when they are
// only in the regions file. A read that needs a cold region pages it in, under the node's
// lock, and never sees a shorter list than the index describes -- which is the invariant
// every policy is safe under: EVICTION CHANGES A COST, NEVER AN ANSWER. What makes it
// hold is what may be evicted: only a region that is durable and clean, whose segments
// the file holds exactly as the tree does. A hot region -- one written to since the
// persister last wrote it, or never written -- is resident until it is persisted.
//
// The policy is LRU over regions and the ceiling is configured; a read at the head hits,
// a read deep in history may pay I/O to the file, and that gradient is the intended shape.
// Every region becomes resident through admit and is counted there, and there is no other
// way to become resident. A region admitted past the ceiling when nothing is evictable --
// every resident region hot, or pinned by the read in progress -- is admitted anyway,
// since a read must be answered and a write must land, and the excess is reported as
// index.over; the persister is what turns hot regions into evictable ones.
//
// Locks, in one order: a node's lock is never held while the residency's is taken. Paging
// reads the file with no lock held, then takes the node's write lock to install what it
// read and to answer the read that asked for it in the same critical section; eviction
// takes the residency's lock to choose and the node's to remove, and a reader in progress
// holds the node's lock, so it cannot lose what it is reading.

// regionCap is the most segments a region holds before it splits.
const regionCap = 64

// bytesPerSegment is what one resident segment is charged: the struct, its share of the
// tree, and its share of the node. Measured on the staging index at 249 bytes per segment
// across 4.5M segments (2c1d653), and rounded up.
const bytesPerSegment = 256

// MinIndexCeiling is the smallest ceiling a store accepts: below it a read at any depth
// thrashes rather than progresses, and it is refused rather than discovered.
const MinIndexCeiling = 8 * regionCap * bytesPerSegment

type region struct {
	node     *Index
	minStart int64 // the range is [minStart, next region's minStart); the first region's starts at its own minStart
	maxStart int64 // the greatest StartCommit present; exact while resident, and as persisted while cold
	maxEnd   int64 // the greatest EndCommit present; may overstate after a removal, which costs a page-in and never an answer
	maxTx    int64
	count    int
	snaps    []int64 // commits of the baseline snapshots in it, ascending: what the seek asks first

	resident bool
	dirty    bool // resident and different from what rec holds
	version  uint64
	rec      *recordRef // where the file holds it; nil until persisted

	prev, next *region // the residency's LRU list, evictable regions only
	listed     bool
}

func (r *region) bytes() int64 { return int64(r.count) * bytesPerSegment }

// covers says whether the region can hold segments with EndCommit in [from, to]: it holds
// some StartCommit at or below to, and some EndCommit at or above from.
func (r *region) covers(from, to *int64) bool {
	if to != nil && r.minStart > *to {
		return false
	}
	if from != nil && r.maxEnd < *from {
		return false
	}
	return true
}

// Residency is the store-wide accounting and policy: what is resident, the ceiling, and
// the order regions leave in.
type Residency struct {
	mu       sync.Mutex
	ceiling  int64
	resident int64
	head     *region // least recently used
	tail     *region // most recently used
	listed   int

	hits, misses, evictions, over atomic.Int64
	file                          *regionsFile
}

// NewResidency answers an unbounded residency: nothing is evicted until SetCeiling.
func NewResidency() *Residency { return &Residency{} }

// SetCeiling sets the ceiling in bytes, 0 for unbounded, and evicts down to it.
func (r *Residency) SetCeiling(bytes int64) {
	r.mu.Lock()
	r.ceiling = bytes
	r.mu.Unlock()
	r.evictToFit(0)
}

// Ceiling answers the configured ceiling, 0 for unbounded.
func (r *Residency) Ceiling() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ceiling
}

// ResidencyStats is what the store says about its index.
type ResidencyStats struct {
	Resident  int64 // bytes charged for resident regions
	Ceiling   int64 // 0 is unbounded
	Evictable int   // resident regions the policy may evict: durable and clean
	Hits      int64 // reads whose regions were resident
	Misses    int64 // regions paged in
	Evictions int64
	Over      int64 // the most bytes the residency stood over its ceiling with nothing evictable
}

func (r *Residency) Stats() ResidencyStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	return ResidencyStats{
		Resident:  r.resident,
		Ceiling:   r.ceiling,
		Evictable: r.listed,
		Hits:      r.hits.Load(),
		Misses:    r.misses.Load(),
		Evictions: r.evictions.Load(),
		Over:      r.over.Load(),
	}
}

// Report renders the counters for an operator.
func (s ResidencyStats) Report() map[string]any {
	m := map[string]any{
		"index.resident.bytes": s.Resident,
		"index.ceiling":        s.Ceiling,
		"index.evictable":      s.Evictable,
		"index.hits":           s.Hits,
		"index.misses":         s.Misses,
		"index.evictions":      s.Evictions,
		"index.over.bytes":     s.Over,
	}
	if s.Hits+s.Misses > 0 {
		m["index.hit.rate"] = float64(s.Hits) / float64(s.Hits+s.Misses)
	}
	return m
}

// charge accounts bytes that became resident, and evicts what the policy allows to make
// room. Called with no node lock held.
func (r *Residency) charge(bytes int64) {
	r.mu.Lock()
	r.resident += bytes
	r.mu.Unlock()
	r.evictToFit(0)
}

// release accounts bytes that stopped being resident.
func (r *Residency) release(bytes int64) {
	r.mu.Lock()
	r.resident -= bytes
	r.mu.Unlock()
}

// touch marks a region most recently used, if it is in the list.
func (r *Residency) touch(reg *region) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reg.listed {
		r.unlinkLocked(reg)
		r.pushLocked(reg)
	}
}

// list puts a region among the evictable: it is durable and clean.
func (r *Residency) list(reg *region) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !reg.listed {
		r.pushLocked(reg)
	}
}

// unlist takes a region out of the evictable: it was written to, or evicted, or dropped.
func (r *Residency) unlist(reg *region) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reg.listed {
		r.unlinkLocked(reg)
	}
}

func (r *Residency) pushLocked(reg *region) {
	reg.prev, reg.next = r.tail, nil
	if r.tail != nil {
		r.tail.next = reg
	} else {
		r.head = reg
	}
	r.tail = reg
	reg.listed = true
	r.listed++
}

func (r *Residency) unlinkLocked(reg *region) {
	if reg.prev != nil {
		reg.prev.next = reg.next
	} else {
		r.head = reg.next
	}
	if reg.next != nil {
		reg.next.prev = reg.prev
	} else {
		r.tail = reg.prev
	}
	reg.prev, reg.next = nil, nil
	reg.listed = false
	r.listed--
}

// evictToFit evicts least recently used regions until resident plus incoming fits under
// the ceiling, or nothing evictable remains, in which case the excess is recorded.
func (r *Residency) evictToFit(incoming int64) {
	for {
		r.mu.Lock()
		if r.ceiling <= 0 || r.resident+incoming <= r.ceiling {
			r.mu.Unlock()
			return
		}
		victim := r.head
		if victim == nil {
			if excess := r.resident + incoming - r.ceiling; excess > r.over.Load() {
				r.over.Store(excess)
			}
			r.mu.Unlock()
			return
		}
		r.unlinkLocked(victim)
		r.mu.Unlock()
		if victim.node.evict(victim) {
			r.evictions.Add(1)
		}
	}
}

// --- the node's side ---

// regionFor answers the region whose range holds startCommit, under the node's lock; nil
// when the node has none yet.
func (i *Index) regionFor(startCommit int64) *region {
	n := len(i.regions)
	if n == 0 {
		return nil
	}
	// The last region whose minStart <= startCommit; the first takes anything below it.
	k := sort.Search(n, func(k int) bool { return i.regions[k].minStart > startCommit })
	if k == 0 {
		return i.regions[0]
	}
	return i.regions[k-1]
}

// missing answers the cold regions among those satisfying pred, under the node's lock.
func (i *Index) missing(pred func(*region) bool) []*region {
	var cold []*region
	for _, reg := range i.regions {
		if !reg.resident && (pred == nil || pred(reg)) {
			cold = append(cold, reg)
		}
	}
	return cold
}

// touched notes the resident regions a read used, so the policy sees them.
func (i *Index) touched(pred func(*region) bool) {
	if i.res == nil {
		return
	}
	i.RLock()
	var used []*region
	for _, reg := range i.regions {
		if reg.resident && reg.listed && (pred == nil || pred(reg)) {
			used = append(used, reg)
		}
	}
	i.RUnlock()
	for _, reg := range used {
		i.res.touch(reg)
	}
	if len(used) > 0 {
		i.res.hits.Add(1)
	}
}

// withResident runs fn under the node's lock with every region satisfying pred resident:
// under the read lock (the write lock, when fn writes) when nothing is missing, and
// otherwise -- after the missing regions are read from the file with no lock held --
// under the write lock, in the same critical section that installs them, so an eviction
// cannot come between. fn sees the tree.
func (i *Index) withResident(pred func(*region) bool, write bool, fn func()) {
	lock, unlock := i.RLocker().Lock, i.RLocker().Unlock
	if write {
		lock, unlock = i.Lock, i.Unlock
	}
	for {
		lock()
		cold := i.missing(pred)
		if len(cold) == 0 {
			fn()
			unlock()
			i.touched(pred)
			return
		}
		unlock()

		loaded := make(map[*region][]LogSegment, len(cold))
		for _, reg := range cold {
			segs, err := i.res.file.read(reg.rec)
			if err != nil {
				// The file is the truth and it cannot be read: the invariant cannot be
				// kept, and saying so loudly beats a shorter answer.
				panic("index: cannot page in a region: " + err.Error())
			}
			loaded[reg] = segs
		}
		i.Lock()
		var charged int64
		for reg, segs := range loaded {
			if reg.resident || reg.node != i {
				continue // paged by another reader, or split away meanwhile
			}
			for _, s := range segs {
				i.Commits.Insert(s)
			}
			reg.resident = true
			reg.dirty = false
			charged += reg.bytes()
		}
		if again := i.missing(pred); len(again) > 0 {
			// A region split or arrived while the file was being read; go round.
			i.Unlock()
			if charged > 0 {
				i.res.charge(charged)
			}
			cold = again
			continue
		}
		fn()
		var listable []*region
		for reg := range loaded {
			if reg.resident && !reg.dirty && reg.rec != nil && reg.node == i {
				listable = append(listable, reg)
			}
		}
		i.Unlock()
		i.res.misses.Add(int64(len(loaded)))
		for _, reg := range listable {
			i.res.list(reg)
		}
		if charged > 0 {
			i.res.charge(charged)
		}
		i.touched(pred)
		return
	}
}

// removeAll pages the whole node in and removes every segment match accepts, answering
// how many went. It is what the repairs and DeleteScope do to a node.
func (i *Index) removeAll(match func(LogSegment) bool) int {
	removed := 0
	var affected []*region
	i.withResident(nil, true, func() {
		var doomed []LogSegment
		i.Commits.All(func(c LogSegment) bool {
			if match(c) {
				doomed = append(doomed, c)
			}
			return true
		})
		seen := map[*region]bool{}
		for _, c := range doomed {
			if i.Commits.Remove(c) {
				removed++
				if reg := i.regionFor(c.StartCommit); reg != nil {
					i.noteRemoved(reg, c)
					if !seen[reg] {
						seen[reg] = true
						affected = append(affected, reg)
					}
				}
			}
		}
	})
	if i.res != nil && removed > 0 {
		for _, reg := range affected {
			i.res.unlist(reg)
		}
		i.res.release(int64(removed) * bytesPerSegment)
	}
	return removed
}

// addSegment puts one segment in this node, into the region its StartCommit falls in --
// paged in first if it is cold, which only a re-add of an old commit reaches -- in the
// same critical section as the paging: the charge for a page-in may evict the region
// again, when it is the only evictable one, so the write must have landed before it.
func (i *Index) addSegment(s LogSegment) {
	added := false
	var reg, upper *region
	i.withResident(func(r *region) bool { return i.ownsStart(r, s.StartCommit) }, true, func() {
		reg = i.regionFor(s.StartCommit)
		if reg == nil {
			reg = &region{node: i, minStart: s.StartCommit, resident: true, dirty: true}
			i.regions = append(i.regions, reg)
		}
		if s.StartCommit < reg.minStart {
			reg.minStart = s.StartCommit // the first region takes what lies below it
		}
		if !i.Commits.Insert(s) {
			return // already there; nothing changed
		}
		added = true
		upper = i.noteAdded(reg, s)
	})
	if added && i.res != nil {
		i.res.unlist(reg)
		if upper != nil {
			i.res.unlist(upper)
		}
		i.res.charge(bytesPerSegment)
	}
}

// removeSegment takes one segment out of this node, paging its region in if it is cold.
func (i *Index) removeSegment(s LogSegment) bool {
	removed := false
	var reg *region
	i.withResident(func(r *region) bool { return i.ownsStart(r, s.StartCommit) }, true, func() {
		reg = i.regionFor(s.StartCommit)
		if reg == nil {
			return
		}
		if i.Commits.Remove(s) {
			removed = true
			i.noteRemoved(reg, s)
		}
	})
	if removed && i.res != nil {
		i.res.unlist(reg)
		i.res.release(bytesPerSegment)
	}
	return removed
}

// evict drops a region's segments from the tree, if it is still evictable. Called by the
// residency with no lock held.
func (i *Index) evict(reg *region) bool {
	i.Lock()
	if !reg.resident || reg.dirty || reg.rec == nil || reg.node != i {
		i.Unlock()
		return false
	}
	var doomed []LogSegment
	lo, hi := reg.minStart, reg.maxStart
	i.Commits.Range(func(s LogSegment) bool {
		doomed = append(doomed, s)
		return true
	}, func(s LogSegment) int {
		if s.StartCommit < lo {
			return -1
		}
		if s.StartCommit > hi {
			return 1
		}
		return 0
	})
	for _, s := range doomed {
		i.Commits.Remove(s)
	}
	reg.resident = false
	bytes := reg.bytes()
	i.Unlock()
	i.res.release(bytes)
	return true
}

// noteAdded keeps a region's bookkeeping after a segment joined it, under the node's
// lock, and splits it when it is over the cap.
func (i *Index) noteAdded(reg *region, s LogSegment) (split *region) {
	reg.count++
	if s.StartCommit > reg.maxStart {
		reg.maxStart = s.StartCommit
	}
	if s.EndCommit > reg.maxEnd {
		reg.maxEnd = s.EndCommit
	}
	if s.EndTx > reg.maxTx {
		reg.maxTx = s.EndTx
	}
	if s.StartCommit == s.EndCommit && s.ScopeID == nil {
		k := sort.Search(len(reg.snaps), func(k int) bool { return reg.snaps[k] >= s.StartCommit })
		if k == len(reg.snaps) || reg.snaps[k] != s.StartCommit {
			reg.snaps = append(reg.snaps, 0)
			copy(reg.snaps[k+1:], reg.snaps[k:])
			reg.snaps[k] = s.StartCommit
		}
	}
	reg.dirty = true
	reg.version++
	if reg.count > regionCap {
		return i.splitRegion(reg)
	}
	return nil
}

// splitRegion halves a resident region at a StartCommit boundary, under the node's lock.
// Both halves are dirty: the file holds neither yet.
func (i *Index) splitRegion(reg *region) *region {
	segs := i.segmentsOf(reg)
	// Distinct StartCommits, so that no StartCommit straddles two regions.
	var starts []int64
	for _, s := range segs {
		if len(starts) == 0 || starts[len(starts)-1] != s.StartCommit {
			starts = append(starts, s.StartCommit)
		}
	}
	if len(starts) < 2 {
		return nil // one StartCommit; it cannot be split, and it is bounded by the entries at one commit
	}
	cut := starts[len(starts)/2]
	upper := &region{node: i, minStart: cut, resident: true, dirty: true}
	reg.count, reg.maxStart, reg.maxEnd, reg.maxTx = 0, 0, 0, 0
	var lowerSnaps, upperSnaps []int64
	for _, c := range reg.snaps {
		if c < cut {
			lowerSnaps = append(lowerSnaps, c)
		} else {
			upperSnaps = append(upperSnaps, c)
		}
	}
	reg.snaps, upper.snaps = lowerSnaps, upperSnaps
	for _, s := range segs {
		target := reg
		if s.StartCommit >= cut {
			target = upper
		}
		target.count++
		if s.StartCommit > target.maxStart {
			target.maxStart = s.StartCommit
		}
		if s.EndCommit > target.maxEnd {
			target.maxEnd = s.EndCommit
		}
		if s.EndTx > target.maxTx {
			target.maxTx = s.EndTx
		}
	}
	reg.dirty = true
	reg.version++
	k := sort.Search(len(i.regions), func(k int) bool { return i.regions[k].minStart > cut })
	i.regions = append(i.regions, nil)
	copy(i.regions[k+1:], i.regions[k:])
	i.regions[k] = upper
	return upper
}

// segmentsOf answers a resident region's segments from the tree, ascending, under the
// node's lock.
func (i *Index) segmentsOf(reg *region) []LogSegment {
	var out []LogSegment
	lo, hi := reg.minStart, reg.maxStart
	i.Commits.Range(func(s LogSegment) bool {
		out = append(out, s)
		return true
	}, func(s LogSegment) int {
		if s.StartCommit < lo {
			return -1
		}
		if s.StartCommit > hi {
			return 1
		}
		return 0
	})
	return out
}

// noteRemoved keeps a region's bookkeeping after a segment left it, under the node's
// lock. A removed maximum is recomputed from the tree, so newestCommit stays exact.
func (i *Index) noteRemoved(reg *region, s LogSegment) {
	reg.count--
	reg.dirty = true
	reg.version++
	if s.StartCommit == s.EndCommit && s.ScopeID == nil {
		k := sort.Search(len(reg.snaps), func(k int) bool { return reg.snaps[k] >= s.StartCommit })
		if k < len(reg.snaps) && reg.snaps[k] == s.StartCommit {
			reg.snaps = append(reg.snaps[:k], reg.snaps[k+1:]...)
		}
	}
	if s.StartCommit == reg.maxStart || s.EndCommit == reg.maxEnd || s.EndTx == reg.maxTx {
		reg.maxStart, reg.maxEnd, reg.maxTx = 0, 0, 0
		if reg.count == 0 {
			return
		}
		for _, t := range i.segmentsOf(&region{minStart: reg.minStart, maxStart: s.StartCommit + (1 << 62)}) {
			if !i.ownsStart(reg, t.StartCommit) {
				break
			}
			if t.StartCommit > reg.maxStart {
				reg.maxStart = t.StartCommit
			}
			if t.EndCommit > reg.maxEnd {
				reg.maxEnd = t.EndCommit
			}
			if t.EndTx > reg.maxTx {
				reg.maxTx = t.EndTx
			}
		}
	}
}

// ownsStart says whether startCommit falls in reg's range, under the node's lock.
func (i *Index) ownsStart(reg *region, startCommit int64) bool {
	return i.regionFor(startCommit) == reg
}

// newestStart is the greatest StartCommit the node holds, resident or cold, under the
// node's lock.
func (i *Index) newestStart() (int64, bool) {
	for k := len(i.regions) - 1; k >= 0; k-- {
		if i.regions[k].count > 0 {
			return i.regions[k].maxStart, true
		}
	}
	return 0, false
}

// MaxCommit answers the greatest EndCommit and EndTx the whole index describes, from the
// regions' headers: nothing is paged in to say it.
func (i *Index) MaxCommit() (commit, txSeq int64, ok bool) {
	i.RLock()
	for _, reg := range i.regions {
		if reg.count == 0 {
			continue
		}
		ok = true
		if reg.maxEnd > commit {
			commit = reg.maxEnd
		}
		if reg.maxTx > txSeq {
			txSeq = reg.maxTx
		}
	}
	i.RUnlock()
	for _, child := range i.childrenOf() {
		c, t, has := child.index.MaxCommit()
		if !has {
			continue
		}
		ok = true
		if c > commit {
			commit = c
		}
		if t > txSeq {
			txSeq = t
		}
	}
	return commit, txSeq, ok
}

// Residency answers the index's residency, nil for an unbounded index with no file.
func (i *Index) Residency() *Residency { return i.res }
