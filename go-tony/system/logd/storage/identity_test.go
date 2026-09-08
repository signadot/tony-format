package storage

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/ident"
)

// Element identity (element_identity.md): a keyed array is stored as an object whose
// fields are its elements' names, and read back as the array a client wrote. These are
// the properties that follow from it, asserted through the store's own API.

func elementName(field, value string) string {
	return kpath.ChildField("items", "("+field+"="+value+")")
}

func skuList(t *testing.T, doc *ir.Node) []string {
	t.Helper()
	items, err := doc.GetKPath("items")
	if err != nil || items == nil {
		t.Fatalf("no items in %s", mustEncode(t, doc))
	}
	if items.Type != ir.ArrayType {
		t.Fatalf("items reads back as %s, not an array:\n%s", items.Type, mustEncode(t, doc))
	}
	var out []string
	for _, e := range items.Values {
		out = append(out, ir.Get(e, "sku").String)
	}
	return out
}

// A keyed array reads back as an array, in name order, and a write naming one element
// touches that element and no other.
func TestKeyedArrayRoundTripsInNameOrder(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)

	c := mustCommit(t, s, nil, `{items: [{sku: B, q: 2}, {sku: A, q: 1}]}`)
	doc, err := readStateAt(s, "", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := skuList(t, doc); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("read back %v, want name order [A B]", got)
	}
	items, _ := doc.GetKPath("items")
	if _, keyed := items.KeyField(); keyed {
		t.Errorf("a state is op-free, and the array read back carries %s", items.Tag)
	}

	c = mustCommit(t, s, nil, `{items: [{sku: A, q: 9}]}`)
	doc, _ = readStateAt(s, "", c, nil)
	if got := skuList(t, doc); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("after writing A alone, read back %v; B must survive", got)
	}
	a, _ := doc.GetKPath(`items."(sku=A)"`)
	if a != nil {
		t.Errorf("the raised document has a field named (sku=A); names are the store's, not the client's")
	}
	if q := ir.Get(items0(t, doc, "A"), "q"); q == nil || q.Int64 == nil || *q.Int64 != 9 {
		t.Errorf("A.q = %v, want 9", q)
	}
}

func items0(t *testing.T, doc *ir.Node, sku string) *ir.Node {
	t.Helper()
	items, _ := doc.GetKPath("items")
	for _, e := range items.Values {
		if ir.Get(e, "sku").String == sku {
			return e
		}
	}
	t.Fatalf("no element %s", sku)
	return nil
}

