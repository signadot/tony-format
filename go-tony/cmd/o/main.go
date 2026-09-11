// Command o is a tool for working with object notation: it reads and writes Tony,
// YAML and JSON, and renders, queries, diffs, matches, patches, evaluates, builds
// and validates documents in any of them. "o system" runs the logd and docd
// servers and talks to them.
//
// Run "o -h" for the command list and "o help <command>" for what a command does
// and the options it takes; "o docs <dir>" writes the same help as markdown, one
// page per command.
package main

import (
	"context"

	"github.com/scott-cotton/cli"
	_ "github.com/signadot/tony-format/go-tony/eval"
	_ "github.com/signadot/tony-format/go-tony/mergeop"
)

func main() {
	cli.MainContext(context.Background(), MainCommand())
}
