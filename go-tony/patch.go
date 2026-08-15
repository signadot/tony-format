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

// Patch applies a patch to a document with optional configuration.
// Use PatchWith for schema-aware patching with full OpContext control.
func Patch(doc, patch *ir.Node, opts ...mergeop.PatchOpt) (*ir.Node, error) {
	cfg := mergeop.NewConfig(opts...)
	ctx := &mergeop.OpContext{Config: cfg}
	return patchAndAnswer(doc, patch, ctx)
}

// PatchWith applies a patch to a document with the given context.
// The context carries schema definitions for .[ref] expansion and behavioral options.
func PatchWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	return patchAndAnswer(doc, patch, ctx)
}

// patchAndAnswer applies the patch and then honours the comment option on the
// way out.
//
// Without it the result carries no comments at all. That takes stripping rather
// than merely not preserving: a head comment is a wrapper, discarded by anything
// that descends through it, while a line comment rides on the node and every
// clone carries it along -- so "comments off" kept the line comments and dropped
// the head ones, which is not a policy, it is two accidents.
func patchAndAnswer(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	res, err := doPatchWith(doc, patch.Clone(), ctx)
	if err != nil || res == nil {
		return res, err
	}
	if ctx == nil || ctx.Config == nil || !ctx.Config.Comments {
		res = ir.StripComments(res)
	}
	return res, nil
}

// doPatch is the backwards-compatible version without context
func doPatch(doc, patch *ir.Node) (*ir.Node, error) {
	return doPatchWith(doc, patch, nil)
}

func doPatchWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	if debug.Patch() {
		debug.Logf("patch type %s at %s with tag %q\n", patch.Type, patch.Path(), patch.Tag)
	}
	// A comment wraps the value it describes. Patching is about the value, so
	// both sides are unwrapped to reach it -- and with mergeop.Comments(true) the
	// wrapper is put back, so a store which keeps comments can keep them through
	// a write. Off by default: the option existed and nothing read it, and every
	// caller today expects a patch to answer with data.
	keepComments := ctx != nil && ctx.Config != nil && ctx.Config.Comments
	var docComment, patchComment *ir.Node
	if doc.Type == ir.CommentType {
		if len(doc.Values) == 0 {
			panic("comment")
		}
		docComment, doc = doc, doc.Values[0]
	}
	if patch.Type == ir.CommentType {
		if len(patch.Values) == 0 {
			panic("comment")
		}
		patchComment, patch = patch, patch.Values[0]
	}
	if docComment != nil || patchComment != nil {
		res, err := doPatchWith(doc, patch, ctx)
		if err != nil || res == nil || !keepComments {
			return res, err
		}
		// The patch's comment is what the writer just said about the value.
		//
		// The document's stands only when the patch is a structural merge which
		// said nothing about it. An OPERATION states the whole value -- a
		// !replace installs its to: comment and all -- so re-applying the old
		// comment there stacked one on the other ("# old\n# new") and made a
		// removed comment come back.
		keep := patchComment
		if keep == nil {
			if _, opTag, _, _, err := mergeop.SplitChild(patch); err == nil && opTag != "" {
				return res, nil
			}
			keep = docComment
		}
		return rewrapComment(res, keep, nil), nil
	}
	preTag, tag, args, child, err := mergeop.SplitChild(patch)
	if err != nil {
		return nil, err
	}
	if tag != "" {
		op := mergeop.Lookup(tag)
		if op == nil {
			return nil, fmt.Errorf("no mergeop for tag %q", tag)
		}
		if ctx != nil && ctx.Config != nil && ctx.Config.RejectUnsafe && mergeop.Unsafe(tag) {
			return nil, fmt.Errorf("unsafe operation %q rejected", tag)
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
		// Restore the non-op part of the patch's tag onto the result -- unless the op
		// already produced it. Most ops return a fresh value carrying no tag of their
		// own, but one that answers with the DOCUMENT's tag (!key, which must stay a
		// keyed list) hands back a tag that already holds this one, and composing again
		// duplicated it: patching !bracket.key(name)[...] gave a result tagged
		// !bracket.bracket.key(name), so Patch(a, Diff(a,b)) did not equal b.
		if res != nil && preTag != "" {
			res.Tag = restoreTag(preTag, res.Tag)
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

// rewrapComment puts a head comment back around a patched value, preferring the
// one the patch carried: it is the more recent statement about the value.
func rewrapComment(res, fromPatch, fromDoc *ir.Node) *ir.Node {
	src := fromPatch
	if src == nil {
		src = fromDoc
	}
	// A result which already carries a comment got it from what was applied,
	// and that is the more recent statement.
	if src == nil || res.Type == ir.CommentType {
		return res
	}
	wrap := &ir.Node{Type: ir.CommentType, Lines: src.Lines, Values: []*ir.Node{res}}
	res.Parent = wrap
	res.ParentIndex = 0
	return wrap
}

// restoreTag composes each label of pre onto tag, skipping any the tag already
// carries.
//
// It is label by label because pre is a whole tag and can hold several: the
// check this replaces asked ir.TagHas whether the tag held pre ENTIRE, which a
// composed pre such as !t1.bracket never is, so it composed again and the result
// came back tagged !t1.bracket.t1.bracket -- and Patch(a, Diff(a,b)) stopped
// equalling b for any document whose tag was composed. The single-label case it
// was written for, !bracket over a !key that already answered with one, is the
// same question asked once per label.
func restoreTag(pre, tag string) string {
	var labels []struct {
		head string
		args []string
	}
	for t := pre; t != ""; {
		head, args, rest := ir.TagArgs(t)
		labels = append(labels, struct {
			head string
			args []string
		}{head, args})
		t = rest
	}
	// Rightmost first, so the composed order is the order pre had.
	for i := len(labels) - 1; i >= 0; i-- {
		if ir.TagHas(tag, labels[i].head) {
			continue
		}
		tag = ir.TagCompose(labels[i].head, labels[i].args, tag)
	}
	return tag
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
		// consumed whatever it produced: a patch which removes the field is
		// still a patch which was applied, and the leftover loop below would
		// otherwise apply it a second time to a null.
		delete(patchMap, field.String)
		if yy == nil {
			//fmt.Printf("sub patch nil\n")
			continue
		}
		dstMap[field.String] = yy
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
		patchTag := ir.StripPresentation(patch.Tag)
		if doc.Tag != "" {
			res.Tag = ir.TagCompose(doc.Tag, nil, patchTag)
		} else {
			res.Tag = patchTag
		}
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
	patchTag := ir.StripPresentation(patch.Tag)
	if doc.Tag != "" {
		res.Tag = ir.TagCompose(doc.Tag, nil, patchTag)
	} else {
		res.Tag = patchTag
	}
	return res, nil
}
