package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var embedSym = &embedSymbol{patchName: embedName}

func Embed() Symbol {
	return embedSym
}

const (
	embedName patchName = "embed"
)

type embedSymbol struct {
	patchName
}

func (s embedSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 arg (key), got %v", s, args)
	}
	return &embedOp{key: args[0], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type embedOp struct {
	patchOp
	key string
}

func (kl embedOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("embed op patch on %s\n", doc.Path())
	}
	toEmbed := kl.child.Clone()
	return kl.embed(doc, toEmbed)
}

func (kl *embedOp) embed(doc, in *ir.Node) (*ir.Node, error) {
	switch in.Type {
	case ir.ObjectType:
		res := make([]ir.KeyVal, len(in.Fields))
		for i := range in.Fields {
			field := in.Fields[i]
			newVal, err := kl.embed(doc, in.Values[i])
			if err != nil {
				return nil, err
			}
			res[i] = ir.KeyVal{Key: field.Clone(), Val: newVal}
		}
		return ir.FromKeyVals(res), nil
	case ir.ArrayType:
		res := make([]*ir.Node, len(in.Values))
		for i, child := range in.Values {
			newChild, err := kl.embed(doc, child)
			if err != nil {
				return nil, err
			}
			res[i] = newChild
		}
		return ir.FromSlice(res), nil
	default:
		if in.Type != ir.StringType {
			return in, nil
		}
		if in.String != kl.key {
			return in, nil
		}
		res := doc.Clone()
		res.Parent = nil
		res.ParentIndex = 0
		res.ParentField = ""
		return res, nil
	}
}
