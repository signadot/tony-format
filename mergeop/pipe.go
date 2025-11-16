package mergeop

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/tony-format/tony/debug"
	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/libdiff"
	"github.com/tony-format/tony/parse"
)

var pipeSym = &pipeSymbol{patchName: pipeName}

func Pipe() Symbol {
	return pipeSym
}

const (
	pipeName patchName = "pipe"
)

type pipeSymbol struct {
	patchName
}

func (s pipeSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op has no args, got %v", s, args)
	}
	return &pipeOp{patchOp: patchOp{op: op{name: s.patchName, child: child}}}, nil
}

type pipeOp struct {
	patchOp
}

func (n pipeOp) Patch(doc *ir.Node, mf MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("patch op pipe on %s\n", doc.Path())
	}
	fields := strings.Fields(n.child.String)
	if len(fields) == 0 {
		return nil, fmt.Errorf("no command to pipe to at %s", n.child.Path())
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	buf := bytes.NewBuffer(nil)
	if doc.Type == ir.StringType {
		buf.WriteString(doc.String)
	} else {
		if err := encode.Encode(doc, buf); err != nil {
			return nil, err
		}
	}
	cmd.Stdin = buf
	oBuf := bytes.NewBuffer(nil)
	cmd.Stdout = oBuf
	eBuf := bytes.NewBuffer(nil)
	cmd.Stderr = eBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("error running %s: %w (%q)", cmd, err, eBuf.String())
	}
	if doc.Type != ir.StringType {
		return parse.Parse(oBuf.Bytes())
	}
	return ir.FromString(oBuf.String()), nil
}
