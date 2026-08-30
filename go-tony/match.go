package tony

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// MatchConfig holds the options a match was given.  See Explaining and
// Tracing; a match with no options is the plain yes-or-no.
type MatchConfig struct {
	// Explain, when set, receives why the match came out as it did.
	Explain *Explanation

	// Trace keeps the frames of sub-matches which succeeded, so an
	// explanation can say which alternative of an !or matched rather than
	// only why nothing did.
	Trace bool
}

type MatchOpt func(*MatchConfig)

// Explaining collects why a match failed into why: the path of each failing
// node, what was expected there, and what was found.  A match which succeeds
// records nothing beyond why.Matched.
//
// Structure is walked through: every field of an object which fails is
// reported, so one round of repairs can fix them all.  Operators stop where
// they stop -- an !all reports the first element which failed, not every one
// -- since that is the walk they do.
func Explaining(why *Explanation) MatchOpt {
	return func(c *MatchConfig) { c.Explain = why }
}

// Tracing collects both polarities into why: the failures Explaining
// collects, and, in why.Matches, the operators which matched -- which branch
// of an !or, which elements an !all accepted.  It keeps a frame per node the
// match visits rather than discarding what succeeded, so it costs more than
// Explaining on a document which matches.
func Tracing(why *Explanation) MatchOpt {
	return func(c *MatchConfig) { c.Explain, c.Trace = why, true }
}

// Match matches doc against a pattern. This is the backwards-compatible
// version that doesn't use context. Use MatchWith for schema-aware matching.
func Match(doc, match *ir.Node, opts ...MatchOpt) (bool, error) {
	return MatchWith(doc, match, nil, opts...)
}

// MatchWith matches doc against a pattern with the given context.
// The context carries schema definitions for .[ref] expansion and behavioral options.
func MatchWith(doc, match *ir.Node, ctx *mergeop.OpContext, opts ...MatchOpt) (bool, error) {
	cfg := MatchConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Explain == nil {
		return matchWith(doc, match, ctx, nil)
	}
	e := &explainer{root: doc, trace: cfg.Trace}
	matched, err := matchWith(doc, match, ctx, e)
	e.finish(cfg.Explain, matched)
	return matched, err
}

// matchWith is MatchWith with the explanation, if any, being collected.  It
// frames the node so that everything the match records beneath belongs to it,
// and so that a node which matches can take its frames with it when it goes.
func matchWith(doc, match *ir.Node, ctx *mergeop.OpContext, e *explainer) (bool, error) {
	if e == nil {
		return matchNode(doc, match, ctx, nil)
	}
	f := e.push(doc, match)
	matched, err := matchNode(doc, match, ctx, e)
	e.pop(f, matched, err)
	return matched, err
}

// uncomment answers the value a comment node wraps, at whatever depth they nest.
// A comment describes the value; it is not part of what the value IS, and no
// question about the data should be able to see it.
func uncomment(n *ir.Node) *ir.Node {
	for n != nil && n.Type == ir.CommentType && len(n.Values) == 1 {
		n = n.Values[0]
	}
	return n
}

