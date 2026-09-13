package parse

import (
	"strings"
	"testing"
)

// A key names one field. A map writing the same key twice was accepted, and every
// consumer then picked its own field: ir.Get the first, ToMap the last, a match
// neither, a patch the first and then the raw second (p478tacqh12krg32msn0 item 3).
// It is refused where it is written, in every format, naming the key. A merge key is
// not a name, and a map may carry several.
func TestDuplicateKeyIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		opts      []ParseOption
		key       string
	}{
		{"block", "a: 1\na: 2\n", nil, `"a"`},
		{"flow", "{a: 1, b: 2, a: 3}", nil, `"a"`},
		{"quoted and bare", "{\"a\": 1, a: 2}", nil, `"a"`},
		{"int keys", "{1: x, 1: y}", nil, "1"},
		{"nested", "outer: {a: 1, a: 2}", nil, `"a"`},
		{"json", `{"a": 1, "a": 2}`, []ParseOption{ParseJSON()}, `"a"`},
		{"yaml", "a: 1\na: 2\n", []ParseOption{ParseYAML()}, `"a"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), tc.opts...)
			if err == nil {
				t.Fatalf("%q parsed", tc.src)
			}
			if !strings.Contains(err.Error(), "duplicate key "+tc.key) {
				t.Errorf("%q: error %q does not name the duplicate key %s", tc.src, err, tc.key)
			}
		})
	}

	// Not duplicates: the same name in two objects, a key and its element index, two
	// merge keys.
	for _, src := range []string{
		"a: {x: 1}\nb: {x: 2}\n",
		"a: 1\n<<: x\n<<: y\nb: 2\n",
		"[{a: 1}, {a: 2}]",
	} {
		if _, err := Parse([]byte(src)); err != nil {
			t.Errorf("%q refused: %v", src, err)
		}
	}
}
