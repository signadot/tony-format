package api

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/schema"
)

func TestStorageContext_RegistersAlongsideTheBuiltins(t *testing.T) {
	reg := schema.NewContextRegistry()
	if err := reg.RegisterContext(StorageContext()); err != nil {
		t.Fatalf("RegisterContext: %v", err)
	}
	ctx, ok := reg.GetContext(StorageContextURI)
	if !ok {
		t.Fatal("logd storage context not found after registering")
	}
	for _, want := range []string{"insert", "delete", "key", "raw"} {
		if ctx.Tags[want] == nil {
			t.Errorf("storage context does not declare %q", want)
		}
	}
	// The narrowing is the point: these are patch-context tags that are NOT storable.
	for _, notWant := range []string{"replace", "strdiff", "arraydiff", "rename", "jsonpatch", "pipe", "if"} {
		if ctx.Tags[notWant] != nil {
			t.Errorf("storage context declares %q, which cannot be stored", notWant)
		}
	}
	if ctx.Tags["key"].SchemaRef == "" {
		t.Error("!key should point at where its extra requirement is defined")
	}
}

func TestValidateForStorage_Vocabulary(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		wantErr   string // substring, empty means it must pass
	}{
		{"plain data", `{a: 1, b: {c: "x"}}`, ""},
		{"a data tag that is not an op", `{a: !mytag 5}`, ""},
		{"insert", `{a: !insert 5}`, ""},
		{"delete", `{a: !delete 5}`, ""},
		{"keyed list", `{items: !key(sku) [{sku: "A", q: 1}, {sku: "B", q: 2}]}`, ""},
		{"tag ops", `{a: !addtag(x) 5}`, ""},

		{"checked replace", `{a: !replace {from: 1, to: 2}}`, "checked"},
		{"rename", `{a: !rename [{from: "x", to: "y"}]}`, "re-evaluates"},
		{"arraydiff", `{a: !arraydiff {0: !insert 1}}`, "re-evaluates"},
		{"pipe", `{a: !pipe ["echo"]}`, "calls out to the system"},
		{"nested, not just at the root", `{a: {b: !rename [{from: "x", to: "y"}]}}`, "a.b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = ValidateForStorage(n)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected %s to be storable, got: %v", tc.src, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error did not mention %q: %v", tc.wantErr, err)
			}
			t.Logf("  %v", err)
		})
	}
}

func TestValidateForStorage_KeyMustBeIndexRepresentable(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		wantErr   string
	}{
		{"string keys", `{items: !key(sku) [{sku: "A"}, {sku: "B"}]}`, ""},
		{"number keys", `{items: !key(sku) [{sku: 1}, {sku: 2}]}`, ""},
		{"nested key field", `{items: !key(meta.name) [{meta: {name: "a"}}]}`, ""},

		{"object-valued key", `{items: !key(sku) [{sku: {a: 1}}]}`, "no scalar"},
		{"missing key field", `{items: !key(sku) [{other: 1}]}`, "no scalar"},
		{"bare !key", `{items: !key [{a: 1}, {a: 2}]}`, "bare !key"},
		{"number and string that render alike", `{items: !key(sku) [{sku: 1}, {sku: "1"}]}`, "one path for two elements"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := parse.Parse([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = ValidateForStorage(n)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("expected %s to be storable, got: %v", tc.src, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error did not mention %q: %v", tc.wantErr, err)
			}
			t.Logf("  %v", err)
		})
	}
}
