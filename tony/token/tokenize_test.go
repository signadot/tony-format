package token

import (
	"fmt"
	"testing"
)

func TestTokenize(t *testing.T) {
	s := `-1-2
- 1
- 2
# comment
# comment trailing
  # comment
  # comment trailing 
  "string"
  "nl\n"
  "\""
  "\\"
  !abc
  !de(f)
  null
  true
  false
  nully
  ''
  ""
  "\u1234"
  "\b\t\r\n\f"
  #"\'" bad escape
  #'\"'
  'z'
  '\''
  "\""
ȡ
{ ∞: 2, x: 0 }
{ ∞∞: -14 }
✓
  https://hello.world/1/2/3
  -3
  -0
  0.01
  0e21
  0E-2
0e+1
|
  a
  b
  c
  d
    e f  
  h
2
|-
  a
  b
  c
false

f: a

  # comment
  # comment
  b: c # after
  c:
  - 1
  - -0e24 # valid json number
|
  a
a,b

-423
--ad0dkd
|
  > 
   had
   dkdk
f[0]
<<: |
  a
spec: {
containers: [{
command: [
  "/app/io-context-server",
  "-tls=secretns=signadot",
  "-port=8443",
],
}]}
{a:b}
`
	toks, err := Tokenize(nil, []byte(s))
	if err != nil {
		t.Error(err)
		return
	}
	for i := range toks {
		fmt.Println(toks[i].Info())
		switch toks[i].Type {
		case TString, TMLit, TLiteral:
			fmt.Printf("`%s`\n---\n%q\n", string(toks[i].Bytes), toks[i].String())
		case TInteger:
			fmt.Printf("%s\n", string(toks[i].Bytes))
		}
	}
}

func TestYQTokenize(t *testing.T) {
	//	s := `
	//
	// ""
	// " "
	// "\""
	// "\n"
	// "a
	// b"
	//
	// "\
	// 1\
	// 2\
	// 3\""
	//
	// ”
	// '
	// adld
	//
	//	\n '
	//
	// ””
	s := `
!and
- a: !glob '*'
- c: d
`
	toks, err := Tokenize(nil, []byte(s), TokenYAML())
	if err != nil {
		t.Errorf("%s: %v", s, err)
		return
	}
	for i := range toks {
		fmt.Println(toks[i].Info())
		switch toks[i].Type {
		case TString, TMLit, TLiteral:
			fmt.Printf("`%s`\n---\n%q\n", string(toks[i].Bytes), toks[i].String())
		case TInteger:
			fmt.Printf("%s\n", string(toks[i].Bytes))
		}
	}
}