// An element is a field: the index holds it under its name, a read at the name narrows to
// it without reading the array, and the counters say so.
func TestKeyedElementIsAFieldAndNarrows(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}, {sku: B, q: 2}, {sku: C, q: 3}]}`)
	if err := s.SwitchDLog(); err != nil {
		t.Fatal(err)
	}
	c := mustCommit(t, s, nil, `{items: [{sku: B, q: 20}]}`)

	paths := indexPathSet(s)
	for _, want := range []string{elementName("sku", "A"), elementName("sku", "B"), elementName("sku", "C")} {
		if !slices.Contains(paths, want) {
			t.Errorf("index has no %q; an element is a field of the array\n got %v", want, paths)
		}
	}
	if slices.Contains(paths, "items[0]") {
		t.Errorf("index has items[0]; identity replaces position")
	}

	before := s.ReadStats()
	elem, narrowed, err := readSubtreeAt(s, elementName("sku", "B"), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !narrowed {
		t.Fatalf("a read at an element did not narrow")
	}
	if q := ir.Get(elem, "q"); q == nil || q.Int64 == nil || *q.Int64 != 20 {
		t.Errorf("B reads %s, want q: 20", mustEncode(t, elem))
	}
	if sku := ir.Get(elem, "sku"); sku == nil || sku.String != "B" {
		t.Errorf("an element read on its own carries its key; got %s", mustEncode(t, elem))
	}
	after := s.ReadStats()
	if after.WideNonField != before.WideNonField {
		t.Errorf("a keyed read was counted wide (keyed-or-idx %d -> %d)", before.WideNonField, after.WideNonField)
	}
	if after.Narrow != before.Narrow+1 {
		t.Errorf("narrow reads %d -> %d, want one more", before.Narrow, after.Narrow)
	}
}

// Inserting one element into N writes one name: the stored delta touches the new element
// and its ancestors and nothing else, so the other N are not read, rewritten or resident.
func TestInsertingOneElementWritesOneName(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}, {sku: B, q: 2}, {sku: C, q: 3}]}`)
	c := mustCommit(t, s, nil, `{items: [{sku: D, q: 4}]}`)

	var touched []string
	for _, seg := range s.index.AllSegments() {
		if seg.EndCommit == c && seg.StartCommit != seg.EndCommit && !seg.Spine {
			touched = append(touched, seg.KindedPath)
		}
	}
	slices.Sort(touched)
	for _, p := range touched {
		if strings.Contains(p, "(sku=A)") || strings.Contains(p, "(sku=B)") || strings.Contains(p, "(sku=C)") {
			t.Errorf("writing D touched %q", p)
		}
	}
	if !slices.ContainsFunc(touched, func(p string) bool { return strings.HasPrefix(p, elementName("sku", "D")) }) {
		t.Errorf("writing D left no segment under its name; wrote %v", touched)
	}
	// And a client's merge, handed the raised delta, lands D beside the others.
	ns, err := s.ReadPatchesInRange("", c, c, nil)
	if err != nil || len(ns) != 1 {
		t.Fatalf("delta for %d: %v %v", c, ns, err)
	}
	prev, _ := readStateAt(s, "", c-1, nil)
	next, err := api.NextState(prev, ns[0].Patch)
	if err != nil {
		t.Fatalf("applying the raised delta: %v\n%s", err, mustEncode(t, ns[0].Patch))
	}
	if got := skuList(t, next); !slices.Equal(got, []string{"A", "B", "C", "D"}) {
		t.Errorf("the raised delta applied by a client gives %v", got)
	}
}

// A delete names what left. The raised delta says !delete with the identity, which is how
// a client's keyed merge removes the right element.
func TestDeletingOneElementIsRaisedAsAKeyedDelete(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	c0 := mustCommit(t, s, nil, `{items: [{sku: A, q: 1}, {sku: B, q: 2}]}`)
	c := mustCommit(t, s, nil, `{items: [!delete {sku: A}]}`)

	doc, _ := readStateAt(s, "", c, nil)
	if got := skuList(t, doc); !slices.Equal(got, []string{"B"}) {
		t.Fatalf("after deleting A: %v", got)
	}
	ns, _ := s.ReadPatchesInRange("", c, c, nil)
	prev, _ := readStateAt(s, "", c0, nil)
	next, err := api.NextState(prev, ns[0].Patch)
	if err != nil {
		t.Fatalf("applying the raised delete: %v\n%s", err, mustEncode(t, ns[0].Patch))
	}
	if got := skuList(t, next); !slices.Equal(got, []string{"B"}) {
		t.Errorf("a client applying the raised delta has %v, want [B]\n delta: %s", got, mustEncode(t, ns[0].Patch))
	}
}

// The identity of an element is immutable: a write that changes a key field, or that names
// an element while carrying another key, is refused at the write.
func TestIdentityIsImmutableAndAuthoritative(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}]}`)

	err := scopedCommit(t, s, nil, elementName("sku", "A"), `{sku: B, q: 2}`)
	if err == nil || !strings.Contains(err.Error(), "authoritative") {
		t.Errorf("an element disagreeing with its name was written: %v", err)
	}
	// Written at the element's name without its key, the key is filled in.
	if err := scopedCommit(t, s, nil, elementName("sku", "A"), `{q: 5}`); err != nil {
		t.Fatalf("a write at an element's name was refused: %v", err)
	}
	c, _ := s.GetCurrentCommit()
	doc, _ := readStateAt(s, "", c, nil)
	a := items0(t, doc, "A")
	if q := ir.Get(a, "q"); q == nil || q.Int64 == nil || *q.Int64 != 5 {
		t.Errorf("A after the element write: %s", mustEncode(t, a))
	}
}

// What is not a name is refused where it is written: a null or missing key, a !key the
// schema does not declare, a position on a keyed array.
func TestWhatCannotBeNamedIsRefused(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}]}`)
	for _, tc := range []struct{ name, path, body, want string }{
		{"null key", "", `{items: [{sku: null, q: 1}]}`, "no name"},
		{"missing key", "", `{items: [{q: 1}]}`, "no name"},
		{"a key the schema does not declare", "", `{other: !key(id) [{id: x}]}`, "schema gives it no identity"},
		{"a position on a keyed array", "items[0]", `{q: 2}`, "identity replaces position"},
		{"a field that is not a name under a keyed array", "items.plain", `{q: 2}`, "not a name"},
		{"a name binding another field", "", `{items: {"(status=open)": {q: 1}}}`, "does not name an element"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := scopedCommit(t, s, nil, tc.path, tc.body)
			if err == nil {
				t.Fatalf("accepted")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused for another reason: %v", err)
			}
		})
	}
	// Nothing above landed.
	c, _ := s.GetCurrentCommit()
	doc, _ := readStateAt(s, "", c, nil)
	if got := skuList(t, doc); !slices.Equal(got, []string{"A"}) {
		t.Errorf("a refused write changed the store: %v", got)
	}
}

