package encode

import (
	"bytes"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
)

func MustString(node *ir.Node) string {
	buf := bytes.NewBuffer(nil)
	if err := Encode(node, buf); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}
