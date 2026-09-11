package libdiff

import "github.com/signadot/tony-format/go-tony/ir"

// DiffFunc diffs two nodes, from and then to, answering nil when they are equal. The
// container diffs call one for each pair of children they match.
type DiffFunc func(*ir.Node, *ir.Node) *ir.Node
