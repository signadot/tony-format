package encode

import (
	"bytes"
	"strings"

	"github.com/signadot/tony-format/tony/ir"
)

func MustString(y *ir.Node) string {
	buf := bytes.NewBuffer(nil)
	if err := Encode(y, buf); err != nil {
		panic(err)
	}
	return strings.TrimSpace(buf.String())
}
