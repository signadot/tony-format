package mergeop

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var arrayDiffSym = &arrayDiffSymbol{arrayDiffName}

func ArrayDiff() Symbol {
	return arrayDiffSym
}

const (
	arrayDiffName patchName = "arraydiff"
)

type arrayDiffSymbol struct {
	patchName
}

func (s arrayDiffSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	if child.Type != ir.ObjectType {
		return nil, errors.New("arraydiff op needs an object")
	}
	return &arrayDiffOp{
		patchOp: patchOp{op: op{name: s.patchName, child: child}},
	}, nil
}

type arrayDiffOp struct {
	patchOp
}

func (op arrayDiffOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch op arraydiff on %s\n", doc.Path())
	}

	if doc.Type != ir.ArrayType {
		return nil, fmt.Errorf("arraydiff only applies to arrays, got %s at %s", doc.Type, doc.Path())
	}
	res, err := patchArrayByIndex(doc, op.child, ctx, pf, df)
	if err != nil {
		return nil, err
	}
	return retagFromDiff(doc, res, op.child, ctx, pf)
}

func patchArrayByIndex(doc, patch *ir.Node, ctx *OpContext, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	diffMap, err := patch.ToIntKeysMap()
	if err != nil {
		return nil, err
	}
	res := []*ir.Node{}

	docVals := doc.Values
	fi, di := uint32(0), uint32(0)
	diffCount := 0
	// A key of an arraydiff is a position in the sequence the two arrays share,
	// which every slot advances by one, and fi is where in the document that
	// leaves us.  A patch whose keys claim more of the document than it has is
	// malformed; say so rather than reading off the end of it.
	overrun := func(what string) error {
		return fmt.Errorf(
			"invalid arraydiff at %s: %s at key %d reaches element %d of %d",
			patch.Path(), what, di, fi, len(docVals))
	}
	for diffCount <= len(diffMap) {
		op := diffMap[di]
		if op == nil {
			if diffCount == len(diffMap) {
				res = append(res, docVals[fi:]...)
				break
			}
			if int(fi) >= len(docVals) {
				return nil, overrun("unchanged element")
			}
			res = append(res, docVals[fi])
			fi++
			di++
			continue
		}
		diffCount++
		// The operation is the first label of the tag chain this package KNOWS, not
		// simply the first label.  A composed tag may carry labels which are not
		// operations ahead of the one which is, and every other dispatch here finds
		// the op with SplitChild rather than by demanding it come first.  Switching
		// on the raw head instead let any leading label MASK the op: logd's
		// !logd-patch-root marker turned !insert into a positional patch, which
		// overwrote the element it was meant to insert before, and !delete into a
		// patch of a null, which panicked every reader of the store
		// (jjbapb1ah12kranxg5n0).
		//
		// The labels AHEAD of the op belong to the value and are put back on it.
		// They were never part of the op's contract -- the labels after it are,
		// which is why those are restored by name (!insert(tag)) or by !raw and
		// otherwise dropped. Parsing alone puts one there: `!delete {by: scott}`
		// reads as !bracket.delete, so wiping the chain compared a braceless object
		// against a document element which had the brace, and a delete of anything
		// but a scalar could not match.
		head, _, _ := ir.TagArgs(op.Tag) // for the message; the op may be deeper
		preTag, tag, args, child, err := SplitChild(op)
		if err != nil {
			return nil, err
		}
		rest := ""
		if child != nil {
			rest = child.Tag
		}
		replTag := ""
		switch {
		case len(args) == 1:
			replTag = "!" + args[0]
		case ir.TagHas(rest, libdiff.RawTag):
			// the element holds merge operations as data: the escape is
			// consumed here, as it is in a patch, and the element keeps its
			// own tags.  Nothing beneath it is interpreted -- this installs
			// the element, it does not walk it.
			replTag = ir.TagRemove(rest, libdiff.RawTag)
		}
		if preTag != "" {
			if replTag == "" {
				replTag = preTag
			} else {
				replTag = ir.TagCompose(replTag, nil, preTag)
			}
		}
		switch tag {
		case "delete":
			if int(fi) >= len(docVals) {
				return nil, overrun("delete")
			}
			if d := df(docVals[fi], op.Clone().WithTag(replTag)); d != nil {
				return nil, fmt.Errorf(
					"cannot patch, unexpected value at %s",
					docVals[fi].Path())
			}
			fi++
			di++
		case "replace":
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
			if int(fi) >= len(docVals) {
				return nil, overrun("replace")
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
		case "insert":
			res = append(res, op.Clone().WithTag(replTag))
			di++
		default:
			if int(fi) >= len(docVals) {
				return nil, overrun("patch " + head)
			}
			tmp, err := pf(docVals[fi], op, ctx)
			if err != nil {
				return nil, err
			}
			// No node is what a patch which deleted the element answers with,
			// and an element that is gone is GONE, not a hole: keeping the nil
			// put it in the slice for ir.FromSlice to dereference. The element
			// is still consumed from the document -- it was matched, it just
			// produced nothing.
			if tmp != nil {
				res = append(res, tmp)
			}
			di++
			fi++
		}
	}
	return ir.FromSlice(res), nil
}
