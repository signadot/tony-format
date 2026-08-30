package mergeop

import (
	"errors"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

// No built-in operation is namespaced, which is the half of the convention this package
// keeps. A consumer's tag is safe from a future release only for as long as this holds:
// the moment a built-in is called acme:thing, someone's acme:thing stops being theirs.
func TestNoBuiltinIsNamespaced(t *testing.T) {
	for _, s := range Symbols() {
		if strings.Contains(s.String(), NamespaceSep) {
			t.Errorf("built-in %q is namespaced; the names without a namespace are this "+
				"package's, and the ones with are the consumer's", s)
		}
	}
}

// A namespaced operation registers, is found under its full name, and is not reachable
// under the bare one -- which is the whole point, since the bare one may be built in
// later.
func TestRegisterNamespaced(t *testing.T) {
	if err := RegisterNamespaced("acmetest", stubSym("shout")); err != nil {
		t.Fatalf("RegisterNamespaced: %v", err)
	}
	if Lookup("acmetest:shout") == nil {
		t.Error("not registered under its namespaced name")
	}
	if Lookup("shout") != nil {
		t.Error("registered under the bare name as well, which is the collision it avoids")
	}
	// Twice is still a collision, within one namespace.
	if err := RegisterNamespaced("acmetest", stubSym("shout")); !errors.Is(err, ErrSymbolExists) {
		t.Errorf("re-registering answered %v, want ErrSymbolExists", err)
	}
	// The same name in another namespace is another operation.
	if err := RegisterNamespaced("othertest", stubSym("shout")); err != nil {
		t.Errorf("the same name under another namespace: %v", err)
	}
}

// Register is for this package's own names, so it refuses a namespaced one. Without that
// the two sets meet in the middle and the guarantee is only a convention.
func TestRegisterRefusesANamespacedName(t *testing.T) {
	err := Register(stubSym("acmetest:elsewhere"))
	if err == nil || !strings.Contains(err.Error(), "RegisterNamespaced") {
		t.Errorf("Register of a namespaced name answered %v, want a refusal naming RegisterNamespaced", err)
	}
}

// A namespace has to be a thing a tag can hold and a reader can tell from the operation.
func TestNamespaceIsChecked(t *testing.T) {
	for _, ns := range []string{"", "has.dot", "has:colon", "has(paren)", "has space"} {
		if err := RegisterNamespaced(ns, stubSym("x")); err == nil {
			t.Errorf("namespace %q was accepted", ns)
		}
	}
}

// An operation which describes itself is described by the tooling, the same as a built-in.
func TestANamespacedOpCanDescribeItself(t *testing.T) {
	if err := RegisterNamespaced("summarytest", describingSym{stubSym("loud")}); err != nil {
		t.Fatalf("RegisterNamespaced: %v", err)
	}
	if got := Summary("summarytest:loud"); got != "shout a string" {
		t.Errorf("Summary = %q, want the symbol's own", got)
	}
	if got := Summary("summarytest:absent"); got != "" {
		t.Errorf("Summary of an unregistered name = %q, want empty", got)
	}
}

type stubSym string

func (s stubSym) String() string { return string(s) }
func (s stubSym) IsMatch() bool  { return false }
func (s stubSym) IsPatch() bool  { return true }
func (s stubSym) Instance(child *ir.Node, args []string) (Op, error) {
	return stubOp{}, nil
}

type describingSym struct{ stubSym }

func (describingSym) Summary() string { return "shout a string" }

type stubOp struct{}

func (stubOp) String() string { return "stub" }
func (stubOp) Match(*ir.Node, *OpContext, MatchFunc) (bool, error) {
	return false, nil
}
func (stubOp) Patch(doc *ir.Node, _ *OpContext, _ MatchFunc, _ PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	return doc, nil
}
