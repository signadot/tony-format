package server

import (
	"errors"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A read answers null when a null was written there, and not otherwise.
//
// On a store where nothing has been written there is no document to navigate, and every
// path answered null with no error -- so "you asked for something that is not there" and
// "somebody wrote null here" were the same answer, and which one a caller got depended on
// whether some ANCESTOR happened to resolve. A path under a written ancestor said
// not_found; the same path with nothing above it said null (bymhrqz7h12ksas3jhn0).
//
// A caller cannot recover the distinction from the answer, so each one invents a
// tiebreak: verse asks the parent for the single key and reads absence off whether it
// came back, which is a second round trip and its guess at a rule the store should state.
func TestAnEmptyDocumentIsAbsentAtEveryPath(t *testing.T) {
	for _, path := range []string{"a", "a.b.c", "verse.a", "verse.a.b.c"} {
		t.Run(path, func(t *testing.T) {
			_, err := extractPathValue(nil, path)
			if err == nil {
				t.Fatalf("answered without error; a path nobody wrote to must not read as a value")
			}
			var pe *PathError
			if !errors.As(err, &pe) {
				t.Fatalf("error is %T, want *PathError", err)
			}
			if pe.Kind != PathAbsent {
				t.Errorf("Kind = %v, want PathAbsent", pe.Kind)
			}
			if pe.Resolved != "" {
				t.Errorf("Resolved = %q, want empty: nothing resolved", pe.Resolved)
			}
		})
	}
}

// The same path answers the same way whether or not an ancestor happens to resolve. This
// is the property the issue asks for, and the one the old behaviour broke.
func TestAbsenceDoesNotDependOnTheNeighbourhood(t *testing.T) {
	written := ir.FromMap(map[string]*ir.Node{
		"verse": ir.FromMap(map[string]*ir.Node{
			"a": ir.FromMap(map[string]*ir.Node{"b": ir.FromInt(1)}),
		}),
	})
	for _, tc := range []struct {
		name, path string
		doc        *ir.Node
	}{
		{"ancestors resolve", "verse.a.ZZZ", written},
		{"no ancestor resolves", "verse.a.ZZZ", nil},
		{"nothing written at all", "nope.at.all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractPathValue(tc.doc, tc.path)
			var pe *PathError
			if !errors.As(err, &pe) || pe.Kind != PathAbsent {
				t.Errorf("got %v, want a PathAbsent error", err)
			}
		})
	}
}

// A null somebody wrote still reads as null. That is the other half of the rule, and the
// half a fix could break by reporting absence for everything.
func TestAWrittenNullStillReadsAsNull(t *testing.T) {
	doc := ir.FromMap(map[string]*ir.Node{
		"verse": ir.FromMap(map[string]*ir.Node{"n": ir.Null()}),
	})
	got, err := extractPathValue(doc, "verse.n")
	if err != nil {
		t.Fatalf("a written null answered %v", err)
	}
	if got == nil || got.Type != ir.NullType {
		t.Errorf("got %v, want a null node", got)
	}
}
