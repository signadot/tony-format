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

// TestKeyTagsThroughComments: a head comment wraps the value it precedes, and
// injectKeyTagsRec switched on node.Type -- so a commented array was never given
// the !key tag its schema declares, and its elements merged by POSITION instead
// of by identity. The comment changed how two writes combined
// (3cdjz00jh12krns4g1n0).
func TestKeyTagsThroughComments(t *testing.T) {
	schema := &api.Schema{KeyFields: []api.KeyField{{Path: "users", Field: "id"}}}
	for _, tc := range []struct{ name, src string }{
		{"no comment", "users:\n- id: a\n"},
		{"comment above the document", "# note\nusers:\n- id: a\n"},
		{"comment above the array", "users:\n# note\n- id: a\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, data := commentedPatcherData(t, tc.src)
			if err := InjectKeyTags(schema, data); err != nil {
				t.Fatal(err)
			}
			users, err := node.GetKPath("users")
			if err != nil || users == nil {
				t.Fatalf("no users array in %v: %v", node, err)
			}
			field, keyed := users.KeyField()
			if !keyed || field != "id" {
				t.Errorf("the array is keyed by %q (keyed=%v); the schema declares id", field, keyed)
			}
		})
	}
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
