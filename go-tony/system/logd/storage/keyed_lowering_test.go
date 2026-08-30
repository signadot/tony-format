package storage

import (
	"sort"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// The schema is the write-time AUTHORITY for what keys an array, and the !key tag on a
// lowered delta is the durable RECORD of what it decided. (Proposed as P1 of
// docs/archive/scope_overlay_plan.md, and outlived it: lowering needs the same property
// the overlay would have.) Two things have to hold for that:
//
//  1. annotation puts !key(f) on EVERY keyed array a delta describes, nested included --
//     otherwise the record is partial and a rebuild silently keys less than the live index
//  2. IndexPatch over a lowered delta yields the paths the schema route yields today --
//     otherwise "both sides index the same self-describing artefact" is not a migration,
//     it is a change of shape

// annotateKeysAt tags arrays at the paths a schema names, navigating in place. Node.GetPath
// returns a Clone, so tagging has to be done on the way down rather than by lookup.
//
// Array elements inherit the array's path, matching both api.AutoIDField.Path ("orders.items")
// and indexPatchRec's own recursion.
func annotateKeysAt(n *ir.Node, prefix string, keys map[string]string) {
	if n == nil {
		return
	}
	if f, ok := keys[prefix]; ok && n.Type == ir.ArrayType {
		if _, args := ir.TagGet(n.Tag, "!key"); len(args) != 1 {
			n.Tag = ir.TagCompose("!key", []string{f}, n.Tag)
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, fld := range n.Fields {
			p := fld.String
			if prefix != "" {
				p = prefix + "." + fld.String
			}
			annotateKeysAt(n.Values[i], p, keys)
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			annotateKeysAt(v, prefix, keys)
		}
	}
}

// keyTagsAt lists "path=field" for every array in n carrying a !key tag.
func keyTagsAt(n *ir.Node, prefix string, dst []string) []string {
	if n == nil {
		return dst
	}
	if n.Type == ir.ArrayType {
		if _, args := ir.TagGet(n.Tag, "!key"); len(args) == 1 {
			dst = append(dst, prefix+"="+args[0])
		}
	}
	switch n.Type {
	case ir.ObjectType:
		for i, fld := range n.Fields {
			p := fld.String
			if prefix != "" {
				p = prefix + "." + fld.String
			}
			dst = keyTagsAt(n.Values[i], p, dst)
		}
	case ir.ArrayType:
		for _, v := range n.Values {
			dst = keyTagsAt(v, prefix, dst)
		}
	}
	return dst
}

func lower(t *testing.T, before, after string, keys map[string]string) *ir.Node {
	t.Helper()
	b, err := parse.Parse([]byte(before))
	if err != nil {
		t.Fatalf("parse before: %v", err)
	}
	a, err := parse.Parse([]byte(after))
	if err != nil {
		t.Fatalf("parse after: %v", err)
	}
	// Strip presentation, then storableDelta. Presentation is how a value was WRITTEN,
	// and two states built separately differ in it for reasons that are nobody's intent
	// -- without the strip the delta states an !addtag(bracket) that no writer asked for.
	// These two states are PARSED here rather than read out of one chain, which is what
	// makes the strip this test's business. A write's own delta keeps presentation, since
	// its base and next come from one chain and a difference in it is the write's.
	return storableDelta(stripPresentationDeepIR(b), stripPresentationDeepIR(a), keys)
}

// TestLowering_TagsEveryKeyedArray: check 1.
func TestLowering_TagsEveryKeyedArray(t *testing.T) {
	for _, tc := range []struct {
		name            string
		before, after   string
		keys            map[string]string
		wantTagsAtLeast []string
	}{
		{
			name:            "top level",
			before:          `{items: [{sku: "A", q: 1}]}`,
			after:           `{items: [{sku: "A", q: 2}]}`,
			keys:            map[string]string{"items": "sku"},
			wantTagsAtLeast: []string{"items=sku"},
		},
		{
			name:            "nested under an object",
			before:          `{a: {items: [{sku: "A", q: 1}]}}`,
			after:           `{a: {items: [{sku: "A", q: 2}]}}`,
			keys:            map[string]string{"a.items": "sku"},
			wantTagsAtLeast: []string{"a.items=sku"},
		},
		{
			name:            "a keyed array inside a keyed array's element",
			before:          `{items: [{sku: "A", sub: [{id: "x", v: 1}]}]}`,
			after:           `{items: [{sku: "A", sub: [{id: "x", v: 2}]}]}`,
			keys:            map[string]string{"items": "sku", "items.sub": "id"},
			wantTagsAtLeast: []string{"items=sku", "items.sub=id"},
		},
		{
			name:            "element added, not just changed",
			before:          `{items: [{sku: "A", q: 1}]}`,
			after:           `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`,
			keys:            map[string]string{"items": "sku"},
			wantTagsAtLeast: []string{"items=sku"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delta := lower(t, tc.before, tc.after, tc.keys)
			if delta == nil {
				t.Fatal("no delta between before and after")
			}
			got := keyTagsAt(delta, "", nil)
			sort.Strings(got)
			t.Logf("  delta: %s", encodeWire(t, delta))
			t.Logf("  key tags in the delta: %v", got)
			for _, want := range tc.wantTagsAtLeast {
				found := false
				for _, g := range got {
					if g == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("delta does not record %q; a rebuild would key less than the live index\n delta: %s",
						want, encodeWire(t, delta))
				}
			}
		})
	}
}

// indexPathsOf indexes one patch into a fresh index and returns the paths it produces.
func indexPathsOf(t *testing.T, patch *ir.Node, schema *api.Schema) []string {
	t.Helper()
	idx := index.NewIndex("")
	last := int64(0)
	e := &dlog.Entry{Commit: 1, LastCommit: &last}
	if err := index.IndexPatch(idx, e, "A", 0, 1, 0, patch, schema, nil); err != nil {
		t.Fatalf("IndexPatch: %v", err)
	}
	seen := map[string]bool{}
	for _, seg := range idx.AllSegments() {
		seen[seg.KindedPath] = true
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// TestLowering_IndexPathsMatchSchemaRoute: check 2. The paths a lowered delta produces
// through the TAG route must be the paths the client patch produces through the SCHEMA
// route, or "both sides index the same artefact" is a change of shape rather than a
// migration.
func TestLowering_IndexPathsMatchSchemaRoute(t *testing.T) {
	schema := &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "items", Field: "sku"}}}
	keys := map[string]string{"items": "sku"}

	for _, tc := range []struct{ name, before, after, clientPatch string }{
		{
			name:        "element updated",
			before:      `{items: [{sku: "A", q: 1}]}`,
			after:       `{items: [{sku: "A", q: 2}]}`,
			clientPatch: `{items: [{sku: "A", q: 2}]}`,
		},
		{
			name:        "element added",
			before:      `{items: [{sku: "A", q: 1}]}`,
			after:       `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`,
			clientPatch: `{items: [{sku: "B", q: 2}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			delta := lower(t, tc.before, tc.after, keys)
			if delta == nil {
				t.Fatal("no delta")
			}
			viaTag := indexPathsOf(t, delta, nil)

			client, err := parse.Parse([]byte(tc.clientPatch))
			if err != nil {
				t.Fatalf("parse client patch: %v", err)
			}
			viaSchema := indexPathsOf(t, client, schema)

			t.Logf("  delta:            %s", encodeWire(t, delta))
			t.Logf("  via tag (delta):  %v", viaTag)
			t.Logf("  via schema (patch): %v", viaSchema)
			if !sameSet(viaTag, viaSchema) {
				t.Errorf("a lowered delta indexes to different paths than the schema route\n tag:    %v\n schema: %v",
					viaTag, viaSchema)
			}
		})
	}
}

// TestLowering_IndexesChangesNotWrites records where check 2 stops holding, and it is not
// a detail: the two routes answer different questions.
//
//	the schema route indexes what a commit WROTE
//	the delta route indexes what a commit CHANGED
//
// They coincide whenever a write changes what it names, which is why the cases above pass.
// They come apart in two ways, both measured here.
func TestLowering_IndexesChangesNotWrites(t *testing.T) {
	schema := &api.Schema{AutoIDFields: []api.AutoIDField{{Path: "items", Field: "sku"}}}
	keys := map[string]string{"items": "sku"}

	t.Run("a delete indexes the element that went, not the one written", func(t *testing.T) {
		delta := lower(t, `{items: [{sku: "A", q: 1}, {sku: "B", q: 2}]}`,
			`{items: [{sku: "A", q: 1}]}`, keys)
		client := mustParseBody(t, `{items: [{sku: "A", q: 1}]}`)
		viaTag, viaSchema := indexPathsOf(t, delta, nil), indexPathsOf(t, client, schema)
		t.Logf("  delta:  %s", encodeWire(t, delta))
		t.Logf("  tag:    %v", viaTag)
		t.Logf("  schema: %v", viaSchema)
		if sameSet(viaTag, viaSchema) {
			t.Error("expected these to differ; the case has changed and the note below is stale")
		}
		t.Log("  the delta names items(B) -- what left -- and the patch names items(A) -- what")
		t.Log("  was sent. Arguably the delta is the better answer for a watch: nothing about")
		t.Log("  A changed. But it is a change in which commits wake a watcher on A.")
	})

	t.Run("a coincident write indexes nothing at all", func(t *testing.T) {
		delta := lower(t, `{items: [{sku: "A", q: 1}]}`, `{items: [{sku: "A", q: 1}]}`, keys)
		if delta != nil {
			t.Fatalf("expected no delta for a write that changes nothing, got %s", encodeWire(t, delta))
		}
		client := mustParseBody(t, `{items: [{sku: "A", q: 1}]}`)
		t.Logf("  delta:  <none>")
		t.Logf("  schema: %v", indexPathsOf(t, client, schema))
		t.Log("  This one collides with R3. The scope overlay's ownership set is built from")
		t.Log("  the index paths of what the scope WROTE, precisely so that a scope writing a")
		t.Log("  value baseline already holds still owns that path -- a reconciling controller")
		t.Log("  does exactly this on every pass. Index the delta instead and that write leaves")
		t.Log("  no path, so R3's fix stops working. See the plan's P1 for the fork.")
	})
}

// mustParseBody parses a write body or fails the test. It lived in the scope stepping
// spike's tests until those went with the overlay they were exploring.
func mustParseBody(t *testing.T, body string) *ir.Node {
	t.Helper()
	n, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %v", body, err)
	}
	return n
}

// stripPresentationDeepIR removes presentation tags throughout, in place.
//
// It was the scope overlay's, which compared two independently materialized documents and
// had to strip presentation before diffing them. Nothing in the store does that any more;
// what is left are the tests that build two states by parsing and want the same thing of
// them.
func stripPresentationDeepIR(n *ir.Node) *ir.Node {
	if n == nil {
		return nil
	}
	n.Tag = ir.StripPresentation(n.Tag)
	for _, f := range n.Fields {
		stripPresentationDeepIR(f)
	}
	for _, v := range n.Values {
		stripPresentationDeepIR(v)
	}
	return n
}
