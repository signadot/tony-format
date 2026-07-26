package aliasnode

import (
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/aliasnodesrc"
	"github.com/signadot/tony-format/go-tony/ir"
)

//tony:schemagen=aliasnode-direct,notag
type Direct struct {
	P *ir.Node `tony:"field=p"`
}

// Local aliases *ir.Node in the same package.
type Local = *ir.Node

//tony:schemagen=aliasnode-local,notag
type LocalAlias struct {
	P Local `tony:"field=p"`
}

//tony:schemagen=aliasnode-cross,notag
type CrossAlias struct {
	P aliasnodesrc.Payload `tony:"field=p"`
}
