package mergeop

import (
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var irSym = &irSymbol{matchName: irName}

// IR is the !ir operator: the pattern it holds is matched against the node's own
// IR representation rather than against the value the node holds.
//
//	!ir {int: !irtype 1}
//
// matches a node whose int field is set -- an integer, as opposed to a float,
// which no pattern over the value can say, because both are Number nodes and
// there is one Number type.  The operand is an object whose keys are the fields
// of ir.Node under the names it serializes them with:
//
//	3         !ir {type: Number, int: 3}
//	3.5       !ir {type: Number, float: 3.5}
//	"x"       !ir {type: String, string: "x"}
//	!k v      !ir {type: String, tag: "!k", string: "v"}
//	{a: 1}    !ir {type: Object, fields: [a], values: [1]}
//
// A key which is not one of those fields is refused where the pattern is built,
// rather than never matching: !ir {itn: 3} is a misspelling, and a pattern which
// silently declines to match every document there is is the shape of wrongness
// nobody finds.
//
// An unset field is ABSENT, not null, so `{int: null}` -- a null pattern matches
// anything present -- says only that the field is there.  Whether that is enough
// depends on the field: Int64 and Float64 are pointers, so absence means unset,
// while String, Bool and Number are omitted when they hold their zero value.  A
// pattern which says what the field holds -- {int: !irtype 1} -- does not have
// to know which kind of field it is asking about.
//
// fields and values are answered as a list, built when a pattern asks for one,
// and !ir applies again at whatever depth it is written: that is what lets a
// schema describe a node all the way down.  The list is one level deep -- a
// node whose values are nodes gives a list of those nodes, not of views of them.
//
// This is a question about the node, not about the document, and the difference
// is the reason it is an operator.  A document which happens to look like an IR
// encoding -- {type: Number, int: 3} written out by hand -- is matched by the
// ordinary object pattern, and by !ir only via ITS representation, which is
// {type: Object, ...}.  base.tony's _ir describes the first, !ir asks the second.
func IR() Symbol {
	return irSym
}

const (
	irName matchName = "ir"
)

type irSymbol struct {
	matchName
}

// irFields are the fields of an IR node, in the order ir.Node declares them.
// The list is the vocabulary a pattern may use, and the error when it uses
// something else.
var irFields = []string{
	"type", "fields", "values", "tag", "lines", "comment",
	"string", "bool", "number", "float", "int",
}

func isIRField(name string) bool {
	for _, f := range irFields {
		if f == name {
			return true
		}
	}
	return false
}

func (s irSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	child = ir.Uncomment(child)
	if child == nil || child.Type != ir.ObjectType {
		return nil, fmt.Errorf("%s op expects an object over the fields of an IR node, got %s",
			s, irOperandType(child))
	}
	asks := make([]irAsk, 0, len(child.Fields))
	for i, f := range child.Fields {
		if i >= len(child.Values) {
			return nil, fmt.Errorf("%s op operand is malformed: %d names and %d values",
				s, len(child.Fields), len(child.Values))
		}
		if f == nil || f.Type != ir.StringType {
			return nil, fmt.Errorf("%s op: a key names a field of an IR node, and %s does not",
				s, irOperandType(f))
		}
		if !isIRField(f.String) {
			return nil, fmt.Errorf("%s op: %q is not a field of an IR node; they are %s",
				s, f.String, strings.Join(irFields, ", "))
		}
		asks = append(asks, irAsk{field: f.String, pattern: child.Values[i]})
	}
	return &irOp{
		matchOp: matchOp{op: op{name: s.matchName, child: child}},
		asks:    asks,
	}, nil
}

// irAsk is one field of the node the pattern asks about, and what it asks.
type irAsk struct {
	field   string
	pattern *ir.Node
}

type irOp struct {
	matchOp
	asks []irAsk
}

// Match reads the pattern's fields against the node's own, one at a time.
//
// It used to build a whole object standing for the node -- an ir.Node which was
// not a place in any document, parentless on top and holding the document's own
// children underneath, so it could be walked down and not back up -- and hand
// that to the generic matcher.  That was one node the library's own invariant
// did not hold for, and it cost more than it looks: a parentless node already
// means "document root", so Root() could not tell the view from one, and Explain
// had to find its root by node identity to work around it (p4tzbzx7h12kr6tkhxn0).
//
// Nothing is built for a field a pattern does not ask about, which the view did
// unconditionally.  What is built for the ones it does is a value: a scalar, or a
// list of clones whose children point back at it.
func (i irOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("ir op match on %s\n", doc.Path())
	}
	for _, ask := range i.asks {
		val, has := irFieldOf(doc, ask.field)
		if !has {
			// An unset field is absent, not null, and a pattern naming it is
			// asking for a node which has it.
			return false, nil
		}
		matched, err := f(val, ask.pattern, ctx)
		if err != nil || !matched {
			return matched, err
		}
	}
	return true, nil
}

