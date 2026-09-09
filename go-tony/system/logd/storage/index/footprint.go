package index

import (
	"sort"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// The footprint (scope_plan.md, phase 1).
//
// A SCOPE'S SNAPSHOT OF A PATH IS ITS LAST COVERING STATEMENT THERE. A stored scope write
// is absolute, so a later statement of the scope dominates what it covers (Cover), and a
// dominated statement contributes nothing to the fold from the dominating one on. The
// footprint is the index knowing that: per scope, a trie of the paths the scope has stated
// something at, holding at each the scope's LIVE statements -- the ones no later statement
// dominates -- each a reference into the log. A scoped read folds the live statements on
// its path's ancestor chain, at the path and under it, and no more; compaction drops an
// entry beyond the cutoff when none of its statements is live; DeleteScope walks the
// footprint's paths instead of paging the whole index.
//
// It is maintained where the entry is indexed (Add) and unmade where it leaves (Remove),
// persisted in the manifest beside the regions, rebuilt from the log by Build exactly as the
// segments are, and it is resident: its size is the live statements of every scope, which
// for a sandbox is its entities' leaves. THE FOOTPRINT CHANGES A COST, NEVER AN ANSWER: a
// read served from it and a read that folds every entry of the scope agree byte for byte,
// which is the property the scope differential holds.
//
// Dominance at the write, in both directions: a statement arriving after the ones it
// dominates drops them, and a statement arriving after one that dominates IT -- a survivor
// re-indexed after a compaction, an entry replayed out of order by a rebuild that is not --
// is not added. Nothing ever comes back once dropped, which is why a repair that removes
// segments the walk could not read rebuilds the footprint whole (Rebuild).

// A Statement is one live statement of a scope: where it is, when, and how to read it.
type Statement struct {
	Path        string
	Commit      int64
	Tx          int64
	LogFile     string
	LogPosition int64
	Generation  int64
	Offers      Cover
	Needs       Cover
}

// entryRef names a log entry: the statements of one entry share it.
type entryRef struct {
	logFile string
	pos     int64
}

type footNode struct {
	live     []Statement // in commit order
	children map[string]*footNode
}

type scopeFoot struct {
	root  *footNode
	refs  map[entryRef]int // live statements per entry
	count int
}

// Footprint holds every scope's live statements. One lock: writes are serialized by the
// commit lock already, and a read takes it for the length of a walk over one scope's
// paths under one kp, which is what it costs.
type Footprint struct {
	mu     sync.Mutex
	scopes map[string]*scopeFoot
}

func newFootprint() *Footprint { return &Footprint{scopes: map[string]*scopeFoot{}} }

func (f *Footprint) scope(id string, create bool) *scopeFoot {
	sf := f.scopes[id]
	if sf == nil && create {
		sf = &scopeFoot{root: &footNode{}, refs: map[entryRef]int{}}
		f.scopes[id] = sf
	}
	return sf
}

// walkDown answers the nodes on the way to path, root first, and the node at path (nil
// when it does not exist and create is false).
func (sf *scopeFoot) walkDown(path string, create bool) (chain []*footNode, at *footNode) {
	node := sf.root
	chain = append(chain, node)
	for rest := path; rest != ""; {
		first, tail := kpath.Split(rest)
		child := node.children[first]
		if child == nil {
			if !create {
				return chain, nil
			}
			child = &footNode{}
			if node.children == nil {
				node.children = map[string]*footNode{}
			}
			node.children[first] = child
		}
		node = child
		chain = append(chain, node)
		rest = tail
	}
	return chain, node
}

// state records a statement, unless a later live statement dominates it, and drops what it
// dominates. It answers whether the statement is live.
func (f *Footprint) state(scope string, st Statement) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, true)
	chain, node := sf.walkDown(st.Path, true)
	// Dominated by something already here and later? Ancestors dominate with a whole or
	// total cover; the node itself by cover against need.
	for k, anc := range chain {
		same := k == len(chain)-1
		for _, t := range anc.live {
			if t.Commit > st.Commit && t.Offers.dominates(st.Needs, same) {
				return false
			}
		}
	}
	// Already here? An entry re-added -- the catch-up after a persist re-indexes what
	// landed during it -- restates the same statement; it is one, at its newest
	// reference.
	for k := range node.live {
		if t := &node.live[k]; t.Commit == st.Commit && t.Tx == st.Tx {
			sf.refs[entryRef{t.LogFile, t.LogPosition}]--
			sf.refs[entryRef{st.LogFile, st.LogPosition}]++
			*t = st
			return true
		}
	}
	// In: in commit order, which is where it lands almost always.
	k := sort.Search(len(node.live), func(k int) bool {
		return node.live[k].Commit > st.Commit || (node.live[k].Commit == st.Commit && node.live[k].Tx > st.Tx)
	})
	node.live = append(node.live, Statement{})
	copy(node.live[k+1:], node.live[k:])
	node.live[k] = st
	sf.refs[entryRef{st.LogFile, st.LogPosition}]++
	sf.count++
	// Out: what it dominates, at the node by need, and everything older beneath it.
	if st.Offers >= CoverWhole {
		sf.dropAt(node, st.Commit, st.Offers, true, st.LogFile, st.LogPosition)
		for name, child := range node.children {
			sf.dropBelow(child, st.Commit, st.LogFile, st.LogPosition)
			if child.empty() {
				delete(node.children, name)
			}
		}
	}
	return true
}