func matchNode(doc, match *ir.Node, ctx *mergeop.OpContext, e *explainer) (bool, error) {
	if debug.Match() {
		debug.Logf("match type %s at %s with tag %q\n", match.Type, match.Path(), match.Tag)
	}
	// A comment describes a value; it is not what the value IS. A head comment
	// wraps the value it precedes in a CommentType node, so a document parsed
	// with comments failed patterns it plainly satisfies -- {name: svc} did not
	// match "# lead\nname: svc" -- and answered false rather than erring, which
	// is the shape of wrongness nobody finds.
	//
	// The format says tools support matching comments "if so desired", and
	// !comment is how they are desired: it reads what wrapped a node through the
	// node's parent, so unwrapping here does not put comments beyond reach -- it
	// keeps every OTHER question blind to them. See mergeop's commentOp.Match.
	doc, match = uncomment(doc), uncomment(match)
	_, tag, args, child, err := mergeop.SplitChild(match)
	if err != nil {
		return false, err
	}
	if tag != "" {
		if e != nil {
			e.op(tag)
		}
		op := mergeop.Lookup(tag)
		if op == nil {
			return false, fmt.Errorf("no mergeop for tag %q", tag)
		}
		opInst, err := op.Instance(child, args)
		if err != nil {
			return false, err
		}
		// Create a MatchFunc that threads ctx and the explanation through
		// recursive calls
		matchFunc := func(d, p *ir.Node, c *mergeop.OpContext) (bool, error) {
			return matchWith(d, p, c, e)
		}
		return opInst.Match(doc, ctx, matchFunc)
	}
	if doc.Type != match.Type && match.Type != ir.NullType {
		if e != nil {
			e.fail(ReasonType)
		}
		return false, nil
	}
	switch match.Type {
	case ir.ObjectType:
		return matchObj(doc, match, ctx, e)
	case ir.ArrayType:
		return matchArray(doc, match, ctx, e)
	case ir.StringType:
		return value(doc.String == match.String, e)
	case ir.BoolType:
		return value(doc.Bool == match.Bool, e)
	case ir.NullType:
		return true, nil
	case ir.NumberType:
		if (doc.Int64 == nil) != (match.Int64 == nil) {
			return value(false, e)
		}
		if (doc.Float64 == nil) != (match.Float64 == nil) {
			return value(false, e)
		}
		if doc.Int64 != nil {
			return value(*doc.Int64 == *match.Int64, e)
		}
		if doc.Float64 != nil {
			return value(*doc.Float64 == *match.Float64, e)
		}
		return value(doc.Number == match.Number, e)
	}
	return false, nil
}

// value reports a scalar comparison, noting a difference in value as the
// reason when one is being collected.
func value(matched bool, e *explainer) (bool, error) {
	if !matched && e != nil {
		e.fail(ReasonValue)
	}
	return matched, nil
}

// matchObj matches the fields a pattern names and ignores the rest: a
// document carrying more than it was asked for still matches.
func matchObj(doc, match *ir.Node, ctx *mergeop.OpContext, e *explainer) (bool, error) {
	mMap := make(map[string]*ir.Node, len(match.Fields))
	for i, field := range match.Fields {
		child := match.Values[i]
		mMap[field.String] = child
	}
	count, failed := 0, false
	for i := range doc.Fields {
		field := doc.Fields[i]
		my := mMap[field.String]
		if my == nil {
			continue
		}
		subMatch, err := matchWith(doc.Values[i], my, ctx, e)
		if err != nil {
			if failed {
				// a match which stopped at the first failure would never
				// have reached this pattern; the frame records the error,
				// and asking why does not turn a mismatch into an error
				continue
			}
			return false, err
		}
		if !subMatch {
			failed = true
			if e == nil {
				return false, nil
			}
			// an explanation wants every field which is wrong, not the
			// first: repairing a document one defect per round trip is
			// what asking why is meant to avoid
			continue
		}
		count++
	}
	if e != nil && count < len(mMap) {
		reportAbsent(doc, match, mMap, e)
	}
	return !failed && count == len(mMap), nil
}

// reportAbsent records the fields the pattern requires and the document does
// not have, which is the one defect a caller can most often repair without
// being told anything else.
func reportAbsent(doc, match *ir.Node, mMap map[string]*ir.Node, e *explainer) {
	present := make(map[string]bool, len(doc.Fields))
	for _, field := range doc.Fields {
		present[field.String] = true
	}
	for _, field := range match.Fields {
		name := field.String
		if present[name] {
			continue
		}
		present[name] = true // a pattern may name a field twice
		e.absent(doc, name, mMap[name])
	}
}

func matchArray(doc, match *ir.Node, ctx *mergeop.OpContext, e *explainer) (bool, error) {
	if len(doc.Values) != len(match.Values) {
		if e != nil {
			e.fail(ReasonLength)
		}
		return false, nil
	}
	failed := false
	for i := range doc.Values {
		subMatch, err := matchWith(doc.Values[i], match.Values[i], ctx, e)
		if err != nil {
			if failed {
				continue // as in matchObj: not an error the match reached
			}
			return false, err
		}
		if !subMatch {
			failed = true
			if e == nil {
				return false, nil
			}
		}
	}
	return !failed, nil
}

