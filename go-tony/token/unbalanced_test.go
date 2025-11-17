package token

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/format"
)

var notOKDocs = []string{
	`{`,
	`[`,
	`}`,
	`]`,
	`{[]}`,
	`{[}]`,
	`[{]}`,
	`[
  a: {
    b: c
  }
  c: !tag 1
}`,
	`
a: 1
  b:
    - 3
   - 4
c: !tag 2`,
	`- 2 - 3`,
	`
- 1
- 2 - 3`,
	`
a
	: 2`,
	`
- - 2
   - 3`,
	"  ",
	"a\n  ",
	`[1,2,3,[,]]`,
	`[
  a: {
    b: c
  }
  c: !tag 1
]`,
	`
- - - f:
        g: 1
        h:
        - - f: 4
        - 1
     g: 4
    - z: 22
  - a: 1
- 22`,
}

func TestUnbalanced(t *testing.T) {
	for i, doc := range notOKDocs {
		toks, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Error(err)
			return
		}
		balanced, err := Balance(toks, format.TonyFormat)
		if err == nil {
			t.Errorf("%d got balanced for %q", i, doc)
			PrintTokens(toks, "weird")
			return
		}
		t.Logf("imbalanced %q error %s", doc, err)
		_ = balanced
	}
}
