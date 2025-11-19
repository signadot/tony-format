package debug

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

type JSON any
type Tony struct{ *ir.Node }

func (y Tony) String() string {
	x := y.Node
	buf := bytes.NewBuffer(nil)
	if err := encode.Encode(x, buf); err != nil {
		return fmt.Sprintf("[raw *y.Y] %v", x)
	}
	return buf.String()
}

func Logf(msg string, args ...any) {
	for i := range args {
		a := args[i]
		switch x := a.(type) {
		case map[string]any, []any, json.Number:
			d, err := json.MarshalIndent(a, "   |", "  ")
			if err != nil {
				args[i] = fmt.Sprintf("%v", a)
				continue
			}
			args[i] = string(d)
		case *ir.Node:
			buf := bytes.NewBuffer(nil)
			if err := encode.Encode(x, buf); err != nil {
				args[i] = fmt.Sprintf("[raw *y.Y] %v", x)
				continue
			}
			args[i] = buf.String()
		case bool, string, float64, int:

		default:
		}
	}
	fmt.Fprintf(os.Stderr, msg, args...)
}
