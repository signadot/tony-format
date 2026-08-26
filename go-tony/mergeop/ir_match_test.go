package mergeop_test

import (
	"strings"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
)

// What !ir answers, asked through a match rather than through the object it used
// to build to answer with.
func TestIRMatches(t *testing.T) {
	for _, tc := range []struct {
		name, doc, pattern string
		want               bool
	}{{
		name: "an integer by its int field", doc: `3`,
		pattern: `!ir {type: Number, int: 3}`, want: true,
	}, {
		// the distinction no pattern over the value can make: both are Number
		name: "a float is not an integer", doc: `3.5`,
		pattern: `!ir {int: null}`, want: false,
	}, {
		name: "a float by its float field", doc: `3.5`,
		pattern: `!ir {type: Number, float: 3.5}`, want: true,
	}, {
		name: "an integer has no float field", doc: `3`,
		pattern: `!ir {float: null}`, want: false,
	}, {
		name: "a string", doc: `"x"`,
		pattern: `!ir {type: String, string: "x"}`, want: true,
	}, {
		name: "a tag", doc: `!k v`,
		pattern: `!ir {type: String, tag: "!k", string: v}`, want: true,
	}, {
		name: "an untagged node has no tag field", doc: `v`,
		pattern: `!ir {tag: null}`, want: false,
	}, {
		// a null pattern matches anything PRESENT, so naming the field is the question
		name: "a null asks only that the field is there", doc: `3`,
		pattern: `!ir {int: null}`, want: true,
	}, {
		name: "every field the pattern names has to hold", doc: `3`,
		pattern: `!ir {type: Number, int: 4}`, want: false,
	}, {
		name: "an object by its keys", doc: `{a: 1}`,
		pattern: `!ir {type: Object, fields: [a], values: [1]}`, want: true,
	}, {
		// the shape base.tony's sparsearray is written in
		name: "an op under fields reaches the keys as nodes", doc: `{a: 1, b: 2}`,
		pattern: `!ir {fields: !all.irtype ""}`, want: true,
	}, {
		name: "and can fail there", doc: `{a: 1}`,
		pattern: `!ir {fields: !all.irtype 1}`, want: false,
	}, {
		name: "an op under values", doc: `{a: 1, b: 2}`,
		pattern: `!ir {values: !all.irtype 1}`, want: true,
	}, {
		// the children are the document's nodes, one level deep, so !ir asks again
		name: "ir applies again at depth", doc: `{a: 3}`,
		pattern: `!ir {values: [!ir {int: 3}]}`, want: true,
	}, {
		name: "a document which merely looks like an IR encoding", doc: `{type: Number, int: 3}`,
		pattern: `!ir {type: Object}`, want: true,
	}, {
		name: "and is not matched by !ir as the node it describes", doc: `{type: Number, int: 3}`,
		pattern: `!ir {type: Number}`, want: false,
	}, {
		name: "composed", doc: `3`,
		pattern: `!not.ir {float: null}`, want: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(mustParseNode(t, tc.doc), mustParseNode(t, tc.pattern))
			if err != nil {
				t.Fatalf("match: %v", err)
			}
			if got != tc.want {
				t.Errorf("matched=%v, want %v", got, tc.want)
			}
		})
	}
}

// A pattern which cannot be read is refused where it is built, which is what !at
// says about a path it cannot parse: reporting it as a mismatch at every node a
// match visits tells nobody anything.
//
// The misspelling is the case worth having. `!ir {itn: 3}` named no field of an
// IR node, so it silently declined to match every document there is -- the shape
// of wrongness nobody finds.
func TestIRRefusesAPatternItCannotRead(t *testing.T) {
	for _, tc := range []struct {
		name, pattern, want string
	}{{
		name:    "a misspelt field",
		pattern: `!ir {itn: 3}`,
		want:    `"itn" is not a field of an IR node`,
	}, {
		name:    "an operand which is not an object",
		pattern: `!ir 3`,
		want:    "expects an object over the fields of an IR node, got Number",
	}, {
		name:    "an operand which is null",
		pattern: `!ir null`,
		want:    "expects an object over the fields of an IR node, got Null",
	}, {
		name:    "a key which is not a name",
		pattern: `!ir {<<: 3}`,
		want:    "a key names a field of an IR node",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tony.Match(mustParseNode(t, `3`), mustParseNode(t, tc.pattern))
			if err == nil {
				t.Fatalf("no error, matched=%v; want an error saying %q", got, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not say %q", err, tc.want)
			}
		})
	}
}
