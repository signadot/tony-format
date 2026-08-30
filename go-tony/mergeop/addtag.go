package mergeop

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var addTagSym = &addTagSymbol{patchName: addTagName}

func AddTag() Symbol {
	return addTagSym
}

const (
	addTagName patchName = "addtag"
)

type addTagSymbol struct {
	patchName
}

func (s addTagSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 args, got %d", s, len(args))
	}
	return &addTagOp{tag: args[0], patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type addTagOp struct {
	patchOp
	tag string
}

func (p addTagOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("addtag op patch on %s\n", doc.Path())
	}
	res, err := patchUnderTagDiff(doc, p.child, ctx, pf)
	if err != nil || res == nil {
		return nil, err
	}
	return res.WithTag("!" + p.tag), nil
}

// patchUnderTagDiff applies whatever a tag diff decorates.  A diff says a tag
// changed by composing !addtag, !rmtag or !retag over the diff of the value,
// which is a null when only the tag changed and the value's own diff when both
// did -- and dropping that would silently discard every change beneath it.
//
// It answers with no node when the diff beneath it deleted the value, which its
// three callers pass on: there is no node left to state a tag of, and writing
// one onto the nil was a crash apiece.
//
// A null says the value did not change, and the test used to require it to carry
// no tag at all. A tag chain has room for more after the operation, and SplitChild
// hands everything after the first registered op to the child -- so
// `!addtag(bracket).logd-patch-root null` arrived here as a TAGGED null, missed
// this branch, and patched the document with it. Against a whole document that is
// total loss, and it was reachable: a no-op !rename at a document root lowers to
// exactly that shape, and logd marks a delta's root with its own label
// (1hf5pzj6h12ksd40jdn0).
//
// What decides is whether an OPERATION trails the tag op, not whether anything
// does. A trailing operation is a real composition and has to run -- `!retag(a,b).insert null`
// inserts a null and means it, and `!addtag(x).delete null` deletes -- while a
// trailing label that names no operation is not the value speaking at all. A diff
// never leaves one there: it composes the tag op LAST, after the value's own tags
// (see mkTagDiff's caller in libdiff/object.go), and a value that BECOMES null is
// emitted as !replace rather than as a tag op over a null.
func patchUnderTagDiff(doc, child *ir.Node, ctx *OpContext, pf PatchFunc) (*ir.Node, error) {
	if child.Type == ir.NullType && !chainHasOp(child.Tag) {
		return doc.Clone(), nil
	}
	return pf(doc, child, ctx)
}

// chainHasOp reports whether a tag chain names a registered operation anywhere in it.
// It is SplitChild's own walk asked as a question: everything that is not an operation
// is a label the value carries, and a chain of only those states nothing to apply.
func chainHasOp(tag string) bool {
	for tag != "" {
		head, _, rest := ir.TagArgs(tag)
		if head == "" {
			return false
		}
		if Lookup(head[1:]) != nil {
			return true
		}
		if rest == tag {
			return false
		}
		tag = rest
	}
	return false
}
