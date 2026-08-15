package tony

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
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

// differ carries the options down a recursion that hands itself to libdiff as a
// plain DiffFunc.
type differ struct {
	cfg DiffConfig
}

type DiffConfig struct {
	Comments bool
}
type DiffOpt func(*DiffConfig)

func DiffComments(v bool) DiffOpt {
	return func(c *DiffConfig) {
		c.Comments = v
	}
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

func (d *differ) diff(from, to *ir.Node) *ir.Node {
	// With comments in the question, a value whose comment changed is a value
	// that changed: the difference is reported as a replace carrying both sides
	// whole, comments included, which a patch given mergeop.Comments(true)
	// installs. Without it -- the default -- comments are not compared and not
	// carried, which is what a diff of DATA wants.
	if d.cfg.Comments && !sameComments(from, to) {
		return libdiff.MakeDiff(from, to)
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
		return libdiff.MakeDiff(from, to)
	}
	switch from.Type {
	case ir.ObjectType:
		return libdiff.DiffObject(from, to, d.diff)

	case ir.ArrayType:
		return d.diffArray(from, to)

	case ir.NumberType:
		return libdiff.DiffNumber(from, to)

	case ir.StringType:
		return libdiff.DiffString(from, to)
	case ir.BoolType:
		if from.Bool == to.Bool {
			if from.Tag == to.Tag {
				return nil
			}
			return from.Clone().WithTag(libdiff.MakeTagDiff(from.Tag, to.Tag))
		}
		return libdiff.MakeDiff(from, to)

	case ir.NullType:
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(libdiff.MakeTagDiff(from.Tag, to.Tag))
	}
	return nil
}

func (d *differ) diffArray(from, to *ir.Node) *ir.Node {
	_, fromArgs := ir.TagGet(from.Tag, "!key")
	if len(fromArgs) != 1 {
		return libdiff.DiffArrayByIndex(from, to, d.diff)
	}
	_, toArgs := ir.TagGet(to.Tag, "!key")
	if len(toArgs) != 1 {
		return libdiff.DiffArrayByIndex(from, to, d.diff)
	}
	if fromArgs[0] != toArgs[0] {
		return libdiff.DiffArrayByIndex(from, to, d.diff)
	}
	if _, err := ir.ParsePath("$." + fromArgs[0]); err != nil {
		return libdiff.DiffArrayByIndex(from, to, d.diff)
	}
	res, err := libdiff.DiffArrayByKey(from, to, fromArgs[0], d.diff)
	if err != nil {
		return libdiff.DiffArrayByIndex(from, to, d.diff)
	}
	return res
}
