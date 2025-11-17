package tony

import (
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
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
	res := doDiff(from, to)
	return res
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

func doDiff(from, to *ir.Node) *ir.Node {
	if from.Type == ir.CommentType {
		if len(from.Values) != 0 {
			return doDiff(from.Values[0], to)
		}
		panic("comment")
	}
	if to.Type == ir.CommentType {
		if len(to.Values) != 0 {
			return doDiff(from, to.Values[0])
		}
		panic("comment")
	}
	if from.Type != to.Type {
		return libdiff.MakeDiff(from, to)
	}
	switch from.Type {
	case ir.ObjectType:
		return libdiff.DiffObject(from, to, doDiff)

	case ir.ArrayType:
		return diffArray(from, to)

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

func diffArray(from, to *ir.Node) *ir.Node {
	_, fromArgs := ir.TagGet(from.Tag, "!key")
	if len(fromArgs) != 1 {
		return libdiff.DiffArrayByIndex(from, to, doDiff)
	}
	_, toArgs := ir.TagGet(to.Tag, "!key")
	if len(toArgs) != 1 {
		return libdiff.DiffArrayByIndex(from, to, doDiff)
	}
	if fromArgs[0] != toArgs[0] {
		return libdiff.DiffArrayByIndex(from, to, doDiff)
	}
	if _, err := ir.ParsePath("$." + fromArgs[0]); err != nil {
		return libdiff.DiffArrayByIndex(from, to, doDiff)
	}
	res, err := libdiff.DiffArrayByKey(from, to, fromArgs[0], doDiff)
	if err != nil {
		return libdiff.DiffArrayByIndex(from, to, doDiff)
	}
	return res
}
