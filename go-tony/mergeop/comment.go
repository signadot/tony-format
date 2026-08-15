package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

// Comment is the !comment operator: it states what the comments at this node
// are, and its child names the positions.
//
//	a: !comment {head: ["# new"]}          the head comment at a is now this
//	a: !comment {line: []}                 the line comment at a is now nothing
//	a: !comment {head: ["# h"], line: [" # l"]}   both, in one statement
//
// A position the operand does not name is left alone, as a field an object patch
// does not name is. Both positions in one operand rather than one operator per
// position, because tag composition shares a child -- !comment.comment could only
// ever carry one set of lines, so two changes at one node needed one statement.
//
// It exists so that a comment change is a delta about the COMMENT. Without it the
// only way to say one had changed was to replace the node, which carries the
// value -- the whole subtree, twice, once as from: and once as to: -- for an edit
// to a line of text. A document whose top comment changed rewrote itself.
//
// The lines are the child rather than the argument because comment text is
// arbitrary and the format keeps the leading whitespace of the line as part of
// the comment, which a tag argument cannot hold cleanly.
//
// Setting the lines to nothing is how a comment is removed, so set and clear are
// one absolute statement: the operator says what IS, never what was, which is
// what lets it apply to a base that has moved and be stored (see
// system/logd/api's storage vocabulary). !addtag and !rmtag are the same shape
// for tags, and !retag is the checked form neither of these needs.
var commentSym = &commentSymbol{patchName: commentName}

func Comment() Symbol {
	return commentSym
}

const (
	commentName patchName = "comment"

	// CommentTag is the operator's name, for anything building one.
	CommentTag = string(commentName)

	// CommentHead names the comment written above a value, which the IR holds as
	// a wrapper around it. It is the operand's field name.
	CommentHead = "head"
	// CommentLine names the comment written after a value on its own line, which
	// the IR holds on the node. It is the operand's field name.
	CommentLine = "line"
)

type commentSymbol struct {
	patchName
}

func (s commentSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op takes no args; the operand names the positions, got %v", s, args)
	}
	if child == nil || child.Type != ir.ObjectType {
		return nil, fmt.Errorf("%s op operand is an object naming %q, %q or both", s, CommentHead, CommentLine)
	}
	for _, f := range child.Fields {
		switch f.String {
		case CommentHead, CommentLine:
		default:
			return nil, fmt.Errorf("%s op position is %q or %q, got %q", s, CommentHead, CommentLine, f.String)
		}
	}
	return &commentOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type commentOp struct {
	patchOp
}

// ArgumentOperand says the child is an argument and not a value being installed,
// so the patch's own presentation describes the argument and must not be carried
// onto the result. Without it, "!comment {head: []}" -- whose operand is written
// with braces, as every operand of this shape is -- left the document rendered
// with braces it never had.
func (p commentOp) ArgumentOperand() {}

func (p commentOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("comment op patch on %s\n", doc.Path())
	}
	if doc == nil {
		return nil, fmt.Errorf("%s op has nothing to comment", p)
	}
	res := doc
	// The line comment first: setting the head comment may wrap the node, and
	// the line comment belongs to the value inside the wrapper either way.
	if operand := ir.Get(p.child, CommentLine); operand != nil {
		lines, err := commentLines(operand)
		if err != nil {
			return nil, err
		}
		res = setLineComment(res, lines)
	}
	if operand := ir.Get(p.child, CommentHead); operand != nil {
		lines, err := commentLines(operand)
		if err != nil {
			return nil, err
		}
		res = setHeadComment(res, lines)
	}
	return res, nil
}

// commentLines reads the operand: a list of strings, or null for none.
func commentLines(child *ir.Node) ([]string, error) {
	if child == nil || child.Type == ir.NullType {
		return nil, nil
	}
	if child.Type != ir.ArrayType {
		return nil, fmt.Errorf("comment op operand is a list of lines or null, got %s", child.Type)
	}
	lines := make([]string, 0, len(child.Values))
	for i, v := range child.Values {
		if v.Type != ir.StringType {
			return nil, fmt.Errorf("comment line %d is a %s, and a comment line is a string", i, v.Type)
		}
		lines = append(lines, v.String)
	}
	return lines, nil
}

// setHeadComment wraps doc in a comment node holding lines, or unwraps it when
// there are none.
//
// Creating and removing are exact inverses, which Diff and Reverse depend on: a
// node that had no head comment and is given none is left as it is, rather than
// gaining an empty wrapper that encodes to nothing and compares as different.
func setHeadComment(doc *ir.Node, lines []string) *ir.Node {
	inner := doc
	if doc.Type == ir.CommentType && len(doc.Values) == 1 {
		inner = doc.Values[0]
	}
	if len(lines) == 0 {
		inner.Parent = doc.Parent
		inner.ParentIndex = doc.ParentIndex
		inner.ParentField = doc.ParentField
		return inner
	}
	wrap := &ir.Node{
		Type:        ir.CommentType,
		Lines:       lines,
		Values:      []*ir.Node{inner},
		Parent:      doc.Parent,
		ParentIndex: doc.ParentIndex,
		ParentField: doc.ParentField,
	}
	inner.Parent = wrap
	inner.ParentIndex = 0
	return wrap
}

// setLineComment puts lines on the node itself, which is where a line comment
// lives. A head comment wrapper is looked through: the line comment belongs to
// the value, not to what was said above it.
func setLineComment(doc *ir.Node, lines []string) *ir.Node {
	target := doc
	if doc.Type == ir.CommentType && len(doc.Values) == 1 {
		target = doc.Values[0]
	}
	if len(lines) == 0 {
		target.Comment = nil
		return doc
	}
	target.Comment = &ir.Node{Type: ir.CommentType, Lines: lines, Parent: target}
	return doc
}
