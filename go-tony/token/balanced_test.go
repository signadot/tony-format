package token

import (
	"fmt"
	"testing"
	"github.com/signadot/tony-format/go-tony/format"
)

func TestBalanceYAMLImplicitNull(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "implicit null with sibling",
			input: "key:\nsibling: value",
		},
		{
			name:  "implicit null at EOF with newline",
			input: "key:\n",
		},
		{
			name:  "implicit null at EOF no newline",
			input: "key:",
		},
		{
			name:  "multiple implicit nulls",
			input: "a:\nb:\nc: value",
		},
		{
			name:  "implicit null with nested sibling",
			input: "parent:\n  child: value\nsibling:",
		},
		{
			name:  "implicit null in nested object",
			input: "outer:\n  inner:\nouter2: value",
		},
		{
			name:  "implicit null with comment",
			input: "key: # comment\nsibling: value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toks, err := Tokenize(nil, []byte(tt.input))
			if err != nil {
				t.Fatalf("Tokenize error: %v", err)
			}
			balanced, err := Balance(toks, format.YAMLFormat)
			if err != nil {
				t.Fatalf("Balance error: %v", err)
			}
			// Verify balanced brackets
			n := 0
			hasNull := false
			for i := range balanced {
				tok := &balanced[i]
				switch tok.Type {
				case TLSquare, TLCurl:
					n++
				case TRSquare, TRCurl:
					n--
				case TNull:
					hasNull = true
				}
			}
			if n != 0 {
				t.Errorf("imbalanced: %d", n)
			}
			if !hasNull {
				t.Errorf("expected TNull token in output")
			}
		})
	}
}

func TestBalanceOK(t *testing.T) {
	for _, doc := range okDocs {
		toks, err := Tokenize(nil, []byte(doc))
		if err != nil {
			t.Error(err)
			continue
		}
		balanced, err := Balance(toks, format.TonyFormat)
		if err != nil {
			t.Errorf("# not balanced:\n%s\n---\n# %v", doc, err)
			return
		}
		t.Logf("balanced %q", doc)
		n := 0
		for i := range balanced {
			tok := &balanced[i]
			fmt.Printf("\t%s %q %s\n", tok.Type, string(tok.Bytes), tok.Pos.String())
			switch tok.Type {
			case TLSquare, TLCurl:
				n++
			case TRSquare, TRCurl:
				n--
			}
		}
		if n != 0 {
			t.Errorf("imbalanced: %d\n", n)
			return
		}
	}
}

var okDocs = []string{
	`a: 1`,
	`
a: 1`,
	`
a: 1
b: c`,
	`
a:
  b: c`,
	`
a:
  b: c
c: d`,
	`
a:
  b:
  - 3
  - 4
c: 2`,
	`
a:
  b:
  - 3
  - 4
c: !tag 2`,
	`
a:
  b:
    c:
    - 3
    - 4
d: !tag 2`,
	`
a:
  b:
    c:
    - 3
    - 4
  d: !tag 2`,
	`{}`,
	`[]`,
	`[{}]`,
	`{
		  a: {
		    b: c
		  },
		  c: !tag 1
		}`,
	`-42`,
	`{

			a: {
			 b: 9 }, # z

			c: {
			 d: 8 }
			}`,
	`{

			a: {
			 b: 9 } # z

			c: {
			 d: 8 },
			}`,
	`
- 1
`,
	`
- 1
- 2
`,
	`
- - 1
`,
	`
- - 1
- - 2
`,
	`
- - 1
  - 2
`,
	`
- 1
- - 2
  - - 3
`,
	`[1,2,3,[]]`,
	`[1,2,3,[]] #z`,
	`
- 1
# h
- 2 # z
`,
	`{
		  a: {
		  # z
		    b: c
		  }, # zz
		  # zzz
		  c: !tag 1
# zzzz
		}`,
	`
- !tag
  a: 1
  b: 2
- 4`,
	`
f:
- !tag
  a: 1
  b: [1,2 3,]
`,
	`
f:
  g:
  - !tag
    a: 1

    b: [1,2,3]
  - 2
`,
	`
- - - - x
      - y
- z
- 13
- - 2`,
	`
f1:
- - - - x
      - y
- z
- 13
- - 2`,
	`
f1:
  f2: |
    m
    l
    ine
    s

f2:
- - a
  - b
- c`,
	`
f1: !x
  f2: 0`,
	`[a]`,
	`[a,b]`,
	`[[]]`,
	`[{}]`,
	`[a,[b]]`,
	`
f1:
- a: b
  c: d

- # it is
  a: bunny
  b: c
- 0
 `,

	`
x: !key(key)
- key: 2
  value: 33`,
	`

<<: |
  # hello multi lines
  # commented list
  - 1
  - false`,
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
	"!key(a)\n- a: b\n  b: !glob 'c*'",
	`{
spec: {
containers: [{
command: [
  "/app/io-context-server",
  "-tls=secretns=signadot",
  "-port=8443",
],
}]}}`,
}
