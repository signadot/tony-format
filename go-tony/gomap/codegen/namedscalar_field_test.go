package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedstr"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedstrpkg"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/valuehost"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/valueleaf"
	"github.com/signadot/tony-format/go-tony/ir"
)

// TestNamedScalarFieldRoundTrip guards issue f69agjyeh12ks: a named scalar held
// directly in a field generated code that did not compile in either direction —
// ir.FromString(s.Op) without the conversion to string, and
// s.Op = node.String without the conversion back to Op. Slice and map elements
// were already converted (item 8); the direct field was missed.
//
// These fixtures failing to compile is itself most of the test — go build
// catches it before this runs — but the round-trip pins the conversions as
// value-preserving rather than merely well-typed.
func TestNamedScalarFieldRoundTrip(t *testing.T) {
	orig := &namedstr.Cmd{
		Op:   namedstr.Op("apply"),
		Ops:  []namedstr.Op{"a", "b"},
		Name: "n",
	}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	// The named string must reach the document as a string, not as anything the
	// schema would have to describe differently.
	if op := ir.ToMap(node)["op"]; op == nil || op.Type != ir.StringType {
		t.Fatalf("op: got %v, want a string node", op)
	}
	var got namedstr.Cmd
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Op != orig.Op || got.Name != orig.Name || len(got.Ops) != 2 || got.Ops[1] != "b" {
		t.Errorf("round-trip mismatch: got %+v, want %+v", got, orig)
	}
}

// TestNamedScalarFieldCrossPackage covers the same shape with the named type
// declared in another package, where the conversion has to be qualified
// (namedstr.Op(...), not Op(...)).
func TestNamedScalarFieldCrossPackage(t *testing.T) {
	orig := &namedstrpkg.Holder{Op: namedstr.Op("apply")}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got namedstrpkg.Holder
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Op != orig.Op {
		t.Errorf("round-trip mismatch: got %q, want %q", got.Op, orig.Op)
	}
}

// TestCrossPackageStructValueField guards item 17: a struct VALUE field of
// another package is reached only through method calls on the value, so the
// package qualifier never appears in the generated body — while the import was
// emitted anyway, from a walk of the field types, and the package did not
// compile. valuehost referencing valueleaf by value is exactly that shape, so
// this file building at all is the assertion.
func TestCrossPackageStructValueField(t *testing.T) {
	orig := &valuehost.Host{Val: valueleaf.Leaf{V: "x"}}
	node, err := orig.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var got valuehost.Host
	if err := got.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if got.Val.V != "x" {
		t.Errorf("round-trip mismatch: got %q, want %q", got.Val.V, "x")
	}
}