// Several !logd-key fields on one array are one identity, the tuple; it spells as
// <{...}> and the elements read back sorted by that spelling.
func TestCompositeIdentity(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {n: !logd-key null, name: !logd-key null}}}`)
	if got := s.schemaForScope(nil).Identity("items"); !slices.Equal(got, []string{"n", "name"}) {
		t.Fatalf("identity %v", got)
	}
	c := mustCommit(t, s, nil, `{items: [{n: 2, name: jane, v: 1}, {n: 1, name: bob, v: 2}]}`)
	paths := indexPathSet(s)
	for _, want := range []string{kpath.ChildField("items", "<{n: 1, name: bob}>"), kpath.ChildField("items", "<{n: 2, name: jane}>")} {
		if !slices.Contains(paths, want) {
			t.Errorf("index has no %q\n got %v", want, paths)
		}
	}
	doc, _ := readStateAt(s, "", c, nil)
	items, _ := doc.GetKPath("items")
	if items == nil || items.Type != ir.ArrayType || len(items.Values) != 2 {
		t.Fatalf("items reads back as %s", mustEncode(t, doc))
	}
	if ir.Get(items.Values[0], "name").String != "bob" {
		t.Errorf("elements are not in name order: %s", mustEncode(t, items))
	}
	// One element, written by its composite name, with a key field missing: filled in.
	if err := scopedCommit(t, s, nil, kpath.ChildField("items", "<{n: 1, name: bob}>"), `{v: 9}`); err != nil {
		t.Fatalf("write at a composite name: %v", err)
	}
	// And a write that binds only part of the identity is a query, not a place.
	if err := scopedCommit(t, s, nil, kpath.ChildField("items", "<{name: bob}>"), `{v: 9}`); err == nil {
		t.Error("a partial composite was accepted as a name")
	}
}

// An auto-generated identity names the element by the id the store generated.
func TestAutoIDNamesTheElement(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {id: !logd-auto-id null}}}`)
	c := mustCommit(t, s, nil, `{items: [{q: 1}]}`)
	doc, _ := readStateAt(s, "", c, nil)
	items, _ := doc.GetKPath("items")
	if items == nil || items.Type != ir.ArrayType || len(items.Values) != 1 {
		t.Fatalf("items: %s", mustEncode(t, doc))
	}
	id := ir.Get(items.Values[0], "id")
	if id == nil || id.String == "" {
		t.Fatalf("no id generated: %s", mustEncode(t, items))
	}
	if !slices.Contains(indexPathSet(s), kpath.ChildField("items", "(id="+id.String+")")) {
		t.Errorf("the element is not indexed under its generated name; paths %v", indexPathSet(s))
	}
}

