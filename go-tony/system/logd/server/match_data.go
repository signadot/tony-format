package server

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// ErrPathNotFound is returned when a path does not exist in the document.
// This is distinct from a path that exists but has a null value.
//
// Every failure from extractPathValue matches it, so a caller that only needs
// "no value here" can keep asking that question. A caller that has to decide how
// alarmed to be wants PathError.Kind instead.
var ErrPathNotFound = errors.New("path not found")

// PathErrorKind says what the CURRENT STATE says about a path. Three facts about the
// present, and they call for different reactions:
//
//	PathAbsent        nothing there. Nothing in the document contradicts the path, so
//	                  creating what is missing is a reasonable next move.
//	PathTypeConflict  something there, of a shape which cannot hold what was asked for --
//	                  an index into an object, a field under a string. Creating here means
//	                  CLOBBERING what is already there, so the move is to stop and
//	                  re-examine the shape you assumed.
//	PathBadSegment    not a well-formed question. A wildcard names a set of values and a
//	                  read answers one.
//
// None of them says anything about the future, which nothing can support: in a mutable
// document a.b[0] resolves the moment someone writes an array at a.b, exactly as a.b.c
// resolves the moment someone writes an object at a.b. "Can never resolve" is not a
// property of a path, and an earlier version of this comment reasoned from it -- which is
// why an index segment was classified as a malformed path and every read at an array
// element was refused (yy0cfe9mh12kr6pwgsn0).
//
// None of them indicates a storage problem: extraction is pure navigation of a document
// that was already read.
type PathErrorKind int

const (
	// PathAbsent: a segment is simply not in the document. Ordinary, and it
	// resolves itself — a watch registered before the first write to its path
	// reports this until someone writes there, which is the normal way to start
	// watching something that does not exist yet.
	PathAbsent PathErrorKind = iota

	// PathTypeConflict: navigation reached a non-object where the path expects a
	// field. It MAY resolve (whatever sits there is later overwritten with an
	// object) or may be a lasting disagreement about the document's shape, and
	// nothing here can tell which.
	PathTypeConflict

	// PathBadSegment: the path addresses something other than an object field —
	// an index or sparse segment. Extraction does not support those, so unlike
	// the other two this NEVER resolves, however long the caller waits.
	PathBadSegment
)

// PathError reports a path that did not resolve, and how far it got.
//
// Resolved is the part that did navigate, which is the difference between a line
// an operator can read ("resolved through verse, no field github" — the document
// is there, the subtree is not) and one that just looks like data loss.
type PathError struct {
	Kind     PathErrorKind
	Path     string  // the full path requested
	Segment  string  // the segment that failed
	Resolved string  // deepest prefix that did resolve; "" means none did
	Found    ir.Type // what was there instead, for PathTypeConflict
}

func (e *PathError) Error() string {
	switch e.Kind {
	case PathBadSegment:
		return fmt.Sprintf("cannot extract %q: segment %q names a set of values, not one", e.Path, e.Segment)
	case PathTypeConflict:
		where := "the document root"
		if e.Resolved != "" {
			where = fmt.Sprintf("%q", e.Resolved)
		}
		// What the segment asked for, so the message names the disagreement rather than
		// assuming every segment wants an object: `[0]` wants an array.
		return fmt.Sprintf("no value at %q: %s is %v, not %s", e.Path, where, e.Found, wants(e.Segment))
	default:
		what := "field"
		if _, isField := kpath.SegmentFieldName(e.Segment); !isField {
			what = "element"
		}
		if e.Resolved == "" {
			return fmt.Sprintf("no value at %q: no %s %q at the document root", e.Path, what, e.Segment)
		}
		return fmt.Sprintf("no value at %q: resolved through %q, no %s %q", e.Path, e.Resolved, what, e.Segment)
	}
}

// wants says what kind of container a segment addresses, for a message about one that is
// not that kind.
func wants(seg string) string {
	if _, isField := kpath.SegmentFieldName(seg); isField {
		return "an object"
	}
	if p, err := kpath.Parse(seg); err == nil && p != nil && p.EntryKind() == kpath.SparseArrayEntry {
		return "a sparse array"
	}
	return "an array"
}

