package mergeop

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var strDiffSym = &strDiffSymbol{patchName: strDiffName}

func StrDiff() Symbol {
	return strDiffSym
}

const (
	strDiffName patchName = "strdiff"
)

type strDiffSymbol struct {
	patchName
}

func (s strDiffSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects 1 arg, got %v", s, args)
	}
	multiLine, err := strconv.ParseBool(args[0])
	if err != nil {
		return nil, fmt.Errorf("strdiff requires a boolean (multiline) argument: %w", err)
	}

	if child.Type != ir.ObjectType {
		return nil, errors.New("strdiff op needs an object")
	}
	return &strDiffOp{
		multiLine: multiLine,
		patchOp:   patchOp{op: op{name: s.patchName, child: child}},
	}, nil
}

type strDiffOp struct {
	patchOp
	multiLine bool
}

func (op strDiffOp) Patch(doc *ir.Node, ctx *OpContext, mf MatchFunc, pf PatchFunc, df libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch op strdiff on %s\n", doc.Path())
	}

	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("strdiff only applies to strings, got %s", doc.Type)
	}
	var (
		res *ir.Node
		err error
	)
	if op.multiLine {
		res, err = libdiff.PatchStringMultiLine(doc, op.child)
	} else {
		res, err = libdiff.PatchStringRunes(doc, op.child)
	}
	if err != nil {
		return nil, err
	}
	return retagFromDiff(doc, res, op.child, ctx, pf)
}

// retagFromDiff restores the tag of a patched value.  A strdiff describes the
// characters of a string and an arraydiff the elements of an array; neither
// describes the tag, and both build a fresh node, so the tag has to be put
// back.  It is the document's own unless the diff composed a tag diff --
// !addtag, !rmtag, !retag -- after the operation, which is the only case a
// diff needs to say anything about.
//
// Any other residual tag belongs to the diff's own int-keyed child rather than
// to the result: !sparsearray and !bracket land there from parsing, and older
// diffs restated the unchanged tag there too, which the document already
// carries.
func retagFromDiff(doc, res, child *ir.Node, ctx *OpContext, pf PatchFunc) (*ir.Node, error) {
	res = res.WithTag(doc.Tag)
	for tag := child.Tag; tag != ""; {
		head, args, rest := ir.TagArgs(tag)
		if isTagDiff(head) {
			// just this one, not whatever the child's own tag composed after it
			return pf(res, ir.Null().WithTag(ir.TagCompose(head, args, "")), ctx)
		}
		tag = rest
	}
	return res, nil
}

func isTagDiff(head string) bool {
	switch head {
	case libdiff.TagInsertTag, libdiff.TagDeleteTag, libdiff.TagReplaceTag:
		return true
	}
	return false
}
