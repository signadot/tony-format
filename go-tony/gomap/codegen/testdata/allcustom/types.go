package allcustom

import (
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
)

//tony:schemagen=allcustom-thing,notag,codec=custom
type Thing struct {
	V string `tony:"field=v"`
}

func (t *Thing) ToTonyIR(o ...gomap.MapOption) (*ir.Node, error)     { return ir.FromString(t.V), nil }
func (t *Thing) FromTonyIR(n *ir.Node, o ...gomap.UnmapOption) error { t.V = n.String; return nil }
