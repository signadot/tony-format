package token

import "testing"

var mLits = []string{
	`|
  a
  b
`,
	`|-
  a
  b
`,
	`|+
  a

  b


`,
}

func TestMLit(t *testing.T) {
	for _, m := range mLits {
		p := &PosDoc{d: []byte(m)}
		i, err := mLit([]byte(m), 2, p, 0)
		if err != nil {
			t.Errorf("\n%s\n: %v %s", m, err, p.Pos(0))
		}
		t.Logf("mlit bytes: `%s`", m[0:i])
	}
}
