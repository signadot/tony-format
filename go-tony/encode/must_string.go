package encode

import (
	"bytes"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

// MustString encodes node as Tony with default options and answers the text with
// surrounding whitespace trimmed. It panics if encoding fails.
func MustString(node *ir.Node) string {
	buf := bytes.NewBuffer(nil)
	if err := Encode(node, buf); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}
