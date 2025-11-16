package libdiff

import "github.com/tony-format/tony/ir"

type DiffFunc func(*ir.Node, *ir.Node) *ir.Node
