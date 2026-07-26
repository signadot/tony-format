package namedmap

import (
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Match is a named map with a hand-written codec (issue f69agjyeh12ks item 2).
// A field of type Match must dispatch to these methods, not inline the map.
//tony:schemagen=namedmap-match,notag,codec=custom
type Match map[string]any

func (m Match) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	return ir.FromString("MATCH"), nil
}

func (m *Match) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error {
	*m = Match{"decoded": node.String}
	return nil
}

//tony:schemagen=namedmap-host,notag
type Host struct {
	M Match `tony:"field=m"`
}
