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
	// !comment is the one operation whose subject is the comment, so it is the
	// one that must meet the document with its wrapper still on: unwrapping here
	// handed it the value and dropped what it was about to state. It falls
	// through to the operator dispatch below untouched.
	commentOp := false
	if _, opTag, _, _, err := mergeop.SplitChild(patch); err == nil && opTag == mergeop.CommentTag {
		commentOp = true
	}
	var docComment, patchComment *ir.Node
	if !commentOp && doc.Type == ir.CommentType {
		if len(doc.Values) == 0 {
			panic("comment")
		}
		docComment, doc = doc, doc.Values[0]
	}
	if !commentOp && patch.Type == ir.CommentType {
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
		// An op whose child is an ARGUMENT leaves the document's presentation
		// alone: the braces on "!comment {head: []}" describe the operand.
		if _, isArg := opInst.(mergeop.ArgumentOperand); res != nil && preTag != "" && !isArg {
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
		// Past the end of the document the element is being introduced, and its
		// patch is still a PATCH: it can carry ops, so it is applied against an
		// absent document rather than appended as data. Appending it raw left
		// the op tag itself in the result -- patching [1, 2] with
		// [1, 2, !delete null] gave [1, 2, !delete null], storing a delete
		// marker as though it were a value -- and made the same element mean two
		// things depending on where it fell: !delete at index 0 removed the
		// element, !delete past the end became one.
		//
		// Absent is null here, the reading the object path and !key both use. An
		// op that resolves to nothing -- a !delete for an element the document
		// never had -- drops out instead of being stored verbatim.
		for i := n; i < len(patch.Values); i++ {
			yy, err := PatchWith(absentAt(doc, "", i), patch.Values[i], ctx)
			if err != nil {
				return nil, err
			}
			if yy == nil {
				continue
			}
			res = append(res, yy)
		}
		out := ir.FromSlice(res)
		out.Tag = mergedTag(doc, patch)
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

// objMergeFast merges patch into doc without going through a map: it walks the two
// field lists, which are already in key order, and rebuilds only what changed. The
// second result is false when the shape is not the one this handles, and then the
// general path below runs.
//
// It exists because the general path rebuilds the WHOLE object for a patch of one
// field: every field into a map, the keys sorted, and a fresh key node allocated for
// each. That is O(fields) with a large constant at the level being patched, which for
// a reconciler writing one path in a set of thousands is the whole cost of the write
// -- and it is paid again by every watcher stepping the same commit, and by the store's
// own head. Measured on a 3000-field object: 557µs through the map, 149µs here
// (v552mdbqh12kr7dtgdn0).
//
// The conditions are the ordinary case and nothing more: both sides plain objects with
// string keys in ascending order and no !merge fields. Anything else -- an unsorted
// document, a merge, a keyed array -- takes the path it always took.
func objMergeFast(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, bool, error) {
	if !sortedStringFields(doc) || !sortedStringFields(patch) {
		return nil, false, nil
	}

	res := &ir.Node{Type: ir.ObjectType}
	n := len(doc.Fields) + len(patch.Fields)
	keys := make([]string, 0, n)
	values := make([]*ir.Node, 0, n)
	// keysKept says the result holds exactly the document's keys, in the document's
	// order: nothing added, nothing removed. Then the result SHARES the document's field
	// slice rather than building an identical one, which is the difference between a fold
	// that costs the size of the document and one that costs a memcpy -- at three
	// thousand fields, 270µs to allocate a key node each against 53µs to share them
	// (rkb7p8v5h12ksdnmgsn0).
	keysKept := true

	add := func(key string, val *ir.Node, fromDoc bool) {
		if val == nil {
			keysKept = false // a patch which removed the field
			return
		}
		if !fromDoc {
			keysKept = false // a key the patch introduced
		}
		keys = append(keys, key)
		values = append(values, val)
	}

	di, pi := 0, 0
	for di < len(doc.Fields) && pi < len(patch.Fields) {
		dk, pk := doc.Fields[di].String, patch.Fields[pi].String
		switch {
		case dk < pk:
			add(dk, doc.Values[di], true)
			di++
		case pk < dk:
			// A field the document does not have: what the patch means on its own.
			val, err := PatchWith(absentAt(doc, pk, pi), patch.Values[pi], ctx)
			if err != nil {
				return nil, false, err
			}
			add(pk, val, false)
			pi++
		default:
			val, err := PatchWith(doc.Values[di], patch.Values[pi], ctx)
			if err != nil {
				return nil, false, err
			}
			add(dk, val, true)
			di++
			pi++
		}
	}
	for ; di < len(doc.Fields); di++ {
		add(doc.Fields[di].String, doc.Values[di], true)
	}
	for ; pi < len(patch.Fields); pi++ {
		val, err := PatchWith(absentAt(doc, patch.Fields[pi].String, pi), patch.Values[pi], ctx)
		if err != nil {
			return nil, false, err
		}
		add(patch.Fields[pi].String, val, false)
	}

	var fields []*ir.Node
	if keysKept && len(keys) == len(doc.Fields) {
		// The document's own key nodes, shared. A key node is a descriptor -- its type,
		// its string, its position -- and every one of those is identical here, since the
		// keys and their order are the document's. What it does not carry over is Parent,
		// which still names the object it was built for: nothing reads a KEY's parent
		// except a diagnostic path, while a VALUE's is read by encoding and is set below.
		fields = make([]*ir.Node, len(doc.Fields))
		copy(fields, doc.Fields)
	} else {
		fields = make([]*ir.Node, len(keys))
		for i, key := range keys {
			fields[i] = &ir.Node{
				Parent: res, ParentIndex: i, ParentField: key,
				Type: ir.StringType, String: key,
			}
		}
	}

	// A value belongs to the object which now holds it.
	for i, v := range values {
		v.Parent, v.ParentIndex, v.ParentField = res, i, keys[i]
	}

	res.Fields, res.Values = fields, values
	patchTag := ir.StripPresentation(patch.Tag)
	if doc.Tag != "" {
		res.Tag = ir.TagCompose(doc.Tag, nil, patchTag)
	} else {
		res.Tag = patchTag
	}
	return res, true, nil
}

// sortedStringFields says whether an object's keys are plain strings in ascending
// order, which is what lets two of them be merged by walking. A store's documents are
// sorted by construction (FromMap sorts, and storage sorts on write and read); a
// document assembled some other way may not be, and is not this function's business.
func sortedStringFields(n *ir.Node) bool {
	for i := range n.Fields {
		f := n.Fields[i]
		if f.Type != ir.StringType {
			return false
		}
		if i > 0 && n.Fields[i-1].String >= f.String {
			return false
		}
	}
	return true
}

// absentAt is the document's absence at a place in it: the null a patch is applied
// to where the document has no node, standing at the field or index it would have
// had.
//
// It used to be a bare ir.Null(), fabricated with no parent -- a node belonging to
// no tree, and indistinguishable from a document root, since both answer nil to
// Parent. An operator which asks what document it is in got the placeholder and
// could not tell (p4tzbzx7h12kr6tkhxn0 is the same shape, one operator over).
//
// Nothing points DOWN at it -- it is not among doc's children and the document is
// unchanged -- so this adds a leaf which knows where it stands rather than a
// second tree. What the patch means here is still what it means on its own: the
// placeholder carries no value, only a place.
func absentAt(doc *ir.Node, field string, index int) *ir.Node {
	res := ir.Null()
	res.Parent, res.ParentField, res.ParentIndex = doc, field, index
	return res
}

func objPatchYWith(doc, patch *ir.Node, ctx *mergeop.OpContext) (*ir.Node, error) {
	if res, ok, err := objMergeFast(doc, patch, ctx); err != nil {
		return nil, err
	} else if ok {
		return res, nil
	}
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
		ppv, err := PatchWith(absentAt(doc, k, 0), pv, ctx)
		if err != nil {
			return nil, err
		}
		if ppv != nil {
			dstMap[k] = ppv
		}
	}
	if len(merges) == 0 {
		res := ir.FromMap(dstMap)
		res.Tag = mergedTag(doc, patch)
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
	res.Tag = mergedTag(doc, patch)
	return res, nil
}

// mergedTag answers the tag a merged CONTAINER wears.
//
// Presentation is the document's: a patch is a statement about content, and the
// brackets it happens to be written with describe the patch, not an instruction to
// restyle what it lands on -- the same reading that makes a patch unable to reorder
// a document's fields. Everything else the patch says composes on top.
//
// The object path always did this; the array path built a fresh ir.FromSlice and wore
// nothing, so replacing an array dropped BOTH sides' presentation and a flow array
// came back written in dashes. Invisible while whole-array replaces were rare, and
// routine once lowering turned relative writes into stated values.
func mergedTag(doc, patch *ir.Node) string {
	patchTag := ir.StripPresentation(patch.Tag)
	if doc.Tag == "" {
		return patchTag
	}
	return ir.TagCompose(doc.Tag, nil, patchTag)
}