// Trim filters a document to only include fields/values that are present in the match criteria.
// It recursively processes objects and arrays, removing fields that aren't in the match.
// The result preserves the tag from the original document.
// Returns nil if the doc doesn't match the criteria (used to signal exclusion).
func Trim(match, doc *ir.Node) *ir.Node {
	_, tag, args, child, err := mergeop.SplitChild(match)
	if err == nil && tag != "" {
		// An operator that names WHERE the pattern applies carries the trim with it:
		// !at walks to the path and trims there, !all distributes over the elements.
		// Every other operator names WHETHER a value qualifies -- !or, !not, !glob,
		// !irtype -- and shapes nothing, so the value goes through whole or not at all.
		if trimmed, located := trimAt(tag, args, child, doc); located {
			return trimmed
		}
		matched, _ := MatchWith(doc, match, nil)
		if matched {
			return doc.Clone()
		}
		return nil
	}

	switch match.Type {
	case ir.ObjectType:
		docMap := ir.ToMap(doc)
		matchMap := ir.ToMap(match)
		for i, field := range doc.Fields {
			matchVal := matchMap[field.String]
			if matchVal == nil {
				delete(docMap, field.String)
				continue
			}
			docVal := doc.Values[i]
			trimmed := Trim(matchVal, docVal)
			if trimmed == nil {
				delete(docMap, field.String)
			} else {
				docMap[field.String] = trimmed
			}
		}
		return ir.FromMap(docMap).WithTag(doc.Tag)
	case ir.ArrayType:
		// For arrays, we need to match each match element against doc elements
		// and keep only the matching ones, trimmed
		var res []*ir.Node
		used := make([]bool, len(doc.Values))

		for _, matchElem := range match.Values {
			// Find the first unused doc element that matches this match element
			for i, docElem := range doc.Values {
				if used[i] {
					continue
				}
				matched, err := Match(docElem, matchElem)
				if err != nil {
					// If matching fails, skip this doc element
					continue
				}
				if matched {
					// Found a match - trim it and add to result
					trimmed := Trim(matchElem, docElem)
					if trimmed != nil {
						res = append(res, trimmed)
					}
					used[i] = true
					break
				}
			}
		}
		return ir.FromSlice(res).WithTag(doc.Tag)
	default:
		return doc.Clone()
	}
}

// The operators that name a LOCATION rather than a test. Taken from the symbols so a
// rename moves them both at once.
var (
	atTag  = mergeop.At().String()
	allTag = mergeop.All().String()
)

// trimAt trims through a locating operator, answering located=false for one that locates
// nothing and is therefore a test on the value rather than a statement about which part
// of it comes back.
//
// Every tagged pattern used to be a test: Trim matched it and returned the document
// WHOLE. So `!at(verse.meta) {rev: !pass null}` -- the composition the vocabulary invites,
// and the one a projection wants -- answered the entire document, silently doing nothing
// rather than refusing. A projection under a path could not be written at all
// (599k23e4h12krhw3gdn0).
func trimAt(tag string, args []string, child, doc *ir.Node) (trimmed *ir.Node, located bool) {
	switch tag {
	case atTag:
		if len(args) != 1 {
			// A malformed !at is a defect in the pattern; let the match report it
			// by failing, as it did before.
			return nil, false
		}
		return trimKPath(args[0], child, doc), true
	case allTag:
		return trimAll(child, doc), true
	}
	return nil, false
}