// Three spellings, one name: the stored field, the self-describing sugar, and the sugar
// resolved against the schema all canonicalize to the field.
func TestThreeSpellingsOneName(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	schema := s.SchemaFor(nil)
	want := elementName("sku", "A")
	for _, spelling := range []string{want, `items(sku=A)`, `items(A)`} {
		got, err := ident.CanonicalPath(schema, spelling)
		if err != nil {
			t.Errorf("%s: %v", spelling, err)
			continue
		}
		if got != want {
			t.Errorf("%s canonicalizes to %s, want %s", spelling, got, want)
		}
	}
	if got, _ := ident.CanonicalPath(schema, `items(A).q`); got != want+".q" {
		t.Errorf("a path below the element: %s", got)
	}
	if _, err := ident.CanonicalPath(schema, `items[0]`); err == nil {
		t.Error("a position on a keyed array canonicalized")
	}
	if _, err := ident.CanonicalPath(schema, `other(A)`); err == nil {
		t.Error("a (key) on an array with no identity canonicalized")
	}
	// The type of the value is the name's: a number and a string of digits are two
	// elements. kpath removes a (key) segment's own quotes, so the bare sugar reads 42 as
	// a number either way, and the string is named through the binding form.
	if got, _ := ident.CanonicalPath(schema, `items(42)`); got != elementName("sku", `42`) {
		t.Errorf("items(42) -> %s", got)
	}
	if got, _ := ident.CanonicalPath(schema, `items(sku="42")`); got != elementName("sku", `"42"`) {
		t.Errorf("items(sku=\"42\") -> %s", got)
	}
}

// An array gains an identity only while it is empty, and never loses one.
func TestIdentityIsDeclaredBeforeTheArrayIsWritten(t *testing.T) {
	s := openTestStorage(t)
	mustCommit(t, s, nil, `{items: [{sku: A}]}`)
	_, err := s.StartMigration(mustParseBody(t, `{define: {items: {sku: !logd-key null}}}`))
	if err == nil || !strings.Contains(err.Error(), "written by position") {
		t.Fatalf("an identity was declared over positional elements: %v", err)
	}
	if s.HasPendingMigration() {
		t.Error("a refused migration is pending")
	}

	s2 := openTestStorage(t)
	declareKeyed(t, s2, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s2, nil, `{items: [{sku: A}]}`)
	if _, err := s2.StartMigration(mustParseBody(t, `{define: {items: {other: null}}}`)); err == nil ||
		!strings.Contains(err.Error(), "lose its identity") {
		t.Errorf("an identity was dropped: %v", err)
	}
	if _, err := s2.StartMigration(mustParseBody(t, `{define: {items: {other: !logd-key null}}}`)); err == nil ||
		!strings.Contains(err.Error(), "change its identity") {
		t.Errorf("an identity was changed: %v", err)
	}
	// Declaring a second, empty keyed array beside the first is fine.
	if _, err := s2.StartMigration(mustParseBody(t, `{define: {items: {sku: !logd-key null}, tags: {id: !logd-key null}}}`)); err != nil {
		t.Errorf("adding an identity for an unwritten array was refused: %v", err)
	}
}

// A precondition is written in the client's vocabulary and evaluated against the raised
// state, so a CAS on one element of a keyed array reads as the client expects.
func TestPreconditionOnAKeyedElement(t *testing.T) {
	s := openTestStorage(t)
	declareKeyed(t, s, `{define: {items: {sku: !logd-key null}}}`)
	mustCommit(t, s, nil, `{items: [{sku: A, q: 1}]}`)
	commit := func(matchPath, match, path, body string) error {
		txn, err := s.NewTx(1, nil)
		if err != nil {
			return err
		}
		p, err := txn.NewPatcher(&api.Patch{
			PathData: api.PathData{Path: path, Data: mustParseBody(t, body)},
			Match:    &api.PathData{Path: matchPath, Data: mustParseBody(t, match)},
		})
		if err != nil {
			return err
		}
		if r := p.Commit(); !r.Committed {
			if r.Error != nil {
				return r.Error
			}
			return errNotMatched
		}
		return nil
	}
	if err := commit(elementName("sku", "A"), `{q: 1}`, elementName("sku", "A"), `{q: 2}`); err != nil {
		t.Errorf("a precondition on the element's current value failed: %v", err)
	}
	if err := commit(elementName("sku", "A"), `{q: 1}`, elementName("sku", "A"), `{q: 3}`); err != errNotMatched {
		t.Errorf("a stale precondition passed: %v", err)
	}
}

// errNotMatched is a precondition that did not hold: the commit was refused with no error.
var errNotMatched = errors.New("not matched")

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
