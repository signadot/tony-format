package libdiff

import "github.com/signadot/tony-format/tony/ir"

type DiffFunc func(*ir.Node, *ir.Node) *ir.Node
