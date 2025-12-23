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
	return patchArrayByIndex(doc, op.child, ctx, pf, df)
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
	for diffCount <= len(diffMap) {
		op := diffMap[di]
		if op == nil {
			if diffCount == len(diffMap) {
				res = append(res, docVals[fi:]...)
				break
			}
			res = append(res, docVals[fi])
			fi++
			di++
			continue
		}
		diffCount++
		tag, args, _ := ir.TagArgs(op.Tag)
		replTag := ""
		if len(args) == 1 {
			replTag = "!" + args[0]
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
			if df(docVals[fi], from.Clone().WithTag(replTag)) != nil {
				return nil, fmt.Errorf("cannot patch, unexpected value at %s",
					docVals[fi].Path())
			}
			res = append(res, to.Clone().WithTag(replTag))
			di++
			fi++
		case "!insert":
			res = append(res, op.Clone().WithTag(replTag))
			di++
		default:
			tmp, err := pf(docVals[fi], op, ctx)
			if err != nil {
				return nil, err
			}
			res = append(res, tmp)
			di++
			fi++
		}
	}
	return ir.FromSlice(res), nil
}
