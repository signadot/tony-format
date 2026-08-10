package storage

import (
	"bytes"
	"testing"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
)

func enc(t *testing.T, s string) string {
	t.Helper()
	n, err := parse.Parse([]byte(s))
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	var buf bytes.Buffer
	if err := encode.Encode(n, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

// patchStr applies patch to doc at the tony.Patch level and returns the encoded result.
func patchStr(t *testing.T, doc, patch string) string {
	t.Helper()
	d, err := parse.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("parse doc: %v", err)
	}
	p, err := parse.Parse([]byte(patch))
	if err != nil {
		t.Fatalf("parse patch: %v", err)
	}
	res, err := tony.Patch(d, p)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if res == nil {
		return "<nil>"
	}
	var buf bytes.Buffer
	if err := encode.Encode(res, &buf, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.String()
}

// TestKey_ReplaceWithDuplicateKeys asks whether a keyed list can be replaced by a list
// that violates the key invariant — two elements with the same name.
//
// Today (state is op-free) the second write is a plain positional merge, so it can.
// Under option A the destination array declares itself keyed, so the same write would
// be merged by identity instead — and identity merge cannot produce duplicates.
func TestKey_ReplaceWithDuplicateKeys(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	scalingCommit(t, s, nil, `{items: !key(name) [{name: "a"}, {name: "b"}]}`, nil)
	showDoc(t, s, nil, "initial (keyed write)")

	scalingCommit(t, s, nil, `{items: [{name: "a"}, {name: "a"}]}`, nil)
	showDoc(t, s, nil, "after plain write of [{a},{a}]  -- TODAY")

	t.Logf("")
	t.Logf("Now the same thing at the merge level, with the destination KEYED")
	t.Logf("(which is what option A makes the stored state look like):")

	keyedDoc := `!key(name) [{name: "a"}, {name: "b"}]`
	t.Logf("  doc:                      %s", enc(t, keyedDoc))
	t.Logf("  patch !key(name)[{a},{a}]: %s", patchStr(t, keyedDoc, `!key(name) [{name: "a"}, {name: "a"}]`))
	t.Logf("  patch (untagged) [{a},{a}]: %s", patchStr(t, keyedDoc, `[{name: "a"}, {name: "a"}]`))
	t.Logf("  patch !replace [{a},{a}]:   %s", patchStr(t, keyedDoc, `!replace [{name: "a"}, {name: "a"}]`))
	t.Logf("  patch !rmtag(key) [{a},{a}]: %s", patchStr(t, keyedDoc, `!rmtag(key) [{name: "a"}, {name: "a"}]`))
	t.Logf("  patch !rmtag(key) []:        %s", patchStr(t, keyedDoc, `!rmtag(key) []`))
	t.Logf("  patch !replace{from,to}:     %s", patchStr(t, keyedDoc,
		`!replace {from: !key(name) [{name: "a"}, {name: "b"}], to: [{name: "a"}, {name: "a"}]}`))
}
