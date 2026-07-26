package aliasnodesrc

import "github.com/signadot/tony-format/go-tony/ir"

// Payload aliases *ir.Node across a package boundary (issue f69agjyeh12ks item 18).
type Payload = *ir.Node
