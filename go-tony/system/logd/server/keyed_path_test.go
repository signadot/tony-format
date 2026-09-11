package server

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A client addresses an element of a keyed array by its identity, in any of the three
// spellings, and the session canonicalizes the path before the store sees it
// (element_identity.md, ident.CanonicalPath). The store holds the element under its name;
// the client reads and writes arrays.
func TestKeyedElementIsAddressableOverTheWire(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	schema, err := parse.Parse([]byte(`{define: {items: {sku: !logd-key null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %s", err)
	}
	narrowWrite(t, store, "", `{items: [{sku: B, q: 2}, {sku: A, q: 1}]}`)

	for _, test := range []struct {
		name    string
		request string
		want    string
	}{
		{"the array reads back as an array, in name order", `{id: "r", match: {path: "items"}}`, `[{q: 1 sku: A} {q: 2 sku: B}]`},
		{"the stored field", `{id: "r", match: {path: "items.\"(sku=A)\""}}`, `{q: 1 sku: A}`},
		{"the self-describing sugar", `{id: "r", match: {path: "items(sku=A)"}}`, `{q: 1 sku: A}`},
		{"the sugar resolved against the schema", `{id: "r", match: {path: "items(A)"}}`, `{q: 1 sku: A}`},
		{"a field of the element", `{id: "r", match: {path: "items(A).q"}}`, `1`},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := narrowRequest(t, store, test.request)
			if resp.Error != nil {
				t.Fatalf("error: %+v", resp.Error)
			}
			if resp.Result == nil || resp.Result.Match == nil {
				t.Fatalf("no match result: %+v", resp)
			}
			if got := wireOf(t, resp.Result.Match.Body); got != test.want {
				t.Errorf("body is %s, want %s", got, test.want)
			}
		})
	}

	// A write at the element's name, through the sugar, and without the key in the body.
	resp := narrowRequest(t, store, `{id: "w", patch: {path: "items(A)", data: {q: 7}}}`)
	if resp.Error != nil {
		t.Fatalf("write at items(A): %+v", resp.Error)
	}
	resp = narrowRequest(t, store, `{id: "r", match: {path: "items"}}`)
	if got, want := wireOf(t, resp.Result.Match.Body), `[{q: 7 sku: A} {q: 2 sku: B}]`; got != want {
		t.Errorf("after the write, items is %s, want %s", got, want)
	}

	// A position on a keyed array is refused where the client can be told.
	resp = narrowRequest(t, store, `{id: "r", match: {path: "items[0]"}}`)
	if resp.Error == nil {
		t.Errorf("items[0] on a keyed array was answered: %+v", resp.Result)
	}
}

// A patch response answers the data as the client wrote it, as committed: a keyed array
// comes back an array, an element carries its key, and an id the store generated is in it. It came back in the store's form, an
// object of the elements' names -- {items: {(sku=A): {...}}} -- so a client learning its
// generated ids from items[0].id found nothing there (750qjcswh12ksyxxmdn0).
func TestPatchResponseAnswersKeyedArraysAsArrays(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	defer store.Close()
	schema, err := parse.Parse([]byte(`{define: {items: {sku: !logd-key null}, users: {id: !logd-auto-id null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetSchema(schema, false); err != nil {
		t.Fatalf("SetSchema: %s", err)
	}

	for _, test := range []struct{ name, request, want string }{
		{"a keyed array at the root's field", `{id: "p", patch: {path: "", data: {items: [{sku: A, q: 1}]}}}`, `{items: [{sku: A q: 1}]}`},
		{"the keyed array itself", `{id: "p", patch: {path: "items", data: [{sku: B, q: 2}]}}`, `[{sku: B q: 2}]`},
		{"an element, by its name", `{id: "p", patch: {path: "items(A)", data: {q: 7}}}`, `{q: 7 sku: A}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			resp := narrowRequest(t, store, test.request)
			if resp.Error != nil {
				t.Fatalf("error: %+v", resp.Error)
			}
			if resp.Result == nil || resp.Result.Patch == nil {
				t.Fatalf("no patch result: %+v", resp)
			}
			if got := wireOf(t, resp.Result.Patch.Data); got != test.want {
				t.Errorf("data is %s, want %s", got, test.want)
			}
		})
	}

	// A generated id is learnt where the client put the element: items[0], not under a name.
	resp := narrowRequest(t, store, `{id: "p", patch: {path: "", data: {users: [{name: ada}]}}}`)
	if resp.Error != nil || resp.Result == nil || resp.Result.Patch == nil {
		t.Fatalf("write: %+v", resp)
	}
	id, err := resp.Result.Patch.Data.GetKPath("users[0].id")
	if err != nil || id == nil || id.String == "" {
		t.Errorf("users[0].id = %v (err %v) in %s, want the generated id", id, err, wireOf(t, resp.Result.Patch.Data))
	}
}
