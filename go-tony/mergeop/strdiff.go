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
	return op.retag(doc, res, ctx, pf)
}

// retag restores the tag of the patched string.  A string patch builds a bare
// string, so the tag has to be put back: a strdiff describes the characters of
// a string, never its tag, so the tag is the document's own unless the diff
// composed a tag diff -- !addtag, !rmtag, !retag -- after the !strdiff.
//
// Any other residual tag belongs to the diff's own int-keyed child, not to the
// result: !sparsearray and !bracket land there from parsing, and older diffs
// restated the unchanged tag there too, which the document already carries.
func (op strDiffOp) retag(doc, res *ir.Node, ctx *OpContext, pf PatchFunc) (*ir.Node, error) {
	res = res.WithTag(doc.Tag)
	for tag := op.child.Tag; tag != ""; {
		head, _, rest := ir.TagArgs(tag)
		if isTagDiff(head) {
			return pf(res, ir.Null().WithTag(tag), ctx)
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
