package libdiff

import "github.com/signadot/tony-format/tony/ir"

func DiffNumber(from *ir.Node, to *ir.Node) *ir.Node {
	if (from.Int64 == nil) != (to.Int64 == nil) ||
		(from.Float64 == nil) != (to.Float64 == nil) {
		return MakeDiff(from, to)
	}
	if from.Int64 != nil {
		if *from.Int64 != *to.Int64 {
			return MakeDiff(from, to)
		}
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(MakeTagDiff(from.Tag, to.Tag))
	}
	if from.Float64 != nil {
		if *from.Float64 != *to.Float64 {
			return MakeDiff(from, to)
		}
		if from.Tag == to.Tag {
			return nil
		}
		return ir.Null().WithTag(MakeTagDiff(from.Tag, to.Tag))
	}
	panic("number")
}
