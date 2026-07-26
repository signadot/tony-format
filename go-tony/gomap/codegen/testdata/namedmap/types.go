package namedmap

import (
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Match is a named map with a hand-written codec (issue f69agjyeh12ks item 2).
// A field of type Match must dispatch to these methods, not inline the map.
//
//tony:schemagen=namedmap-match,notag,codec=custom
type Match map[string]any

// LastToOpts and LastFromOpts record how many options the hand-written codec
// was handed, so a test can tell whether the generated caller forwarded them
// (issue f69agjyeh12ks item 5). A nested codec that cannot see the encode
// options cannot know the target format, which is what item 5 was about.
var (
	LastToOpts   int
	LastFromOpts int
)

func (m Match) ToTonyIR(opts ...gomap.MapOption) (*ir.Node, error) {
	LastToOpts = len(opts)
	return ir.FromString("MATCH"), nil
}

func (m *Match) FromTonyIR(node *ir.Node, opts ...gomap.UnmapOption) error {
	LastFromOpts = len(opts)
	*m = Match{"decoded": node.String}
	return nil
}

//tony:schemagen=namedmap-host,notag
type Host struct {
	M Match `tony:"field=m"`
}
