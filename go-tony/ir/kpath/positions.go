package kpath

// Positions is where a walk is in a pattern: every segment the pattern could be at,
// given the steps the walk has taken. It is the state of matching a pattern holding
// `..` the way a glob is matched (matchPath), kept as a set rather than found by
// backtracking, because a walk over a tree reaches each node once and has to know at
// each what the pattern could still be. A position is a pointer into the pattern; nil
// is the position past its end, which says the node the walk is at is one the pattern
// names.
//
// A set is always closed over `..`: a position at a descent also holds the position
// after it, since a descent may take no segments at all. That is what makes a..b find
// a b directly under a as well as one further down, and what makes `a..` name a itself.
//
// Two walkers step it: ir's list over a document and logd's set read over the store.
// Each says for itself whether a segment takes a child (a field, a position, a keyed
// element, by that walker's own rules); this holds what is common, which is what `..`
// means.
type Positions []*KPath

// Start is where a walk is before it has taken a step: at p, and, through any
// descents at its head, at whatever follows them.
func Start(p *KPath) Positions {
	return Positions(nil).closeOver(p)
}

// Done says the pattern may be spent here: the node the walk is at is one it names.
func (ps Positions) Done() bool {
	for _, p := range ps {
		if p == nil {
			return true
		}
	}
	return false
}

// Live says some position can still take a child, so there is a reason to walk on.
func (ps Positions) Live() bool {
	for _, p := range ps {
		if p != nil {
			return true
		}
	}
	return false
}

// Step is where the walk is after taking one child. takes says whether a segment
// names that child; it is asked about concrete segments and wildcards, never about a
// descent or a spent pattern, since those are what Step itself knows: a descent keeps
// its position (it absorbs the child), and a spent pattern takes nothing. An empty
// answer says the child is not on any path the pattern names, and is not worth walking.
func (ps Positions) Step(takes func(seg *KPath) bool) Positions {
	var next Positions
	for _, p := range ps {
		switch {
		case p == nil:
		case p.Descend:
			next = next.closeOver(p)
		case takes(p):
			next = next.closeOver(p.Next)
		}
	}
	return next
}

// closeOver adds p and, while p is a descent, what follows it, each once.
func (ps Positions) closeOver(p *KPath) Positions {
	for {
		if ps.holds(p) {
			return ps
		}
		ps = append(ps, p)
		if p == nil || !p.Descend {
			return ps
		}
		p = p.Next
	}
}

func (ps Positions) holds(p *KPath) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}
