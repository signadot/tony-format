package main

import (
	"io"
	"strings"
	"testing"

	"github.com/scott-cotton/cli"
)

// runOBoth is runOIn with the ERROR stream kept. A usage error is written there, and the
// shared harness returns stdout alone, so a test that wants to read the complaint has to
// capture it itself.
func runOBoth(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	outBuf, errBuf := &strings.Builder{}, &strings.Builder{}
	cc := &cli.Context{
		Out: nopWC{outBuf},
		Err: nopWC{errBuf},
		In:  io.NopCloser(strings.NewReader("")),
	}
	cmd := MainCommand()
	err := cmd.Run(cc, args)
	return cmd.Exit(cc, err), outBuf.String(), errBuf.String()
}

// -t, -j and -y name what a document IS, and a document is one thing. Two of them is not
// a preference to resolve but a question the caller has to answer, and resolving it
// silently is what happened: the switch in parseOpts and encOpts tries tony first, so
// `-j -t` read json as tony and said nothing about it.
//
// The root refused them together already. Every command accepts the root's options in its
// own position too, and there the check was not made -- `o -j -t v f` was refused while
// `o v -j -t f` was not, which is the same mistake spelled differently. Both positions are
// checked here for that reason, and each command is asked in its own right, since the
// check has to be made per command rather than once.
func TestOneFormatOnly(t *testing.T) {
	dir := t.TempDir()
	doc := writeDoc(t, dir, "d.tony", "a: 1\n")

	for _, args := range [][]string{
		{"-j", "-t", "v", doc}, // the root's position
		{"v", "-j", "-t", doc}, // the command's own
		{"v", "-t", "-y", doc},
		{"v", "-j", "-y", doc},
		{"get", "-j", "-t", "a", doc},
		{"list", "-t", "-y", "a", doc},
		{"patch", "-j", "-t", "{a: 2}", doc},
		{"diff", "-t", "-j", doc, doc},
		{"dump", "-j", "-y", doc},
	} {
		t.Run(strings.Join(args[:len(args)-1], " "), func(t *testing.T) {
			code, out, errOut := runOBoth(t, args...)
			if code == 0 {
				t.Errorf("exited 0; two formats were accepted\n%s", out)
			}
			if !strings.Contains(errOut, "at most one of") {
				t.Errorf("said nothing about the conflict:\n%s", errOut)
			}
		})
	}

	// And one of them alone is still just itself.
	for _, f := range []string{"-t", "-y"} {
		t.Run("alone "+f, func(t *testing.T) {
			if code, _, errOut := runOBoth(t, "v", f, doc); code != 0 {
				t.Errorf("o v %s exited %d\n%s", f, code, errOut)
			}
		})
	}
}
