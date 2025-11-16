package libdiff

import "github.com/tony-format/tony/ir"

func MakeDiff(from, to *ir.Node) *ir.Node {
	switch {
	case from == nil:
		if to.Tag == "" {
			return to.Clone().WithTag(InsertTag)
		}
		return to.Clone().WithTag(InsertTag + "(" + to.Tag[1:] + ")")
	case to == nil:
		if from.Tag == "" {
			return from.Clone().WithTag(DeleteTag)
		}
		return from.Clone().WithTag(InsertTag + "(" + from.Tag[1:] + ")")
	default:
		return ir.FromMap(map[string]*ir.Node{
			"from": from,
			"to":   to,
		}).WithTag(ReplaceTag)
	}
}

func MakeTagDiff(from, to string) string {
	switch {
	case from == "":
		return TagInsertTag + "(" + to[1:] + ")"
	case to == "":
		return TagDeleteTag + "(" + from[1:] + ")"
	default:
		return TagReplaceTag + "(" + from[1:] + "," + to[1:] + ")"
	}
}
