package mergeop

import (
	"fmt"

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
// there is one Number type.  The fields are the fields of ir.Node under the
// names it serializes them with:
//
//	3         !ir {type: Number, int: 3}
//	3.5       !ir {type: Number, float: 3.5}
//	"x"       !ir {type: String, string: "x"}
//	!k v      !ir {type: String, tag: "!k", string: "v"}
//	{a: 1}    !ir {type: Object, fields: [a], values: [1]}
//
// An unset field is absent, not null, so `{int: null}` -- a null pattern matches
// anything present -- says only that the field is there.  Whether that is enough
// depends on the field: Int64 and Float64 are pointers, so absence means unset,
// while String, Bool and Number are omitted when they hold their zero value.  A
// pattern which says what the field holds -- {int: !irtype 1} -- does not have
// to know which kind of field it is asking about.
//
// fields and values hold the node's children as they are, so a pattern under
// them descends into the document and !ir applies again at whatever depth it is
// written: that is what lets a schema describe a node all the way down.
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

func (s irSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	return &irOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}}, nil
}

type irOp struct {
	matchOp
}

func (i irOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("ir op match on %s\n", doc.Path())
	}
	return f(IRView(doc), i.child, ctx)
}

// IRView answers the node's IR representation: an object over the fields of
// ir.Node, named as ir.Node serializes them, holding what the node holds.  A
// field the node does not have is absent.
//
// The view has no parent, so it is not a place in the document and an
// explanation reports it at the node !ir was applied to -- the convention
// !field and !tag follow for the nodes they synthesize.  A document's own
// `age.int` and a view's `int` field would otherwise be reported as the same
// path, which is the one confusion this operator exists to avoid.
//
// The children under fields and values ARE the document's own nodes, so a
// pattern which reaches them is explained at its place in the document.  The
// view is one level deep: a node whose values are nodes has a view whose values
// are those nodes, not views of them.  Writing !ir again asks for the next
// level.
func IRView(doc *ir.Node) *ir.Node {
	view := &ir.Node{Type: ir.ObjectType}
	put := func(name string, val *ir.Node) {
		field := ir.FromString(name)
		field.Parent = view
		field.ParentIndex = len(view.Fields)
		field.ParentField = name
		view.Fields = append(view.Fields, field)
		view.Values = append(view.Values, val)
	}

	put("type", ir.FromString(doc.Type.String()))
	// A container HAS fields and values, empty ones included: an empty object's
	// keys are all integers as much as a full one's are, and `!all` over nothing
	// says so, where an absent field would fail instead.
	if len(doc.Fields) > 0 || doc.Type == ir.ObjectType {
		put("fields", &ir.Node{Type: ir.ArrayType, Values: doc.Fields})
	}
	if len(doc.Values) > 0 || doc.Type == ir.ObjectType || doc.Type == ir.ArrayType {
		put("values", &ir.Node{Type: ir.ArrayType, Values: doc.Values})
	}
	if doc.Tag != "" {
		put("tag", ir.FromString(doc.Tag))
	}
	if len(doc.Lines) > 0 {
		lines := make([]*ir.Node, len(doc.Lines))
		for i, ln := range doc.Lines {
			lines[i] = ir.FromString(ln)
		}
		put("lines", ir.FromSlice(lines))
	}
	if doc.Comment != nil {
		put("comment", doc.Comment)
	}
	if doc.String != "" {
		put("string", ir.FromString(doc.String))
	}
	if doc.Bool {
		put("bool", ir.FromBool(true))
	}
	if doc.Number != "" {
		put("number", ir.FromString(doc.Number))
	}
	if doc.Float64 != nil {
		put("float", ir.FromFloat(*doc.Float64))
	}
	if doc.Int64 != nil {
		put("int", ir.FromInt(*doc.Int64))
	}
	return view
}