// Is matches ErrPathNotFound for ABSENCE only.
//
// It used to match every kind, on the reasoning that a caller which only wants to know
// whether a value exists should not have to care why. But that is the same collapse the
// wire codes just stopped making, one layer lower: a caller writing the obvious
// errors.Is(err, ErrPathNotFound) read a type conflict as absence and went on to create
// what it thought was missing, over something already there. A split which exists on the
// wire and not in Go is a trap for whoever has not been bitten yet.
//
// A caller which genuinely wants "no value at this path, whatever the reason" asks
// NoValueAt.
func (e *PathError) Is(target error) bool {
	return target == ErrPathNotFound && e.Kind == PathAbsent
}

// NoValueAt reports that a path holds no value, for whatever reason: nothing there, or
// something of a shape that cannot hold it. It is the question the sentinel used to answer
// by accident, asked on purpose.
func NoValueAt(err error) bool {
	var pe *PathError
	return errors.As(err, &pe)
}

// IsPathAbsent reports the ordinary case: the path has no value and nothing there
// contradicts it. Equivalent to errors.Is(err, ErrPathNotFound) now that the sentinel
// narrows; kept because it says which question it is asking.
//
// Older doc, kept for the sense of it: the path has no value yet and will
// have one as soon as something writes there. It is the one that should not
// raise an alarm.
func IsPathAbsent(err error) bool {
	var pe *PathError
	return errors.As(err, &pe) && pe.Kind == PathAbsent
}

// extractPathValue navigates the document structure according to the kpath
// and returns the value at that path. The document structure mirrors the path.
// For example, path "users" with doc {users: {id: "1"}} returns {id: "1"}.
// Empty path returns the doc as-is.
//
// A failure is always a *PathError; see PathErrorKind for what the caller can do
// about each one.
func extractPathValue(doc *ir.Node, kp string) (*ir.Node, error) {
	if kp == "" {
		// The whole document, including the empty one. A store where nothing has been
		// written has a null root, and that is a fact about the store rather than about
		// a path: there is no segment that failed to resolve, so there is nothing to
		// report absent.
		if doc == nil {
			return ir.Null(), nil
		}
		return doc, nil
	}
	if doc == nil {
		// Nothing has been written anywhere, so nothing is at kp -- and that is the same
		// answer the walk below gives for a path whose ancestors DO resolve. It used to
		// be null, which made an unwritten path indistinguishable from a written null,
		// and made which one a caller got depend on whether some ancestor happened to
		// exist (bymhrqz7h12ksas3jhn0). Nothing resolved, so Resolved is empty and
		// Segment is the first thing asked for.
		first := kp
		if parts := splitKPath(kp); len(parts) > 0 {
			first = parts[0]
		}
		return nil, &PathError{Kind: PathAbsent, Path: kp, Segment: first}
	}

	// Navigate through the document following the path structure. Segment matching
	// canonicalizes quoting (see valueAtPath / patchMayAffect): SplitAll yields a
	// digit-first key as the quoted "9digitdid" but it is stored unquoted, so the
	// segment is reduced to its canonical field name and the key is matched in
	// either stored form. A verbatim compare drops every quoted key (~62% of %08x
	// decision ids are digit-first), which here would report the subtree absent.
	current := doc
	parts := splitKPath(kp)

	// The names matched so far, so a failure can say how deep it got rather than
	// only which segment stopped it.
	var resolved []string

	for _, part := range parts {
		// A non-field segment addresses inside a container that is not an object, so the
		// object check below is asked only of the segments it is about.
		_, isFieldSeg := kpath.SegmentFieldName(part)
		if isFieldSeg && (current == nil || current.Type != ir.ObjectType) {
			found := ir.NullType
			if current != nil {
				found = current.Type
			}
			return nil, &PathError{
				Kind: PathTypeConflict, Path: kp, Segment: part,
				Resolved: strings.Join(resolved, "."), Found: found,
			}
		}

		name, isField := kpath.SegmentFieldName(part)
		if !isField {
			// An index, a sparse index or a key. ir walks these already -- `o get
			// 'a.votes[1]'` has always worked -- and this loop only ever knew object
			// fields, so every read at an element of an array was refused as a bad
			// segment. What the loop adds over ir's walk is the error taxonomy the
			// session's codes rest on, so the step is delegated and the failure is
			// classified here (yy0cfe9mh12kr6pwgsn0).
			if p, perr := kpath.Parse(part); perr == nil && p != nil && p.Wild() {
				// A wildcard names a SET of values, and a read answers one. This is the
				// caller's path being wrong in a way no write can fix, which is what
				// PathBadSegment is for.
				return nil, &PathError{
					Kind: PathBadSegment, Path: kp, Segment: part,
					Resolved: strings.Join(resolved, "."),
				}
			}
			if !segmentSuitsContainer(part, current) {
				// The path addresses an element of a container this is not: an index
				// into an object, a field of an array. That is the same disagreement
				// PathTypeConflict reports for a field under a string, and it deserves
				// the same answer rather than "nothing there".
				found := ir.NullType
				if current != nil {
					found = current.Type
				}
				return nil, &PathError{
					Kind: PathTypeConflict, Path: kp, Segment: part,
					Resolved: strings.Join(resolved, "."), Found: found,
				}
			}
			next, err := current.GetKPath(part)
			if err != nil || next == nil {
				// The right kind of container, and no such element: an answer, not a
				// broken path.
				return nil, &PathError{
					Kind: PathAbsent, Path: kp, Segment: part,
					Resolved: strings.Join(resolved, "."),
				}
			}
			current = next
			resolved = append(resolved, part)
			continue
		}

		// Find the field matching this part
		found := false
		for i, field := range current.Fields {
			if field.String == name || unquoteFieldKey(field.String) == name {
				current = current.Values[i]
				found = true
				break
			}
		}
		if !found {
			return nil, &PathError{
				Kind: PathAbsent, Path: kp, Segment: name,
				Resolved: strings.Join(resolved, "."),
			}
		}
		resolved = append(resolved, name)
	}

	return current, nil
}

