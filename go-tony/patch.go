package tony

import (
	"fmt"
	"maps"
	"slices"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
)

// Patch applies a patch to a document. This is the backwards-compatible
// version that doesn't use context. Use PatchWith for schema-aware patching.
func Patch(doc, patch *ir.Node) (*ir.Node, error) {
	return PatchWith(doc, patch, nil)
}

// PatchWith applies a patch to a document with the given context.
// The context carries schema definitions for .[ref] expansion and behavioral options.
func PatchWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	return doPatchWith(doc, patch.Clone(), ctx)
}

type PatchConfig struct {
	Comments bool
}
type PatchOpt func(*PatchConfig)

func PatchComments(v bool) PatchOpt {
	return func(c *PatchConfig) { c.Comments = v }
}

// doPatch is the backwards-compatible version without context
func doPatch(doc, patch *ir.Node) (*ir.Node, error) {
	return doPatchWith(doc, patch, nil)
}

func doPatchWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	if debug.Patch() {
		debug.Logf("patch type %s at %s with tag %q\n", patch.Type, patch.Path(), patch.Tag)
	}
	if doc.Type == ir.CommentType {
		if len(doc.Values) != 0 {
			return doPatchWith(doc.Values[0], patch, ctx)
		}
		panic("comment")
	}
	if patch.Type == ir.CommentType {
		if len(patch.Values) != 0 {
			return doPatchWith(doc, patch.Values[0], ctx)
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
		// Create MatchFunc and PatchFunc that thread ctx through recursive calls
		matchFunc := func(d, p *ir.Node, c *mergeop.OpContext) (bool, error) {
			return MatchWith(d, p, c)
		}
		patchFunc := func(d, p *ir.Node, c *mergeop.OpContext) (*ir.Node, error) {
			return doPatchWith(d, p, c)
		}
		res, err := opInst.Patch(doc, ctx, matchFunc, patchFunc, Diff)
		if err != nil {
			err = fmt.Errorf("%s patching %q gave %w", opInst, encode.MustString(doc), err)
		}
		return res, err
	}
	switch patch.Type {
	case ir.ObjectType:
		return objPatchYWith(doc, patch, ctx)

	case ir.ArrayType:
		if doc.Type != ir.ArrayType {
			return patch.Clone(), nil
		}
		n := min(len(patch.Values), len(doc.Values))
		res := make([]*ir.Node, 0, n)

		for i := range n {
			yy, err := PatchWith(doc.Values[i], patch.Values[i], ctx)
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

// objPatchY is the backwards-compatible version without context
func objPatchY(doc, patch *ir.Node) (*ir.Node, error) {
	return objPatchYWith(doc, patch, nil)
}

func objPatchYWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
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
		yy, err := PatchWith(dy, patch, ctx)
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
		ppv, err := PatchWith(ir.Null(), pv, ctx)
		if err != nil {
			return nil, err
		}
		if ppv != nil {
			dstMap[k] = ppv
		}
	}
	if len(merges) == 0 {
		res := ir.FromMap(dstMap)
		res.Tag = ir.TagRemove(patch.Tag, "!bracket")
		return res, nil
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
	res := ir.FromKeyVals(kvs)
	res.Tag = ir.TagRemove(patch.Tag, "!bracket")
	return res, nil
}
