package storage

import (
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// Resolution of the three-way tension in the plan's P1 (store deltas / index writes /
// rebuild from the log). A lowered delta is the WRITE EXPRESSED ABSOLUTELY, not the
// minimal difference: Diff(before, after), then union in an assertion at each path the
// patch named, valued from after.
//
// Still absolute, still self-describing, and it names what was written -- so it indexes
// like the schema route does today and a rebuild reproduces that from the log alone. The
// price is that a delta is no longer minimal: a coincident write stores a redundant
// assertion, and a delete stores the tombstone plus whatever else the write named. That
// cost is proportional to what the client wrote, not to history.
func lowerPreservingWrites(t *testing.T, before, after, clientPatch string, keys map[string]string) *ir.Node {
	t.Helper()
	delta := lower(t, before, after, keys)
	af := mustParseBody(t, after)
	annotateKeysAt(af, "", keys)

	patch := mustParseBody(t, clientPatch)
	for _, p := range writtenLeafPaths(patch, "", keys, nil) {
		v, err := af.GetKPath(p)
		if err != nil || v == nil {
			continue // the write removed it; the diff's tombstone carries that
		}
		// A keyed element cannot be rooted at items("A"): RootPatchAt builds a patch from
		// a kpath, and a key SEGMENT carries the key value while constructing the patch
		// needs the key FIELD. Root at the array and carry a one-element keyed list, which
		// is the same shape the stepping harness uses.
		root, val := p, v.Clone()
		if arr, key, ok := splitKeyedPath(p, keys); ok {
			list := ir.FromSlice([]*ir.Node{val})
			list.Tag = ir.TagCompose("!key", []string{key}, "")
			root, val = arr, list
		}
		rooted, err := tx.RootPatchAt(root, val)
		if err != nil {
			t.Logf("  RootPatchAt(%q) failed: %v", root, err)
			continue
		}
		if delta == nil {
			delta = rooted
			continue
		}
		if delta, err = tony.Patch(delta, rooted); err != nil {
			t.Fatalf("union %q: %v", p, err)
		}
	}
	return delta
}

// writtenLeafPaths names the paths a patch writes, using key segments for keyed arrays --
// the same shape indexPatchRec records.
func writtenLeafPaths(n *ir.Node, prefix string, keys map[string]string, dst []string) []string {
	if n == nil {
		return dst
	}
	if f, keyed := keys[prefix]; keyed && n.Type == ir.ArrayType {
		for _, elem := range n.Values {
			if k, ok := ir.ElemKey(elem, f); ok {
				dst = append(dst, prefix+"("+k+")")
			}
		}
		return dst
	}
	if n.Type != ir.ObjectType || len(n.Fields) == 0 {
		if prefix != "" {
			dst = append(dst, prefix)
		}
		return dst
	}
	for i, fld := range n.Fields {
		p := fld.String
		if prefix != "" {
			p = prefix + "." + fld.String
		}
		dst = writtenLeafPaths(n.Values[i], p, keys, dst)
	}
	return dst
}

func TestLowering_PreservingWrites(t *testing.T) {
	schema := &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "items", Field: "sku"}}}
	keys := map[string]string{"items": "sku"}

	for _, tc := range []struct{ name, before, after, client string }{
		{"element updated", `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 2}]}`, `{items: [{sku: "A", q: 2}]}`},
		{"element added", `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`, `{items: [{sku: "B", q: 2}]}`},
		{"coincident write", `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 1}]}`},
		{"element deleted", `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`, `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 1}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delta := lowerPreservingWrites(t, tc.before, tc.after, tc.client, keys)
			if delta == nil {
				t.Logf("  delta: <none>")
			} else {
				t.Logf("  delta: %s", encodeWire(t, delta))
			}
			var viaTag []string
			if delta != nil {
				viaTag = indexPathsOf(t, delta, nil)
			}
			viaSchema := indexPathsOf(t, mustParseBody(t, tc.client), schema)
			t.Logf("  tag:    %v", viaTag)
			t.Logf("  schema: %v", viaSchema)
			missing := []string{}
			for _, p := range viaSchema {
				found := false
				for _, q := range viaTag {
					if p == q {
						found = true
					}
				}
				if !found {
					missing = append(missing, p)
				}
			}
			if len(missing) > 0 {
				t.Logf("  => MISSING from the delta's paths: %v", missing)
			} else {
				t.Logf("  => delta names everything the schema route does")
			}

			// And it must still carry the document from before to after.
			if delta == nil {
				t.Log("  (no delta to apply)")
				return
			}
			got, err := tony.Patch(mustParseBody(t, tc.before), delta)
			if err != nil {
				t.Fatalf("apply lowered delta: %v", err)
			}
			if left := leftoverStorage(t, got, mustParseBody(t, tc.after), keys); left != nil {
				t.Errorf("  round trip left: %s", encodeWire(t, left))
			}
		})
	}
}

func leftoverStorage(t *testing.T, a, b *ir.Node, keys map[string]string) *ir.Node {
	t.Helper()
	annotateKeysAt(a, "", keys)
	annotateKeysAt(b, "", keys)
	return tony.Diff(a, b)
}

// splitKeyedPath reports the array path and key field when p names a keyed element.
func splitKeyedPath(p string, keys map[string]string) (arrayPath, keyField string, ok bool) {
	i := len(p) - 1
	if i < 0 || p[i] != ')' {
		return "", "", false
	}
	j := 0
	for k := i; k >= 0; k-- {
		if p[k] == '(' {
			j = k
			break
		}
	}
	if j == 0 {
		return "", "", false
	}
	arrayPath = p[:j]
	f, ok := keys[arrayPath]
	return arrayPath, f, ok
}