// watchAbsence tracks a watch that started with no value at its path, so the log
// can report the arrival rather than leaving the reader wondering whether the
// absence was ever resolved. Both halves fire at most once per watch.
//
// It is deliberately not a counter of occurrences: the interesting fact is the
// transition, and one line per event is what made this condition unreadable.
type watchAbsence struct {
	log     *slog.Logger
	path    string
	pending bool
}

// arm records that the watch initialized with no value at its path.
func (w *watchAbsence) arm() { w.pending = true }

// observe reports the arrival the first time the watch sees a real value. A null
// subtree is not an arrival: it is how an absent path is delivered.
func (w *watchAbsence) observe(sub *ir.Node) {
	if !w.pending || sub == nil || sub.Type == ir.NullType {
		return
	}
	w.pending = false
	if w.log != nil {
		w.log.Info("watched path now has a value", "path", w.path)
	}
}

// segmentSuitsContainer says whether a non-field segment addresses the KIND of container it
// is applied to: an index wants an array, a sparse index or a key wants what holds them.
// It exists to tell "this is not that kind of container" from "there is nothing there",
// which are different answers to a client (PathTypeConflict against PathAbsent).
func segmentSuitsContainer(seg string, n *ir.Node) bool {
	if n == nil {
		return false
	}
	p, err := kpath.Parse(seg)
	if err != nil || p == nil {
		return false
	}
	switch p.EntryKind() {
	case kpath.ArrayEntry:
		return n.Type == ir.ArrayType
	case kpath.SparseArrayEntry:
		// A sparse array is an object keyed by number, and an array answers a sparse
		// index too -- both are what the store may hold for one.
		return n.Type == ir.ObjectType || n.Type == ir.ArrayType
	default:
		// A keyed segment (key) addresses an element of a keyed array.
		return n.Type == ir.ArrayType
	}
}

// splitKPath splits a simple kpath into its parts.
func splitKPath(kp string) []string {
	return kpath.SplitAll(kp)
}

// filterState filters the state to match the given criteria and trims the result.
// It delegates to tony.FilterState so logd and docd (which applies the same
// projection over a composed read) stay identical.
func filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	return tony.FilterState(state, match)
}
