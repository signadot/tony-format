package tony

import (
	"fmt"
	"maps"
	"slices"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/mergeop"
)

func Patch(doc, patch *ir.Node) (*ir.Node, error) {
	return doPatch(doc, patch.Clone())
}

type PatchConfig struct {
	Comments bool
}
type PatchOpt func(*PatchConfig)

func PatchComments(v bool) PatchOpt {
	return func(c *PatchConfig) { c.Comments = v }
}

func doPatch(doc, patch *ir.Node) (*ir.Node, error) {
	if debug.Patch() {
		debug.Logf("patch type %s at %s with tag %q\n", patch.Type, patch.Path(), patch.Tag)
	}
	if doc.Type == ir.CommentType {
		if len(doc.Values) != 0 {
			return doPatch(doc.Values[0], patch)
		}
		panic("comment")
	}
	if patch.Type == ir.CommentType {
		if len(patch.Values) != 0 {
			return doPatch(doc, patch.Values[0])
		}
		panic("comment")
	}
	tag, args, child, err := mergeop.SplitChild(patch)
	if err != nil {
		return nil, err
	}
	if tag != "" {
		op := mergeop.Lookup(tag)
		if op == nil {
			return nil, fmt.Errorf("no mergeop for tag %q", tag)
		}
		opInst, err := op.Instance(child, args)
		if err != nil {
			return nil, err
		}
		res, err := opInst.Patch(doc, Match, doPatch, Diff)
		if err != nil {
			err = fmt.Errorf("%s patching %q gave %w", opInst, encode.MustString(doc), err)
		}
		return res, err
	}
	switch patch.Type {
	case ir.ObjectType:
		return objPatchY(doc, patch)

	case ir.ArrayType:
		if doc.Type != ir.ArrayType {
			return patch.Clone(), nil
		}
		n := min(len(patch.Values), len(doc.Values))
		res := make([]*ir.Node, 0, n)

		for i := range n {
			yy, err := Patch(doc.Values[i], patch.Values[i])
			if err != nil {
				return nil, err
			}
			if yy == nil {
				continue
			}
			res = append(res, yy)
		}
		for i := n; i < len(patch.Values); i++ {
			res = append(res, patch.Values[i])
		}
		out := ir.FromSlice(res)
		return out, nil

	default:
		return patch.Clone(), nil
	}
}

func objPatchY(doc, patch *ir.Node) (*ir.Node, error) {
	//fmt.Printf("obj patch w/out op\ndoc\n%s\npatch\n%s\n", doc.MustString(), patch.MustString())
	var (
		patchMap      = make(map[string]*ir.Node, len(patch.Fields))
		dstMap        = make(map[string]*ir.Node, len(doc.Fields)+len(patch.Fields))
		merges        = make([]*ir.Node, 0)
		mergeLasts    = make([]*string, 0)
		docMerges     = make([]*ir.Node, 0)
		docMergeLasts = make([]*string, 0)
	)
	var lastP *ir.Node
	for i := range patch.Fields {
		field := patch.Fields[i]
		val := patch.Values[i]
		if field.Type == ir.NullType {
			merges = append(merges, val)
			if lastP == nil {
				mergeLasts = append(mergeLasts, nil)
			} else {
				mergeLasts = append(mergeLasts, &lastP.ParentField)
			}
			continue
		}
		patchMap[field.String] = val
		lastP = val
	}
	lastP = nil

	for i := range doc.Fields {
		field := doc.Fields[i]
		dy := doc.Values[i]
		if field.Type == ir.NullType {
			docMerges = append(docMerges, dy)
			if lastP != nil {
				docMergeLasts = append(docMergeLasts, &lastP.ParentField)
			} else {
				docMergeLasts = append(docMergeLasts, nil)
			}
			continue
		}
		lastP = dy
		patch, present := patchMap[field.String]
		if !present {
			dstMap[field.String] = dy
			continue
		}
		yy, err := Patch(dy, patch)
		if err != nil {
			return nil, err
		}
		if yy == nil {
			//fmt.Printf("sub patch nil\n")
			continue
		}
		dstMap[field.String] = yy
		delete(patchMap, field.String)
	}
	//fmt.Printf("dstMap from doc %v\n", dstMap)
	for k, pv := range patchMap {
		_, present := dstMap[k]
		if present {
			continue
		}
		ppv, err := Patch(ir.Null(), pv)
		if err != nil {
			return nil, err
		}
		if ppv != nil {
			dstMap[k] = ppv
		}
	}
	if len(merges) == 0 {
		return ir.FromMap(dstMap), nil
	}
	n := len(dstMap) + len(merges)
	kvs := make([]ir.KeyVal, 0, n)
	mi := 0
	dmi := 0
	dstKeys := slices.Sorted(maps.Keys(dstMap))
	for _, dk := range dstKeys {
		for dmi < len(docMerges) && (docMergeLasts[dmi] == nil || *docMergeLasts[dmi] < dk) {
			kvs = append(kvs, ir.KeyVal{Val: docMerges[dmi]})
			dmi++
		}
		for mi < len(merges) && (mergeLasts[mi] == nil || *mergeLasts[mi] < dk) {
			kvs = append(kvs, ir.KeyVal{Val: merges[mi]})
			mi++
		}
		kvs = append(kvs, ir.KeyVal{
			Key: ir.FromString(dk),
			Val: dstMap[dk],
		})

	}
	for dmi < len(docMerges) {
		kvs = append(kvs, ir.KeyVal{Val: docMerges[dmi]})
		dmi++
	}
	for mi < len(merges) {
		kvs = append(kvs, ir.KeyVal{Val: merges[mi]})
		mi++
	}
	return ir.FromKeyVals(kvs), nil
}
