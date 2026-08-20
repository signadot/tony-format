package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// countingHandler counts records instead of writing them, so a test can assert
// how many times something was reported rather than that it was reported at all.
type countingHandler struct{ n *int }

func (h countingHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (h countingHandler) Handle(context.Context, slog.Record) error { *h.n++; return nil }
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler        { return h }
func (h countingHandler) WithGroup(string) slog.Handler             { return h }

func countingLogger(n *int) *slog.Logger { return slog.New(countingHandler{n: n}) }

func mustDoc(t *testing.T, src string) *ir.Node {
	t.Helper()
	doc, err := parse.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return doc
}

// An extraction failure has to say WHICH of three things happened. They read the
// same in a log otherwise, and only one of them is ordinary: a watch on a path
// nothing has written yet reports absence until someone writes there, which is
// how watching something that does not exist is supposed to work.
func TestExtractPathValueClassifiesFailures(t *testing.T) {
	doc := mustDoc(t, "verse:\n  entity:\n    a: 1\n  scalar: hello\n  votes: [1, 2]\n")

	for _, tc := range []struct {
		name     string
		path     string
		kind     PathErrorKind
		resolved string
		// substrings the message must carry, so a reader can act on it
		says []string
	}{
		{
			name: "absent below an existing parent", path: "verse.github.pr",
			kind: PathAbsent, resolved: "verse",
			says: []string{`no value at "verse.github.pr"`, `resolved through "verse"`, `no field "github"`},
		},
		{
			name: "absent at the root", path: "nosuch.thing",
			kind: PathAbsent, resolved: "",
			says: []string{`no field "nosuch" at the document root`},
		},
		{
			name: "a scalar sits where an object is expected", path: "verse.scalar.deeper",
			kind: PathTypeConflict, resolved: "verse.scalar",
			says: []string{`"verse.scalar" is`, "not an object"},
		},
		{
			// This one never resolves however long the caller waits, which is
			// why it must not be reported as "no data yet".
			// An index into an object is a disagreement about what is THERE, not a
			// malformed path: write an array at verse.entity and [0] resolves. It is
			// the same answer a field under a string gets.
			name: "an index where an object is", path: "verse.entity[0]",
			kind: PathTypeConflict, resolved: "verse.entity",
			says: []string{`no value at "verse.entity[0]"`, "not an array"},
		},
		{
			// The right kind of container, and no such element.
			name: "an index past the end of an array", path: "verse.votes[9]",
			kind: PathAbsent, resolved: "verse.votes",
			says: []string{`no value at "verse.votes[9]"`, `no element "[9]"`},
		},
		{
			// A wildcard names a SET, and a read answers one value: no state makes it
			// resolvable, which is what a bad segment is.
			name: "a wildcard segment", path: "verse.*",
			kind: PathBadSegment, resolved: "verse",
			says: []string{"names a set of values"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := extractPathValue(doc, tc.path)
			if err == nil {
				t.Fatalf("extractPathValue(%q) succeeded, want a failure", tc.path)
			}

			var pe *PathError
			if !errors.As(err, &pe) {
				t.Fatalf("error is %T, want *PathError: %v", err, err)
			}
			if pe.Kind != tc.kind {
				t.Errorf("Kind = %v, want %v", pe.Kind, tc.kind)
			}
			if pe.Resolved != tc.resolved {
				t.Errorf("Resolved = %q, want %q", pe.Resolved, tc.resolved)
			}
			for _, want := range tc.says {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("message %q does not mention %q", err.Error(), want)
				}
			}

			// The sentinel means ABSENCE, and only absence. It used to match every kind,
			// so a caller writing the obvious errors.Is(err, ErrPathNotFound) read a
			// type conflict as "nothing there" and created what it thought was missing,
			// over something already there -- the same collapse the wire codes stopped
			// making, one layer lower (yy0cfe9mh12kr6pwgsn0).
			if got := errors.Is(err, ErrPathNotFound); got != (tc.kind == PathAbsent) {
				t.Errorf("errors.Is(err, ErrPathNotFound) = %v for kind %v, want %v",
					got, tc.kind, tc.kind == PathAbsent)
			}
			// And the question the sentinel used to answer by accident, asked on purpose.
			if !NoValueAt(err) {
				t.Error("NoValueAt = false; every one of these is a path holding no value")
			}
			if got := IsPathAbsent(err); got != (tc.kind == PathAbsent) {
				t.Errorf("IsPathAbsent = %v, want %v", got, tc.kind == PathAbsent)
			}
		})
	}
}

// A path that resolves must not be reported as anything.
func TestExtractPathValueResolves(t *testing.T) {
	doc := mustDoc(t, "verse:\n  entity:\n    a: 1\n")
	got, err := extractPathValue(doc, "verse.entity")
	if err != nil {
		t.Fatalf("extractPathValue: %v", err)
	}
	if got == nil || got.Type != ir.ObjectType {
		t.Fatalf("got %v, want the object at verse.entity", got)
	}
}

// The arrival half of the absence report fires once, and only for a real value:
// a null subtree is how an absent path is delivered, not news that it arrived.
func TestWatchAbsenceReportsArrivalOnce(t *testing.T) {
	var seen int
	w := &watchAbsence{path: "verse.github.pr"}
	w.log = countingLogger(&seen)

	// Not armed: nothing to report.
	w.observe(ir.FromString("x"))
	if seen != 0 {
		t.Fatalf("reported %d arrivals before arming, want 0", seen)
	}

	w.arm()
	w.observe(nil)
	w.observe(ir.Null())
	if seen != 0 {
		t.Fatalf("reported %d arrivals for a null subtree, want 0", seen)
	}

	w.observe(ir.FromString("x"))
	w.observe(ir.FromString("y"))
	if seen != 1 {
		t.Fatalf("reported %d arrivals, want exactly 1", seen)
	}
}
