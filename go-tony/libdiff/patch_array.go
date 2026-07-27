package libdiff

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

func PatchArrayByIndex(doc, patch *ir.Node, df DiffFunc) (*ir.Node, error) {
	diffMap, err := patch.ToIntKeysMap()
	if err != nil {
		return nil, err
	}
	res := []*ir.Node{}

	docVals := doc.Values
	fi, di := uint32(0), uint32(0)
	n := uint32(len(docVals))
	diffCount := 0
	for diffCount <= len(diffMap) {
		op := diffMap[di]
		if op == nil {
			if diffCount == len(diffMap) {
				if fi < n {
					res = append(res, docVals[fi:]...)
				}
				break
			}
			if fi < n {
				res = append(res, docVals[fi])
				fi++
			}
			di++
			continue
		}
		diffCount++
		tag, args, rest := ir.TagArgs(op.Tag)
		replTag := ""
		switch {
		case len(args) == 1:
			replTag = "!" + args[0]
		case hasTag(rest, RawTag):
			// the value holds merge operations as data: the escape is
			// consumed here, as it is in a patch, and the value keeps its own
			// tags.  Nothing beneath is interpreted -- this path installs the
			// value, it does not walk it.
			replTag = ir.TagRemove(rest, RawTag)
		}

		switch tag {
		case "!delete":
			if d := df(docVals[fi], op.Clone().WithTag(replTag)); d != nil {
				return nil, fmt.Errorf(
					"cannot patch, unexpected value at %s",
					docVals[fi].Path())
			}
			fi++
			di++
		case "!replace":
			if op.Type != ir.ObjectType {
				return nil, fmt.Errorf(
					"invalid arraydiff, got type %s at %s",
					op.Type,
					op.Path())
			}
			to := ir.Get(op, "to")
			if to == nil {
				return nil, fmt.Errorf(
					"invalid arraydiff, missing 'to:' under !replace at %s",
					op.Path())
			}
			from := ir.Get(op, "from")
			if from == nil {
				return nil, fmt.Errorf(
					"invalid arraydiff, missing 'from:' under !replace at %s",
					op.Path())
			}
			// from: and to: are whole values, tags included -- a !replace
			// never carries the tag as an argument the way !insert and
			// !delete do, so there is nothing here to put back.
			if df(docVals[fi], from.Clone()) != nil {
				return nil, fmt.Errorf("cannot patch, unexpected value at %s",
					docVals[fi].Path())
			}
			res = append(res, to.Clone())
			di++
			fi++
		case "!insert":
			res = append(res, op.Clone().WithTag(replTag))
			di++
		default:
			return nil, fmt.Errorf("unexpected tag from arraydiff op: %q (%s)", tag, encode.MustString(op))
		}
	}
	return ir.FromSlice(res), nil
}
