package tony

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// Diff produces a succint comparison of from and to.  If there are
// no differences, Diff returns nil.
//
// A resulting diff may be reversed using [libdiff.Reverse].
//
// A resulting diff may be used as a patch in [Patch].
//
// The structure returned by Diff contains a minimal set of changes
// indicated by yaml tags which double as patch operations.
//
//   - if the types of from and to differ then the result is a node
//     !replace
//     from: from
//     to: to
//
//   - for ObjectType any field f in to but not in from has a field
//     `f: !delete[(<orig-tag>)] ...`
//
//   - for ObjectType any field f in from but not in to has a field
//     `f: !insert[(<orig-tag>)] ...`
//
//   - for any field f shared by from and to which is equal, it is absent
//     in the result.
//
//   - for any field f with a difference, it contains a diff of the value
//     of f in from and respectively to.
//
// For ArrayType nodes which differ, if both nodes are tagged by
// the same key with !key(<key>), they are treated as objects but presented
// as an array with tag !key(<key>).
//
// For StringTypes, a string diff may computed and if the size of the string
// diff is less than half the size of the the smallest string
//
// If only the tags differ, the tags !addtag(<tag>) !rmtag(<tag>) and !retag(<from>,<to>)
// will be present decorating a null.
func Diff(from, to *ir.Node) *ir.Node {
	return (&differ{}).diff(from, to)
}

// DiffWith is Diff with options.  Diff itself cannot take them: it is passed
// around as a libdiff.DiffFunc, whose signature is exactly func(from, to)
// *ir.Node, so the options live here as they do for MatchWith and PatchWith.
//
// DiffComments existed before this did, and there was no way to pass it.
func DiffWith(from, to *ir.Node, opts ...DiffOpt) *ir.Node {
	d := &differ{}
	for _, opt := range opts {
		opt(&d.cfg)
	}
	return d.diff(from, to)
}

type differ struct {
	cfg DiffConfig
}

type DiffConfig struct {
	Comments bool
	Absolute bool
}
type DiffOpt func(*DiffConfig)

func DiffComments(v bool) DiffOpt {
	return func(c *DiffConfig) {
		c.Comments = v
	}
}

// DiffAbsolute restricts the diff to primitives whose result STATES what the value
// is, rather than how it relates to what was there.
//
// The default answer is the smaller one and the right one for a diff a human reads,
// or for a patch applied to the document it was taken against: !replace{from,to}
// checks that the value is still what it was, !strdiff names an edit to the string
// that was there, !arraydiff names positions in the array that was there. Every one
// of those consults the base.
//
// A store cannot promise the base. Baseline advances underneath a scope, and a
// snapshot re-materializes a document these were never applied to, so what a store
// keeps has to mean the same thing against a base that has moved. With this on, the
// three above answer with the new value itself: to state a result rather than an
// edit is to carry it.
//
// It costs size, and only where it has to. An object diffs field by field and a
// KEYED array element by element, both unchanged -- it is a positional array and a
// string that come out whole, because there is no way to say "this position" or
// "this substring" without naming what was there to count from.
//
// It is not yet the whole of absoluteness. !retag survives, from tag differences
// libdiff builds directly, and a container answered whole MERGES rather than
// replaces -- both because the vocabulary has no primitive meaning "the value here
// is exactly this" (5k4a6drqh12ksnsaj5n0).
func DiffAbsolute(v bool) DiffOpt {
	return func(c *DiffConfig) {
		c.Absolute = v
	}
}

// makeDiff answers the difference between two values that are not being descended
// into. Absolute: the new value, which needs no base to mean what it means.
func (d *differ) makeDiff(from, to *ir.Node) *ir.Node {
	if d.cfg.Absolute {
		return to.Clone()
	}
	return libdiff.MakeDiff(from, to)
}

// sameComments reports whether two nodes carry the same comments: the head
// comment a CommentType wrapper holds, and the line comment on the node itself.
// A node with no comment and a node with an empty one are the same.
func sameComments(from, to *ir.Node) bool {
	if !sameLines(headLines(from), headLines(to)) {
		return false
	}
	return sameLines(lineLines(unwrapComment(from)), lineLines(unwrapComment(to)))
}

func headLines(n *ir.Node) []string {
	if n != nil && n.Type == ir.CommentType {
		return n.Lines
	}
	return nil
}

func lineLines(n *ir.Node) []string {
	if n != nil && n.Comment != nil {
		return n.Comment.Lines
	}
	return nil
}

func unwrapComment(n *ir.Node) *ir.Node {
	for n != nil && n.Type == ir.CommentType && len(n.Values) == 1 {
		n = n.Values[0]
	}
	return n
}

func sameLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// commentDiff answers the operator that turns from's comments into to's, or nil
// when the values differ as well and the value's own difference should carry
// them.
//
// Both positions can change at once, and one operand states both: tag
// composition shares a child, so two operators on one node could only ever
// carry one set of lines.
func (d *differ) commentDiff(from, to *ir.Node) *ir.Node {
	// Everything except the comments AT THIS NODE has to be identical, deeper
	// comments included, or the small operator would state this node's comments
	// and silently drop whatever else moved. DeepEqual is comment blind, so it
	// answered yes to a document whose comments had changed further down.
	a, b := ir.Uncomment(from).Clone(), ir.Uncomment(to).Clone()
	a.Comment, b.Comment = nil, nil
	if !a.DeepEqualWithComments(b) {
		return nil // something else moved; MakeDiff states it whole, comments included
	}
	positions := map[string]*ir.Node{}
	if !sameLines(headLines(from), headLines(to)) {
		positions[mergeop.CommentHead] = linesNode(headLines(to))
	}
	if !sameLines(lineLines(unwrapComment(from)), lineLines(unwrapComment(to))) {
		positions[mergeop.CommentLine] = linesNode(lineLines(unwrapComment(to)))
	}
	if len(positions) == 0 {
		return nil
	}
	op := ir.FromMap(positions)
	op.Tag = "!" + mergeop.CommentTag
	return op
}

// linesNode is a comment's lines as the operand holds them: a list of strings,
// empty when the comment is gone.
func linesNode(lines []string) *ir.Node {
	vals := make([]*ir.Node, 0, len(lines))
	for _, ln := range lines {
		vals = append(vals, ir.FromString(ln))
	}
	return ir.FromSlice(vals)
}

func (d *differ) diff(from, to *ir.Node) *ir.Node {
	// With comments in the question, a comment that changed is reported as a
	// change to the COMMENT -- !comment(head) or !comment(line), carrying the
	// lines -- rather than as a replacement of the value it describes. Replacing
	// carried the whole subtree twice for an edit to a line of text, and at the
	// root that is the document.
	//
	// A value that ALSO changed is a different matter: the value's own difference
	// carries its comments with it, and a node gets one statement, not two.
	// Without the option -- the default -- comments are neither compared nor
	// carried, which is what a diff of DATA wants.
	if d.cfg.Comments && !sameComments(from, to) {
		if cd := d.commentDiff(from, to); cd != nil {
			return cd
		}
		return d.makeDiff(from, to)
	}
	if from.Type == ir.CommentType {
		if len(from.Values) != 0 {
			return d.diff(from.Values[0], to)
		}
		panic("comment")
	}
	if to.Type == ir.CommentType {
		if len(to.Values) != 0 {
			return d.diff(from, to.Values[0])
		}
		panic("comment")
	}
	if from.Type != to.Type {
		return d.makeDiff(from, to)
	}
	switch from.Type {
	case ir.ObjectType:
		return libdiff.DiffObject(from, to, d.diff)

	case ir.ArrayType:
		return d.diffArray(from, to)

	case ir.NumberType:
		if d.cfg.Absolute {
			if from.DeepEqual(to) {
				return nil
			}
			return to.Clone()
		}
		return libdiff.DiffNumber(from, to)

	case ir.StringType:
		if d.cfg.Absolute {
			// !strdiff is an edit script counted from the string that was there.
			if from.String == to.String && from.Tag == to.Tag {
				return nil
			}
			return to.Clone()
		}
		return libdiff.DiffString(from, to)
	case ir.BoolType:
		if from.Bool == to.Bool {
			if from.Tag == to.Tag {
				return nil
			}
			return from.Clone().WithTag(libdiff.MakeTagDiff(from.Tag, to.Tag))
		}
		return d.makeDiff(from, to)

	case ir.NullType:
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(libdiff.MakeTagDiff(from.Tag, to.Tag))
	}
	return nil
}

// diffArrayByIndex is the positional answer, and the one that cannot be stated
// absolutely: !arraydiff names positions in the array that WAS there. Absolute
// carries the new array whole.
func (d *differ) diffArrayByIndex(from, to *ir.Node) *ir.Node {
	if d.cfg.Absolute {
		if from.DeepEqual(to) {
			return nil
		}
		return to.Clone()
	}
	return libdiff.DiffArrayByIndex(from, to, d.diff)
}

func (d *differ) diffArray(from, to *ir.Node) *ir.Node {
	_, fromArgs := ir.TagGet(from.Tag, "!key")
	if len(fromArgs) != 1 {
		return d.diffArrayByIndex(from, to)
	}
	_, toArgs := ir.TagGet(to.Tag, "!key")
	if len(toArgs) != 1 {
		return d.diffArrayByIndex(from, to)
	}
	if fromArgs[0] != toArgs[0] {
		return d.diffArrayByIndex(from, to)
	}
	if _, err := ir.ParsePath("$." + fromArgs[0]); err != nil {
		return d.diffArrayByIndex(from, to)
	}
	res, err := libdiff.DiffArrayByKey(from, to, fromArgs[0], d.diff)
	if err != nil {
		return d.diffArrayByIndex(from, to)
	}
	return res
}
