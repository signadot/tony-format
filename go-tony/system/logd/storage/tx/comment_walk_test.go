package tx

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// commentedPatcherData parses src with comments and wraps it the way a patcher
// hands a patch to the injectors.
func commentedPatcherData(t *testing.T, src string) (*ir.Node, []*PatcherData) {
	t.Helper()
	node, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return node, []*PatcherData{{
		API: &api.Patch{PathData: api.PathData{Path: "", Data: node}},
	}}
}

// TestKeyedLoweringThroughComments: a head comment wraps the value it precedes, and a
// walk that switches on node.Type stops at the wrapper -- so a commented keyed array
// would keep its positional form, and its elements would merge by POSITION instead of by
// identity. The comment must not change how two writes combine (3cdjz00jh12krns4g1n0).
func TestKeyedLoweringThroughComments(t *testing.T) {
	schema := &api.Schema{KeyFields: []api.KeyField{{Path: "users", Field: "id"}}}
	for _, tc := range []struct{ name, src string }{
		{"no comment", "users:\n- id: a\n"},
		{"comment above the document", "# note\nusers:\n- id: a\n"},
		{"comment above the array", "users:\n# note\n- id: a\n"},
		{"comment above the element", "users:\n# note\n- id: a\n# other\n- id: b\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, data := commentedPatcherData(t, tc.src)
			if err := LowerKeyed(schema, data); err != nil {
				t.Fatal(err)
			}
			users, err := node.GetKPath("users")
			if err != nil || users == nil {
				t.Fatalf("no users in %v: %v", node, err)
			}
			if users.Type != ir.ObjectType {
				t.Fatalf("users is %s after lowering; a keyed array is stored as an object of names", users.Type)
			}
			if ir.Get(users, "(id=a)") == nil {
				t.Errorf("no element named (id=a): fields are %v", fieldNames(users))
			}
		})
	}
}

func fieldNames(n *ir.Node) []string {
	out := make([]string, 0, len(n.Fields))
	for _, f := range n.Fields {
		out = append(out, f.String)
	}
	return out
}

// TestAutoIDsThroughComments: the same wrapper stood between injectAutoIDsRec and
// the array, and between the array and a commented element, so an id the schema
// asks for was not generated.
func TestAutoIDsThroughComments(t *testing.T) {
	schema := &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "users", Field: "id"}}}
	for _, tc := range []struct{ name, src string }{
		{"no comment", "users:\n- name: Alice\n"},
		{"comment above the document", "# note\nusers:\n- name: Alice\n"},
		{"comment above the array", "users:\n# note\n- name: Alice\n"},
		{"comment above the element", "users:\n- name: Alice\n# note\n- name: Bo\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, data := commentedPatcherData(t, tc.src)
			elems, err := node.ListKPath(nil, "users[*]")
			if err != nil {
				t.Fatal(err)
			}
			if len(elems) == 0 {
				t.Fatalf("the walk found no elements to key, so this proves nothing: %q", tc.src)
			}
			if got := InjectAutoIDs(1, schema, data); got != len(elems) {
				t.Fatalf("generated %d ids for %d elements", got, len(elems))
			}
			// ListKPath answers with clones, so the ids are read back from the tree
			// after the injection rather than from the slice taken before it.
			elems, err = node.ListKPath(nil, "users[*]")
			if err != nil {
				t.Fatal(err)
			}
			for i, elem := range elems {
				id := ir.Get(elem, "id")
				if id == nil || id.String == "" {
					t.Errorf("element %d has no id: %v", i, elem)
				}
			}
		})
	}
}
