package mergeop

import (
	"fmt"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
)

var diveSym = &diveSymbol{patchName: diveName}

func Dive() Symbol {
	return diveSym
}

const (
	diveName patchName = "dive"
)

type diveSymbol struct {
	patchName
}

func (s diveSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	patches, err := getPatches(child)
	if err != nil {
		return nil, err
	}
	return &diveOp{patches: patches, patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

func getPatches(node *ir.Node) ([]divePatch, error) {
	if node.Type != ir.ArrayType {
		return nil, fmt.Errorf("expected list")
	}
	res := make([]divePatch, len(node.Values))
	for i := range node.Values {
		divePatch, err := getPatch(node.Values[i])
		if err != nil {
			return nil, err
		}
		res[i] = *divePatch
	}
	return res, nil
}

func getPatch(node *ir.Node) (*divePatch, error) {
	if node.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected an object")
	}
	m := ir.ToMap(node)
	res := &divePatch{}
	res.Match = m["match"]
	res.Patch = m["patch"]
	if res.Patch == nil {
		return nil, fmt.Errorf("field 'patch' is required")
	}
	return res, nil
}

type diveOp struct {
	patchOp
	patches []divePatch
}

type divePatch struct {
	Match *ir.Node `json:"match,omitempty"`
	Patch *ir.Node `json:"patch"`
}

func (d *diveOp) Dive(doc *ir.Node, mf MatchFunc, pf PatchFunc) (*ir.Node, error) {
	switch doc.Type {
	case ir.ObjectType:
		out := make([]ir.KeyVal, len(doc.Fields))
		for i := range doc.Fields {
			fieldVal, err := d.Dive(doc.Values[i], mf, pf)
			if err != nil {
				return nil, err
			}
			out[i] = ir.KeyVal{Key: doc.Fields[i].Clone(), Val: fieldVal}
		}
		return d.do(ir.FromKeyVals(out), mf, pf)

	case ir.ArrayType:
		out := make([]*ir.Node, len(doc.Values))
		for i := range doc.Values {
			res, err := d.Dive(doc.Values[i], mf, pf)
			if err != nil {
				return nil, err
			}
			out[i] = res
		}
		return d.do(ir.FromSlice(out), mf, pf)
	default:
		return d.do(doc, mf, pf)
	}
}

func (d *diveOp) do(doc *ir.Node, mf MatchFunc, pf PatchFunc) (*ir.Node, error) {
	var (
		patchDoc = doc
		err      error
	)
	for i := range d.patches {
		p := &d.patches[i]
		if p.Match != nil {
			ok, err := mf(patchDoc, p.Match)
			if err != nil {
				return nil, err
			}
			if !ok {
				//fmt.Printf("# no match\n%s\n---\n# on\n%s",
				//	p.Match.MustString(), patchDoc.MustString())
				continue
			}
			//fmt.Printf("# match\n%s\n---\n# on\n%s",
			//	p.Match.MustString(), patchDoc.MustString())
		}
		patchDoc, err = pf(patchDoc, p.Patch.Clone())
		if err != nil {
			return nil, err
		}
	}
	return patchDoc, nil
}

func (dive diveOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("dive op called on %s\n", doc.Path())
	}
	res, err := dive.Dive(doc, mf, pf)
	if err != nil {
		return nil, err
	}
	res.ParentField = doc.ParentField
	res.ParentIndex = doc.ParentIndex
	res.Parent = doc.Parent
	return res, nil
}
