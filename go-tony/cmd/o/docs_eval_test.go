package main

import (
	"os"
	"regexp"
	"testing"
)

var tonyBlock = regexp.MustCompile("(?s)```tony\n(.*?)```")

// TestEvalDocExample: the example on the eval page did not run. Its list under
// `!eval` was indented beneath the key, which the format refuses -- in block mode
// the "- " prefix IS the indentation -- and its block scalar carried a line
// comment, which belongs to the block. The command it showed, `o -e x=7 -c`, is
// not a command either: eval is a subcommand, and it has no -c.
//
// None of that is visible by reading, and all of it is what a reader types
// first. A documented example is a claim about the binary, so it is run against
// one: the page's first tony block, through the command the page names, must
// produce the page's second block.
func TestEvalDocExample(t *testing.T) {
	const docsPath = "../../../docs/eval.md"
	src, err := os.ReadFile(docsPath)
	if err != nil {
		t.Skipf("no %s to check against: %v", docsPath, err)
	}
	blocks := tonyBlock.FindAllStringSubmatch(string(src), -1)
	if len(blocks) < 2 {
		t.Fatalf("%s has %d tony blocks, want an input and its output", docsPath, len(blocks))
	}
	in, want := blocks[0][1], blocks[1][1]

	dir := t.TempDir()
	path := writeDoc(t, dir, "eval.tony", in)

	code, out := runO(t, "eval", "-e", "x=7", path)
	if code != 0 {
		t.Fatalf("`o eval -e x=7` on the documented example exited %d:\n%s", code, out)
	}
	if out != want {
		t.Errorf("the page says the output is\n%s\nand it is\n%s", want, out)
	}
}
