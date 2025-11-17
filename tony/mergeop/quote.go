package mergeop

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
	"github.com/signadot/tony-format/tony/parse"
)

var quoteSym = &quoteSymbol{patchName: quoteName}

func Quote() Symbol {
	return quoteSym
}

const (
	quoteName patchName = "quote"
)

type quoteSymbol struct {
	patchName
}

func (s quoteSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	return &quoteOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type quoteOp struct {
	patchOp
}

func (p quoteOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("quote op patch on %s\n", doc.Path())
	}
	q, err := QuoteY(doc)
	if err != nil {
		return nil, err
	}
	return q, nil
}

func QuoteY(node *ir.Node) (*ir.Node, error) {
	if node.Type == ir.StringType {
		return node, nil
	}
	buf := bytes.NewBuffer(nil)
	if err := encode.Encode(node, buf); err != nil {
		return nil, err
	}
	return parse.Parse([]byte(strconv.Quote(buf.String())))
}
