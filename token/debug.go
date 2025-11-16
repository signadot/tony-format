package token

import "fmt"

func PrintTokens(toks []Token, msg string) {
	fmt.Printf("%s tokens:\n", msg)
	for i := range toks {
		t := &toks[i]
		fmt.Printf("\t%s `%s` %s\n", t.Type, t.Bytes, t.Pos)
	}
}
