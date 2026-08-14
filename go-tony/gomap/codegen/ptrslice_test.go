package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/ptrslice"
	"github.com/signadot/tony-format/go-tony/parse"
)

// roundTripPS takes a PS the whole way a document goes -- encode to text, parse
// it back, decode -- since the three states are only distinguishable if they
// survive all of it, and it is the wire that collapses them.
func roundTripPS(t *testing.T, in *ptrslice.PS) *ptrslice.PS {
	t.Helper()
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	text := encode.MustString(node)
	parsed, err := parse.Parse([]byte(text))
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	out := &ptrslice.PS{}
	if err := out.FromTonyIR(parsed); err != nil {
		t.Fatalf("FromTonyIR of %q: %v", text, err)
	}
	return out
}

// TestPtrSlice_ThreeStatesSurviveARoundTrip is the point of the whole field
// shape: absent, explicitly empty, and present are three answers, and a plain
// slice can only carry two of them (issue h1500vtxh12krec6fxn0).
func TestPtrSlice_ThreeStatesSurviveARoundTrip(t *testing.T) {
	empty := []string{}
	present := []string{"a", "b"}

	for _, tc := range []struct {
		name     string
		in       *[]string
		wantNil  bool
		wantLen  int
		wantElem []string
	}{
		{name: "absent", in: nil, wantNil: true},
		{name: "explicitly empty", in: &empty, wantLen: 0},
		{name: "present", in: &present, wantLen: 2, wantElem: []string{"a", "b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTripPS(t, &ptrslice.PS{Probe: tc.in})
			if tc.wantNil {
				if got.Probe != nil {
					t.Fatalf("absent came back as %v, want nil", *got.Probe)
				}
				return
			}
			if got.Probe == nil {
				t.Fatalf("a present pointer came back nil, so %q is indistinguishable from absent", tc.name)
			}
			if len(*got.Probe) != tc.wantLen {
				t.Fatalf("got %d elements, want %d", len(*got.Probe), tc.wantLen)
			}
			for i, want := range tc.wantElem {
				if (*got.Probe)[i] != want {
					t.Errorf("element %d is %q, want %q", i, (*got.Probe)[i], want)
				}
			}
		})
	}
}

// TestPtrSlice_ElementKinds: a pointer to a slice decodes its elements the same
// way the plain slice beside it does, whether they are a builtin, a named
// builtin, or a struct that has a codec of its own.
func TestPtrSlice_ElementKinds(t *testing.T) {
	ports := []int{80, 443}
	steps := []ptrslice.Step{{Name: "build"}, {Name: "test"}}
	plain := []string{"x"}

	got := roundTripPS(t, &ptrslice.PS{Ports: &ports, Steps: &steps, Plain: plain})

	if got.Ports == nil || len(*got.Ports) != 2 || (*got.Ports)[0] != 80 || (*got.Ports)[1] != 443 {
		t.Errorf("ports round-tripped to %v, want [80 443]", got.Ports)
	}
	if got.Steps == nil || len(*got.Steps) != 2 || (*got.Steps)[0].Name != "build" || (*got.Steps)[1].Name != "test" {
		t.Errorf("steps round-tripped to %v, want [{build} {test}]", got.Steps)
	}
	if len(got.Plain) != 1 || got.Plain[0] != "x" {
		t.Errorf("plain slice round-tripped to %v, want [x]", got.Plain)
	}
}

// TestPtrSlice_EmptyIsAnArrayOnTheWire: the encoded form is what another reader
// sees, so an explicitly empty list has to be an empty array and not a missing
// field -- that is the direction omitzero collapses on a plain slice.
func TestPtrSlice_EmptyIsAnArrayOnTheWire(t *testing.T) {
	empty := []string{}
	node, err := (&ptrslice.PS{Probe: &empty}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	probe, _ := node.GetPath("$.probe")
	if probe == nil {
		t.Fatal("an explicitly empty list was not emitted at all, so it reads as absent")
	}
	if len(probe.Values) != 0 {
		t.Fatalf("empty list emitted %d elements", len(probe.Values))
	}

	absent, err := (&ptrslice.PS{}).ToTonyIR()
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := absent.GetPath("$.probe"); v != nil {
		t.Fatalf("a nil pointer emitted %v, want nothing", v)
	}
}

// TestPtrSlice_ReflectionPathAgrees: gomap's reflection path is the fallback for
// types without generated code, and a document must not mean different things
// depending on which path read it.
func TestPtrSlice_ReflectionPathAgrees(t *testing.T) {
	type reflPS struct {
		Probe *[]string `tony:"field=probe,omitzero"`
	}
	empty := []string{}

	for _, tc := range []struct {
		name    string
		in      *[]string
		wantNil bool
	}{
		{name: "absent", in: nil, wantNil: true},
		{name: "explicitly empty", in: &empty},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := gomap.ToTonyIR(&reflPS{Probe: tc.in})
			if err != nil {
				t.Fatalf("reflection ToTonyIR: %v", err)
			}
			out := &reflPS{}
			if err := gomap.FromTonyIR(node, out); err != nil {
				t.Fatalf("reflection FromTonyIR: %v", err)
			}
			if tc.wantNil != (out.Probe == nil) {
				t.Fatalf("nil-ness did not survive: sent nil=%v, got nil=%v", tc.wantNil, out.Probe == nil)
			}
		})
	}
}
