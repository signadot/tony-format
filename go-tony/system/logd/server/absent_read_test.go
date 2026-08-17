package server

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// A read at a path with nothing at it could not narrow -- there is nothing to narrow
// to -- so it read the whole document to be told so. On a staging verse that was
// eleven of twenty-five reads, at up to 269ms each, because a charter rule watches a
// slice before anything has been written to it (ap8ddvp2h12krd43gdn0).
//
// The store answers those from the index now. What a client sees must not change: the
// same error, the same kind, the same message -- otherwise a cheap answer is a
// different answer.
func TestAbsentPathAnswersAsTheDocumentWould(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open storage: %s", err)
	}
	defer store.Close()

	for i := 0; i < 20; i++ {
		id := "e" + strconv.Itoa(i)
		narrowWrite(t, store, "verse.entities."+id, "{id: "+id+", blob: "+strings.Repeat("x", 100)+"}")
	}
	narrowWrite(t, store, "verse.demo.probe", "{k: v}")

	// an object which later became a scalar, which the index remembers only as
	// "written", and a branch which was written and then deleted
	narrowWrite(t, store, "verse.demo.was", "{k: v}")
	narrowWrite(t, store, "verse.demo", "{was: s}")
	narrowWrite(t, store, "verse.gone.branch", "{k: v}")
	narrowWrite(t, store, "verse.gone", "!delete {branch: {k: v}}")

	// cheap says whether the index is expected to answer without the document. Where
	// it is not -- a scalar in the way, which the index cannot see -- the read falls
	// back and the client is no worse off than before.
	for _, tc := range []struct {
		path  string
		cheap bool
		// generous marks the one divergence: an ancestor deleted by a patch which
		// still described what it deleted keeps a node with children, so the cheap
		// answer resolves through it and names a deeper missing field than the
		// document does. Same kind, same code, same "no value at" -- a diagnostic
		// clause differs, and the case is named here rather than glossed.
		generous bool
	}{
		{"verse.demo.qp", true, false},              // a sibling of something written
		{"verse.task.instance", true, false},        // a branch nothing has written
		{"nothing.at.all", true, false},             // not even the first segment
		{"verse.entities.e3.missing", true, false},  // under something that exists
		{"verse.demo.probe.k.deeper", false, false}, // under a scalar: the document answers
		{"verse.demo.was.k", false, false},          // was an object, is a scalar: likewise
		// A delete is a write to the path with nothing indexed under it, so the index
		// cannot tell it from a scalar landing there and declines -- which is the safe
		// half of not being able to see deletions at all.
		{"verse.gone.branch.k.deeper", false, false},
		{"verse.gone.zz", true, true}, // a sibling under a deleted branch's parent
	} {
		path := tc.path
		t.Run(path, func(t *testing.T) {
			// what the whole document says, which is the answer of record
			commit, err := store.GetCurrentCommit()
			if err != nil {
				t.Fatalf("current commit: %s", err)
			}
			doc, err := store.ReadStateAt("", commit, nil)
			if err != nil {
				t.Fatalf("wide read: %s", err)
			}
			_, wideErr := extractPathValue(doc, path)
			if wideErr == nil {
				t.Fatalf("the document has a value at %q, so this case proves nothing", path)
			}

			// and what the cheap answer says
			spine, ok := store.AbsentSpineAt(path, nil)
			if ok != tc.cheap {
				t.Fatalf("answered from the index: %v, want %v", ok, tc.cheap)
			}
			if !ok {
				// the store declined, so the client gets the wide answer unchanged
				return
			}
			_, cheapErr := extractPathValue(spine, path)
			if cheapErr == nil {
				t.Fatalf("the cheap answer found a value at %q", path)
			}

			var widePE, cheapPE *PathError
			if !errors.As(wideErr, &widePE) || !errors.As(cheapErr, &cheapPE) {
				t.Fatalf("errors are %T and %T, want *PathError", wideErr, cheapErr)
			}
			if widePE.Kind != cheapPE.Kind {
				t.Errorf("kind: cheap %v, document %v", cheapPE.Kind, widePE.Kind)
			}
			if widePE.Path != cheapPE.Path {
				t.Errorf("path: cheap %q, document %q", cheapPE.Path, widePE.Path)
			}
			if tc.generous {
				if widePE.Error() == cheapPE.Error() {
					t.Errorf("the message no longer differs; this case is not generous")
				}
				return
			}
			if widePE.Segment != cheapPE.Segment {
				t.Errorf("segment: cheap %q, document %q", cheapPE.Segment, widePE.Segment)
			}
			if widePE.Error() != cheapPE.Error() {
				t.Errorf("message differs\n cheap    %s\n document %s", cheapPE, widePE)
			}
		})
	}
}
