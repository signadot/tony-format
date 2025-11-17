package tony

import (
	"os"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func TestNode(t *testing.T) {
	doc := `
f1: 31.2
f2:
- a
  # pre 1
- 1
  # post 1
- -2


  # pre f3
- f3: null # after f3
- !wrap
  pre: before
  value: it

  post: after

f4: true
f5: !key(name) false
f6: "six"
f7:
  f8: |
    hello
    multiline
    null
    string
f9:
- - - - x
      - y
- z
- 13
- - 2
`
	y, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Errorf("error decoding: %v", err)
		return
	}
	err = encode.Encode(y, os.Stdout)
	if err != nil {
		t.Errorf("error encoding: %v", err)
		return
	}
}
