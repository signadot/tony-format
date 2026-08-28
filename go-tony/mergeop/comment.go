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
// The operand may also carry a VALUE, which is a patch -- or, in a match, a
// pattern -- for what this node holds:
//
//	a: !comment {head: ["# new"], value: {b: !delete null}}
//
// so a node whose comment and whose value both changed is one statement. That is
// what a diff of two states needs: without it the only answer for such a node is to
// state it whole, and an absolute delta stating an object whole MERGES, so whatever
// the new value no longer has stays.
//
// The value is applied FIRST and the comments are stated on the result, because a
// comment describes what the value has become.
//
// A missing value: is not `value: null`. Absent says nothing about the value and
// leaves it alone; present says what it is, and null says it is null.
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
var commentSym = &commentSymbol{name: commentName}

func Comment() Symbol {
	return commentSym
}

const (
	commentName name = "comment"

	// CommentTag is the operator's name, for anything building one.
	CommentTag = string(commentName)

	// CommentHead names the comment written above a value, which the IR holds as
	// a wrapper around it. It is the operand's field name.
	CommentHead = "head"
	// CommentLine names the comment written after a value on its own line, which
	// the IR holds on the node. It is the operand's field name.
	CommentLine = "line"

	// CommentValue names the optional patch or match for the VALUE this node
	// holds, so one node can state both what its comments are and what its value
	// is. It is the operand's field name.
	//
	// It is a field of the operand rather than a second child because tag
	// composition shares one child: !comment.replace hands the same operand to
	// both, and !comment refuses one holding from: and to:. Rather than teach
	// composition to split an operand, the operand carries the other half.
	CommentValue = "value"
)

type commentSymbol struct {
	name
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
		case CommentHead, CommentLine, CommentValue:
		default:
			return nil, fmt.Errorf("%s op operand names %q, %q or %q, got %q",
				s, CommentHead, CommentLine, CommentValue, f.String)
		}
	}
	return &commentOp{op: op{name: s.name, child: child}}, nil
}

type commentOp struct {
	op
}

// ArgumentOperand says the child is an argument and not a value being installed,
// so the patch's own presentation describes the argument and must not be carried
// onto the result. Without it, "!comment {head: []}" -- whose operand is written
// with braces, as every operand of this shape is -- left the document rendered
// with braces it never had.
func (p commentOp) ArgumentOperand() {}

// Match asks of the comments what Patch states about them: a position the operand
// names is compared, exactly, and a position it does not name is not asked about --
// the same silence an object pattern keeps about a field it does not mention.
//
// No lines is the absence of a comment, in both directions: `!comment {head: []}`
// says the node has nothing written above it, which is the question the patch's
// "set it to nothing" answers to.
//
// It asks about the value only when the operand names one, as the patch changes it
// only then. `!and [!comment {...}, <the value>]` is the same question composed, and
// stays the natural spelling by hand; value: is what a patch derived from a diff
// carries, so a match and a patch keep one shape.
//
// The default stays blind, which is the part that could not be an option: with
// comments participating everywhere, two identical comments still mismatched,
// because matchNode has no case which compares them. Asking explicitly is a
// question the walk can answer (8241kcggh12krgh4g1n0).
func (p commentOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("comment op match on %s\n", doc.Path())
	}
	if doc == nil {
		return false, nil
	}
	for _, position := range []struct {
		field string
		lines func(*ir.Node) []string
	}{
		{CommentHead, headCommentLines},
		{CommentLine, lineCommentLines},
	} {
		operand := ir.Get(p.child, position.field)
		if operand == nil {
			continue // not named, not asked
		}
		want, err := commentLines(operand)
		if err != nil {
			return false, err
		}
		if !sameLines(position.lines(doc), want) {
			return false, nil
		}
	}
	// And the value, when the operand names one. A pattern that wants both is
	// still `!and [!comment {...}, <the value>]`, which composes; this is the
	// same question asked in one operand, so that a patch and the match it was
	// derived from have the same shape.
	if operand := ir.Get(p.child, CommentValue); operand != nil {
		return f(doc, operand, ctx)
	}
	return true, nil
}

// headCommentLines answers what is written above doc, which the IR holds as a
// wrapper AROUND it -- so from the value the wrapper is its parent.
//
// Reading it through the parent rather than being handed the wrapper is what lets
// this compose. The walk unwraps a head comment before any op sees the node, which
// is what makes every other match comment-blind; an operation handed the unwrapped
// node can still see what wrapped it, and so can one several compositions deep --
// `!and [!comment {...}, <value>]` needs no special arrangement, because a node
// keeps its parent however it was reached.
func headCommentLines(doc *ir.Node) []string {
	if doc == nil {
		return nil
	}
	if doc.Type == ir.CommentType && len(doc.Values) == 1 {
		return doc.Lines // handed the wrapper itself
	}
	if p := doc.Parent; p != nil && p.Type == ir.CommentType && len(p.Values) == 1 && p.Values[0] == doc {
		return p.Lines
	}
	return nil
}

// lineCommentLines answers what is written after doc on its own line, which the IR
// holds on the node. A head comment wrapper is looked through, as setLineComment
// looks through it: the line comment belongs to the value, not to what was said
// above it.
func lineCommentLines(doc *ir.Node) []string {
	target := doc
	if doc != nil && doc.Type == ir.CommentType && len(doc.Values) == 1 {
		target = doc.Values[0]
	}
	if target == nil || target.Comment == nil {
		return nil
	}
	return target.Comment.Lines
}

// sameLines compares two comments, where no lines and no comment are one thing.
func sameLines(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func (p commentOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("comment op patch on %s\n", doc.Path())
	}
	if doc == nil {
		return nil, fmt.Errorf("%s op has nothing to comment", p)
	}
	res := doc
	// The value first, and the comments stated on the result: a comment describes
	// what the value has become. Absent leaves the value alone, which is not what
	// `value: null` says -- that says the value IS null.
	if operand := ir.Get(p.child, CommentValue); operand != nil {
		next, err := pf(res, operand, ctx)
		if err != nil {
			return nil, err
		}
		if next == nil {
			// The value patch removed the node. Nothing is left to say a comment
			// about, and saying one would put back what was just deleted.
			return nil, nil
		}
		res = next
	}
	// The line comment before the head: setting the head comment may wrap the
	// node, and the line comment belongs to the value inside the wrapper either
	// way.
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
