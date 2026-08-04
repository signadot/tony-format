package customcodec

import (
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Leaf supplies its own codec and is marked codec=custom (issue f69agjyeh12ks
// item 4): it stays resolvable — Host calls its methods — but codegen must not
// generate ToTonyIR/FromTonyIR for it (that would collide with these).
//
//tony:schemagen=customcodec-leaf,notag,codec=custom
type Leaf struct {
	V string `tony:"field=v"`
}

func (l *Leaf) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("LEAF:" + l.V), nil
}

func (l *Leaf) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error {
	l.V = "from:" + node.String
	return nil
}

//tony:schemagen=customcodec-host,notag
type Host struct {
	Leaf Leaf `tony:"field=leaf"`
}