// dropAt removes the statements at node older than commit that a cover of strength c
// dominates there, keeping the dominator itself.
func (sf *scopeFoot) dropAt(node *footNode, commit int64, c Cover, same bool, logFile string, pos int64) {
	kept := node.live[:0]
	for _, t := range node.live {
		dominated := t.Commit < commit && c.dominates(t.Needs, same)
		if dominated && !(t.LogFile == logFile && t.LogPosition == pos) {
			sf.refs[entryRef{t.LogFile, t.LogPosition}]--
			sf.count--
			continue
		}
		kept = append(kept, t)
	}
	node.live = kept
}

// dropBelow removes every statement older than commit at node and beneath it: a whole or
// total cover above them retires them all.
func (sf *scopeFoot) dropBelow(node *footNode, commit int64, logFile string, pos int64) {
	sf.dropAt(node, commit, CoverTotal, false, logFile, pos)
	for name, child := range node.children {
		sf.dropBelow(child, commit, logFile, pos)
		if child.empty() {
			delete(node.children, name)
		}
	}
}

func (n *footNode) empty() bool { return len(n.live) == 0 && len(n.children) == 0 }

// forget removes a statement, if it is live: the one at its path from its commit and
// transaction, which is the identity the index removes a segment by. Its position is not
// part of that -- a re-indexed survivor is removed by the key and added where the rewrite
// put it -- so the position is not matched here.
func (f *Footprint) forget(scope string, st Statement) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	if sf == nil {
		return
	}
	chain, node := sf.walkDown(st.Path, false)
	if node == nil {
		return
	}
	kept := node.live[:0]
	for _, t := range node.live {
		if t.Commit == st.Commit && t.Tx == st.Tx {
			sf.refs[entryRef{t.LogFile, t.LogPosition}]--
			sf.count--
			continue
		}
		kept = append(kept, t)
	}
	node.live = kept
	// Prune what emptied, from the leaf up.
	for k := len(chain) - 1; k > 0; k-- {
		if !chain[k].empty() {
			break
		}
		for name, child := range chain[k-1].children {
			if child == chain[k] {
				delete(chain[k-1].children, name)
			}
		}
	}
}

// Live answers the scope's live statements that bear on a read at kp: those on kp's
// ancestor chain, at kp, and beneath it, each entry once, in commit order. Nil when the
// scope has none.
func (f *Footprint) Live(scope, kp string) []Statement {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	if sf == nil {
		return nil
	}
	var out []Statement
	seen := map[entryRef]bool{}
	take := func(t Statement) {
		ref := entryRef{t.LogFile, t.LogPosition}
		if !seen[ref] {
			seen[ref] = true
			out = append(out, t)
		}
	}
	// The chain holds the nodes that exist on the way to kp, kp's own last when it does.
	chain, node := sf.walkDown(kp, false)
	for _, anc := range chain {
		for _, t := range anc.live {
			take(t)
		}
	}
	if node != nil {
		var walk func(n *footNode)
		walk = func(n *footNode) {
			for _, c := range n.children {
				for _, t := range c.live {
					take(t)
				}
				walk(c)
			}
		}
		walk(node)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Commit != out[b].Commit {
			return out[a].Commit < out[b].Commit
		}
		return out[a].Tx < out[b].Tx
	})
	return out
}

// Reaches says whether the scope has a live statement at kp, above it, or beneath it.
func (f *Footprint) Reaches(scope, kp string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	if sf == nil {
		return false
	}
	chain, node := sf.walkDown(kp, false)
	for _, anc := range chain {
		if len(anc.live) > 0 {
			return true
		}
	}
	return node != nil && !node.empty()
}

// TotallyCovered says whether a live total cover of the scope stands at kp or above it,
// so that nothing baseline does at or beneath kp can show through.
func (f *Footprint) TotallyCovered(scope, kp string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	if sf == nil {
		return false
	}
	chain, _ := sf.walkDown(kp, false)
	for _, anc := range chain {
		for _, t := range anc.live {
			if t.Offers == CoverTotal {
				return true
			}
		}
	}
	return false
}

