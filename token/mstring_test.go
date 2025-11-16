package token

import (
	"regexp"
	"testing"
)

func TestFoldedMString(t *testing.T) {
	fms := []string{
		`"abc"
"def"`,
		`'abc'
"def"`,
	}
	for _, fm := range fms {
		testFoldedMString(fm, t)
	}
}

func testFoldedMString(fms string, t *testing.T) {
	d := []byte(fms)
	posDoc := &PosDoc{}
	indent := 0
	for indent < len(d) {
		if d[indent] != ' ' {
			break
		}
		indent++
	}
	toks, off, err := mString(d[indent:], 0, indent, posDoc)
	if err != nil {
		t.Errorf("`%s` gave %v", fms, err)
		return
	}
	if off+indent != len(fms) {
		t.Errorf("`%s` gave offset %d not %d", fms, off, len(fms))
		return
	}
	if len(toks) == 0 {
		t.Errorf("`%s` no tokens", fms)
		return
	}
	back := toks[0].String()
	cmp := ""
	rx, err := regexp.Compile(`"|'`)
	if err != nil {
		t.Error(err)
		return
	}
	for i, qs := range rx.Split(fms, -1) {
		if i%2 == 0 {
			continue
		}
		cmp += qs
	}
	if cmp != back {
		t.Errorf("got `%s` want `%s`", back, cmp)
	}
}