// irFieldOf answers the node to match a pattern against for one field of doc's
// IR representation, and whether doc has that field at all.
//
// The scalars are made here rather than read from the document because they are
// not IN the document: a node's type and the presence of its int field are facts
// about the node, and the nodes carrying them are values with no place.  comment
// is the exception -- it IS a node of the document, and it is handed over as
// itself, so a pattern reaching into it is at its real position.
func irFieldOf(doc *ir.Node, field string) (*ir.Node, bool) {
	switch field {
	case "type":
		return ir.FromString(doc.Type.String()), true
	case "fields":
		// A container HAS fields and values, empty ones included: an empty object's
		// keys are all integers as much as a full one's are, and `!all` over nothing
		// says so, where an absent field would fail instead.
		if len(doc.Fields) == 0 && doc.Type != ir.ObjectType {
			return nil, false
		}
		return childList(doc.Fields), true
	case "values":
		if len(doc.Values) == 0 && doc.Type != ir.ObjectType && doc.Type != ir.ArrayType {
			return nil, false
		}
		return childList(doc.Values), true
	case "tag":
		if doc.Tag == "" {
			return nil, false
		}
		return ir.FromString(doc.Tag), true
	case "lines":
		if len(doc.Lines) == 0 {
			return nil, false
		}
		lines := make([]*ir.Node, len(doc.Lines))
		for i, ln := range doc.Lines {
			lines[i] = ir.FromString(ln)
		}
		return ir.FromSlice(lines), true
	case "comment":
		if doc.Comment == nil {
			return nil, false
		}
		return doc.Comment, true
	case "string":
		if doc.String == "" {
			return nil, false
		}
		return ir.FromString(doc.String), true
	case "bool":
		// Bool is not a pointer, so false is indistinguishable from unset: the
		// reason base.tony's bool stays !irtype true.
		if !doc.Bool {
			return nil, false
		}
		return ir.FromBool(true), true
	case "number":
		if doc.Number == "" {
			return nil, false
		}
		return ir.FromString(doc.Number), true
	case "float":
		if doc.Float64 == nil {
			return nil, false
		}
		return ir.FromFloat(*doc.Float64), true
	case "int":
		if doc.Int64 == nil {
			return nil, false
		}
		return ir.FromInt(*doc.Int64), true
	}
	return nil, false
}

// childList answers a node's children as a list to match a pattern against.
//
// They are CLONED into it.  A list holding the node's own children would be a
// node you can walk down and not back up -- its children's Parent points at the
// document, not at the list -- and that is the shape this operator is no longer
// handing out.  The clone makes it a value instead: a coherent little document
// standing for a part of the node, and one built only when a pattern asks.
//
// What that costs is node identity, and the only thing which reads it is
// Explain's walk to the root: a failure under fields or values is reported at the
// !ir rather than at the child's own path.  That is the reading this operator
// asks for anyway -- the question is about the node's representation, not about a
// place in the document -- and Explain's identity walk is its own to answer for.
func childList(kids []*ir.Node) *ir.Node {
	clones := make([]*ir.Node, len(kids))
	for i, kid := range kids {
		clones[i] = kid.Clone()
	}
	// FromSlice re-parents what it is given, which is why it is given clones: the
	// document's own children may not be spliced into a list built for a match.
	return ir.FromSlice(clones)
}

// irOperandType names what a malformed operand is, for the error.
func irOperandType(n *ir.Node) string {
	if n == nil {
		return "nothing"
	}
	return n.Type.String()
}
