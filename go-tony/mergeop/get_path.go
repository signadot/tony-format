package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var (
	getPathSym  = &getSymbol{name: getPathName}
	listPathSym = &getSymbol{name: listPathName, list: true}
)

// GetPath is the !get-path(anchor) operator: it answers with the node at a
// kpath, which is what nothing else in the vocabulary could say.
//
//	# the sidecar takes the image the main container has
//	sidecar: {image: !get-path(root) spec.containers.main.image}
//
//	# a match on a RELATION between two parts of one document
//	status: {replicas: !get-path(root) spec.replicas}
//
// !at walks to a path and applies a match there, !embed hands the whole node to
// an operand with no path, and !has-path answers whether.  None of them answers
// WITH the value at a path, so nothing could be compared against one, written
// from one, or bound to a name.
//
// The path is relative to the node the operator meets, which is what makes it
// mean the same thing wherever it is written.  !get-path(root) anchors it at the
// document instead -- Root(), the walk to nil, which is the library's own answer
// to what document a node is in.  The anchor is a tag component and not a sigil
// in the path: kpath spells the root as the EMPTY path, and the '$' was taken out
// of the query surface deliberately, so a root sigil in the grammar would reach
// every stored path, the index and every watch name to say something the operator
// can say by itself.
//
// A path which names nothing is an ERROR, and that is on purpose where !at reads
// the same absence as a mismatch.  !at relocates a PATTERN, so asking for an a.b
// in a document without one is a thing a document can fail to be; this answers
// with a VALUE, and there is none, so the alternatives are a null -- which a match
// reads as "anything" and a patch WRITES -- or a silent no-match which says
// nothing about why.  Neither is an answer.
func GetPath() Symbol {
	return getPathSym
}

// ListPath is the !list-path(anchor) operator: the nodes at a kpath, as a list.
//
//	!list-path containers[*].image
//
// The pair is named for the two walks it is: ir.Node.GetKPath answers a node and
// ListKPath answers the nodes, and !get-path and !list-path are those two asked
// from a pattern.
//
// It is !get-path's plural and takes the paths its singular refuses: a wild
// segment (.* [*] {*} ..) names a set rather than a node, and kpath already knows
// which is which, so the two operators are that distinction made into two names
// rather than a rule about what one of them does with a wild path.
//
// A path which names nothing is the EMPTY LIST, where !get-path errors: each
// keeps the promise its name makes, and an empty list is a list -- the reading
// !all over an empty container already has.
//
// The values are copies, and so is !get-path's answer.  A node belongs to one
// tree: the patch side installs what it is given, and the container builders
// re-parent what they are handed, so the document's own node would be spliced out
// of the document.  The copy is detached -- !get-path's answer is a root, and each
// of !list-path' is parented to the list -- so a walk up stops at what was asked
// for.  Navigating down is what the path already did, and !get-path(root) inside
// such a value therefore means that value, not the document it came from.
func ListPath() Symbol {
	return listPathSym
}

const (
	getPathName  name = "get-path"
	listPathName name = "list-path"

	// rootArg anchors the path at the document rather than at the node met.
	rootArg = "root"
)

type getSymbol struct {
	name
	list bool
}

func (s getSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	child = ir.Uncomment(child)
	if child == nil || child.Type != ir.StringType {
		return nil, fmt.Errorf("%s op expects a kpath as its operand, got %s", s, operandType(child))
	}
	// The walk reparses the path, but a pattern which cannot be read is a defect
	// in the pattern, and saying so where it is built beats reporting it as a
	// mismatch at every node the match visits -- the reading !at gives.
	kp, err := kpath.Parse(child.String)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", s, child.String, err)
	}
	if !s.list && kp.HasWild() {
		return nil, fmt.Errorf("%s %s: the path names a set of nodes rather than one; %s answers a list",
			s, child.String, listPathName)
	}
	res := &getOp{
		op:   op{name: s.name, child: child},
		path: child.String,
		list: s.list,
	}
	// Read by NAME rather than by position: the anchor is optional, so a component
	// added later has to compose with it and without it, which args[0] cannot do.
	for _, arg := range args {
		switch arg {
		case rootArg:
			res.root = true
		default:
			return nil, fmt.Errorf("%s(%s): the only tag component is %q, which anchors the path at the document",
				s, arg, rootArg)
		}
	}
	return res, nil
}

// getOp is !get-path and !list-path: one operation, and the two names are which
// promise it keeps.
type getOp struct {
	op
	path string
	root bool
	list bool
}

func (g getOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("%s(%s) op match on %s\n", g.name, g.path, doc.Path())
	}
	val, err := g.fetch(doc)
	if err != nil {
		return false, err
	}
	// The value is matched as an ordinary pattern rather than as literal data.
	// A document is usually just data, so a tag in one which happens to name an
	// operation is the unusual case, and !raw is deliberately not contagious --
	// it says to put it "at the depth where literal comparison starts", and it
	// also means EXACT comparison rather than the partial object match a pattern
	// gives, which is a larger change than the tag question would justify making
	// by accident.  Revisit with a real document in hand.
	return f(doc, val, ctx)
}

func (g getOp) Patch(doc *ir.Node, ctx *OpContext, _ MatchFunc, _ PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("%s(%s) op patch on %s\n", g.name, g.path, doc.Path())
	}
	// The value is what results.  It is not handed to pf: that would apply it as
	// a PATCH, which for an object is a merge with what is already there, and
	// "the value over there" is a value.
	return g.fetch(doc)
}

// fetch answers the value at the path, copied and detached, or says why there is
// none.
func (g getOp) fetch(doc *ir.Node) (*ir.Node, error) {
	anchor := doc
	if g.root {
		anchor = doc.Root()
	}
	if g.list {
		// A path which runs into a scalar reached nothing, which is the empty
		// list here rather than an error: the walk says so as an error and the
		// operator's answer is a LIST, and an empty one is a list.
		nodes, err := anchor.ListKPath(nil, g.path)
		if err != nil {
			return childList(nil), nil
		}
		return childList(nodes), nil
	}
	// The walk reports "no such node" two ways -- a nil node, and an error where
	// the path ran into something it cannot descend -- and they are one fact to a
	// caller who wanted the value. The cause is kept, because which of the two it
	// was says where the path went wrong.
	node, err := anchor.GetKPath(g.path)
	if err != nil {
		return nil, fmt.Errorf("%s %s: the path names nothing %s: %w", g.name, g.path, g.anchorPhrase(), err)
	}
	if node == nil {
		return nil, fmt.Errorf("%s %s: the path names nothing %s", g.name, g.path, g.anchorPhrase())
	}
	return detached(node), nil
}

func (g getOp) anchorPhrase() string {
	if g.root {
		return "in the document"
	}
	return "below the node it is written at"
}

// detached answers a copy which is a root: Clone carries Parent, ParentIndex and
// ParentField across, and a value lifted out of a document keeps none of them.
func detached(n *ir.Node) *ir.Node {
	res := n.Clone()
	res.Parent, res.ParentIndex, res.ParentField = nil, 0, ""
	return res
}
