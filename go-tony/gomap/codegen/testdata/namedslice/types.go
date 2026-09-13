package namedslice

import (
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Names is a named slice with a hand-written codec: the slice form of
// f69agjyeh12ks item 2 (p478tacqh12krg32msn0 item 25). A field of type Names
// dispatches to these methods rather than inlining the slice.
//
//tony:schemagen=namedslice-names,notag,codec=custom
type Names []string

func (n Names) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("NAMES"), nil
}

func (n *Names) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error {
	*n = Names{"decoded", node.String}
	return nil
}

//tony:schemagen=namedslice-host,notag
type Host struct {
	N Names `tony:"field=n"`
}
