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
// A descent may be bounded: with a depth, each `..` takes at most that many segments,
// and a position at one carries how many it has taken. The walk then stops at the
// bound rather than walking deeper and filtering, since a position that has taken its
// depth is dropped and nothing beneath is worth walking for it.
//
// Two walkers step it: ir's list over a document and logd's set read over the store.
// Each says for itself whether a segment takes a child (a field, a position, a keyed
// element, by that walker's own rules); this holds what is common, which is what `..`
// means.
type Positions struct {
	at    []position
	depth int // the most segments a `..` may take; Unbounded for any number
}

// position is one place the pattern could be, and, at a descent, how many segments
// that descent has taken so far.
type position struct {
	p     *KPath
	taken int
}

// Unbounded is the depth at which a `..` may take any number of segments.
const Unbounded = -1

// Start is where a walk is before it has taken a step: at p, and, through any
// descents at its head, at whatever follows them. depth bounds every `..` in p, or is
// Unbounded.
func Start(p *KPath, depth int) Positions {
	return Positions{depth: depth}.closeOver(position{p: p})
}

// Done says the pattern may be spent here: the node the walk is at is one it names.
func (ps Positions) Done() bool {
	for _, x := range ps.at {
		if x.p == nil {
			return true
		}
	}
	return false
}

// Live says some position can still take a child, so there is a reason to walk on.
func (ps Positions) Live() bool {
	for _, x := range ps.at {
		if x.p != nil {
			return true
		}
	}
	return false
}

// Step is where the walk is after taking one child. takes says whether a segment
// names that child; it is asked about concrete segments and wildcards, never about a
// descent or a spent pattern, since those are what Step itself knows: a descent keeps
// its position (it absorbs the child) unless it has taken its depth, and a spent
// pattern takes nothing. An empty answer says the child is not on any path the pattern
// names, and is not worth walking.
func (ps Positions) Step(takes func(seg *KPath) bool) Positions {
	next := Positions{depth: ps.depth}
	for _, x := range ps.at {
		switch {
		case x.p == nil:
		case x.p.Descend:
			if ps.depth == Unbounded || x.taken < ps.depth {
				next = next.closeOver(position{p: x.p, taken: x.taken + 1})
			}
		case takes(x.p):
			next = next.closeOver(position{p: x.p.Next})
		}
	}
	return next
}

// Each calls fn with the segment at each position, nil for the spent pattern.
func (ps Positions) Each(fn func(*KPath)) {
	for _, x := range ps.at {
		fn(x.p)
	}
}

// Empty says no position is held at all: neither a node the pattern names nor a
// segment that could take a child.
func (ps Positions) Empty() bool { return len(ps.at) == 0 }

// closeOver adds x and, while x is at a descent, what follows it, each once.
func (ps Positions) closeOver(x position) Positions {
	for {
		if ps.holds(x) {
			return ps
		}
		ps.at = append(ps.at, x)
		if x.p == nil || !x.p.Descend {
			return ps
		}
		x = position{p: x.p.Next}
	}
}

func (ps Positions) holds(x position) bool {
	for _, y := range ps.at {
		if y == x {
			return true
		}
	}
	return false
}