// EntryLive says whether any statement of the entry at logFile:pos is live in the scope.
func (f *Footprint) EntryLive(scope, logFile string, pos int64) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	return sf != nil && sf.refs[entryRef{logFile, pos}] > 0
}

// Paths answers the paths at which the scope has live statements, and whether the
// footprint knows the scope at all.
func (f *Footprint) Paths(scope string) ([]string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sf := f.scope(scope, false)
	if sf == nil {
		return nil, false
	}
	var out []string
	var walk func(n *footNode, path string)
	walk = func(n *footNode, path string) {
		if len(n.live) > 0 {
			out = append(out, path)
		}
		for name, c := range n.children {
			walk(c, kpath.Join(path, name))
		}
	}
	walk(sf.root, "")
	sort.Strings(out)
	return out, true
}

// Drop forgets a scope entirely.
func (f *Footprint) Drop(scope string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.scopes, scope)
}

// FootprintStats is what the store says about it.
type FootprintStats struct {
	Scopes     int
	Statements int
}

func (f *Footprint) Stats() FootprintStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := FootprintStats{Scopes: len(f.scopes)}
	for _, sf := range f.scopes {
		st.Statements += sf.count
	}
	return st
}

func (s FootprintStats) Report() map[string]any {
	return map[string]any{
		"index.footprint.scopes":     s.Scopes,
		"index.footprint.statements": s.Statements,
	}
}

// ManifestScope is a scope's footprint as the manifest holds it: its live statements,
// flat, in commit order.
type ManifestScope struct {
	Scope      string
	Statements []Statement
}

// snapshot answers every scope's footprint for the manifest.
func (f *Footprint) snapshot() []ManifestScope {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.scopes))
	for name := range f.scopes {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ManifestScope, 0, len(names))
	for _, name := range names {
		sf := f.scopes[name]
		ms := ManifestScope{Scope: name}
		var walk func(n *footNode)
		walk = func(n *footNode) {
			ms.Statements = append(ms.Statements, n.live...)
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(sf.root)
		sort.Slice(ms.Statements, func(a, b int) bool {
			x, y := ms.Statements[a], ms.Statements[b]
			if x.Commit != y.Commit {
				return x.Commit < y.Commit
			}
			if x.Tx != y.Tx {
				return x.Tx < y.Tx
			}
			return x.Path < y.Path
		})
		out = append(out, ms)
	}
	return out
}

// load installs what a manifest holds, as it holds it: the set is already free of
// dominance, so nothing is dropped and nothing is re-decided.
func (f *Footprint) load(scopes []ManifestScope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = map[string]*scopeFoot{}
	for _, ms := range scopes {
		sf := f.scope(ms.Scope, true)
		for _, st := range ms.Statements {
			_, node := sf.walkDown(st.Path, true)
			node.live = append(node.live, st)
			sf.refs[entryRef{st.LogFile, st.LogPosition}]++
			sf.count++
		}
	}
}

// statementOf is the footprint's record of a statement segment.
func statementOf(seg *LogSegment) Statement {
	return Statement{
		Path:        seg.KindedPath,
		Commit:      seg.EndCommit,
		Tx:          seg.EndTx,
		LogFile:     seg.LogFile,
		LogPosition: seg.LogPosition,
		Generation:  seg.LogFileGeneration,
		Offers:      seg.Offers,
		Needs:       seg.Needs,
	}
}

// RebuildFootprint remakes every scope's footprint from the segments the index holds,
// in commit order. It is the repair path's: a repair that removed segments the log could
// not read may have removed a dominator, and what it dominated does not come back on its
// own. It pages the whole index, as the repair that calls it already has.
func (i *Index) RebuildFootprint() {
	var stmts []LogSegment
	for _, seg := range i.AllSegments() {
		if seg.Statement && seg.ScopeID != nil && seg.StartCommit != seg.EndCommit {
			stmts = append(stmts, seg)
		}
	}
	sort.Slice(stmts, func(a, b int) bool {
		x, y := stmts[a], stmts[b]
		if x.EndCommit != y.EndCommit {
			return x.EndCommit < y.EndCommit
		}
		if x.EndTx != y.EndTx {
			return x.EndTx < y.EndTx
		}
		return x.KindedPath < y.KindedPath
	})
	i.foot.mu.Lock()
	i.foot.scopes = map[string]*scopeFoot{}
	i.foot.mu.Unlock()
	for _, seg := range stmts {
		i.foot.state(*seg.ScopeID, statementOf(&seg))
	}
}

// Footprint answers the index's footprint.
func (i *Index) Footprint() *Footprint { return i.foot }
