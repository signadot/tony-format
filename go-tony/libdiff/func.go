package libdiff

import "github.com/signadot/tony-format/go-tony/ir"

type DiffFunc func(*ir.Node, *ir.Node) *ir.Node