// trimKPath is the trim of !at(kp): the nodes the path names, each trimmed by the pattern
// beneath the operator, kept where they sit.
//
// In place is the point. A projection names what comes back and not where it moves to, so
// the answer is rooted as the document is and the paths a caller already holds still reach
// what they reached -- the shape the operator's own composition implies, and the one a
// client can navigate without learning a second addressing scheme.
//
// A path naming nothing excludes, as !at itself does not match a document without the
// path. A node the pattern excludes takes the whole !at with it, since !at holds only when
// every node a wild path reaches does -- though Trim excludes rarely, because it shapes
// rather than decides: a scalar the pattern disagrees with still comes back, and whether
// the document qualifies is Match's question. FilterState is where the two are put
// together.
func trimKPath(kp string, child, doc *ir.Node) *ir.Node {
	nodes, err := doc.ListKPath(nil, kp)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	// ListKPath answers nodes detached from the tree whose parents they still point
	// into, so the spine up to the root -- not identity with doc's own children -- is
	// what says where each one belongs.
	spines := map[*ir.Node]map[int]*ir.Node{}
	for _, node := range nodes {
		t := Trim(child, node)
		if t == nil {
			return nil
		}
		if node.Parent == nil {
			// The path named the document itself.
			return t
		}
		at, up := t, node
		for p := node.Parent; p != nil; p, up = p.Parent, p {
			kids := spines[p]
			if kids == nil {
				kids = map[int]*ir.Node{}
				spines[p] = kids
			}
			if prev, seen := kids[up.ParentIndex]; !seen || prev == nil {
				kids[up.ParentIndex] = at
			}
			// Above the first step the entry is a marker: what goes there is
			// built from that container's own retained children.
			at = nil
		}
	}
	return graftSpine(doc, spines)
}

// graftSpine rebuilds n holding the retained children and nothing else, so the answer keeps
// the document's rooting while carrying only what the path named.
//
// A rebuilt array closes its gaps, which is what Trim's own array case already does with
// the elements a pattern does not name: a positional index into the answer is not the index
// it was in the document. For a keyed list (!key) the identity a caller addresses by
// survives, which is the case this is for.
func graftSpine(n *ir.Node, spines map[*ir.Node]map[int]*ir.Node) *ir.Node {
	kids := spines[n]
	if len(kids) == 0 {
		return nil
	}
	child := func(i int) *ir.Node {
		if repl := kids[i]; repl != nil {
			return repl
		}
		return graftSpine(n.Values[i], spines)
	}
	switch n.Type {
	case ir.ObjectType:
		dst := make(map[string]*ir.Node, len(kids))
		for i := range kids {
			if i >= len(n.Fields) || n.Fields[i].Type == ir.NullType {
				continue
			}
			if kept := child(i); kept != nil {
				dst[n.Fields[i].String] = kept
			}
		}
		if len(dst) == 0 {
			return nil
		}
		return ir.FromMap(dst).WithTag(n.Tag)
	case ir.ArrayType:
		var res []*ir.Node
		for i := range n.Values {
			if _, ok := kids[i]; !ok {
				continue
			}
			if kept := child(i); kept != nil {
				res = append(res, kept)
			}
		}
		if len(res) == 0 {
			return nil
		}
		return ir.FromSlice(res).WithTag(n.Tag)
	}
	return nil
}

// trimAll is the trim of !all: the pattern applies to every element, so the trim does too,
// and the container comes back with every element trimmed. !all matches only when every
// element does, so one element the pattern rejects excludes the whole container.
func trimAll(child, doc *ir.Node) *ir.Node {
	switch doc.Type {
	case ir.ObjectType:
		dst := make(map[string]*ir.Node, len(doc.Fields))
		for i := range doc.Fields {
			if doc.Fields[i].Type == ir.NullType {
				continue
			}
			t := Trim(child, doc.Values[i])
			if t == nil {
				return nil
			}
			dst[doc.Fields[i].String] = t
		}
		return ir.FromMap(dst).WithTag(doc.Tag)
	case ir.ArrayType:
		res := make([]*ir.Node, 0, len(doc.Values))
		for _, v := range doc.Values {
			t := Trim(child, v)
			if t == nil {
				return nil
			}
			res = append(res, t)
		}
		return ir.FromSlice(res).WithTag(doc.Tag)
	}
	// A scalar has no elements; !all defers to the pattern on the value itself.
	return Trim(child, doc)
}
