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

// PathErrorKind says WHY a path did not resolve. The three reasons deserve three
// different reactions, and collapsing them into one sentinel is what made an
// ordinary not-written-yet path log at the same volume as a real fault.
//
// None of them indicates a storage problem. Extraction is pure navigation of a
// document a successful read already returned, so it cannot discover anything
// about the health of the store; that news arrives from the read itself.
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
		return fmt.Sprintf("cannot extract %q: segment %q is not an object field", e.Path, e.Segment)
	case PathTypeConflict:
		where := "the document root"
		if e.Resolved != "" {
			where = fmt.Sprintf("%q", e.Resolved)
		}
		return fmt.Sprintf("no value at %q: %s is %v, not an object", e.Path, where, e.Found)
	default:
		if e.Resolved == "" {
			return fmt.Sprintf("no value at %q: no field %q at the document root", e.Path, e.Segment)
		}
		return fmt.Sprintf("no value at %q: resolved through %q, no field %q", e.Path, e.Resolved, e.Segment)
	}
}

// Is makes every kind match ErrPathNotFound, so callers that only care whether a
// value exists keep working unchanged.
func (e *PathError) Is(target error) bool { return target == ErrPathNotFound }

// IsPathAbsent reports the ordinary case: the path has no value yet and will
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
	if doc == nil {
		return ir.Null(), nil
	}
	if kp == "" {
		return doc, nil
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
		if current == nil || current.Type != ir.ObjectType {
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
			return nil, &PathError{
				Kind: PathBadSegment, Path: kp, Segment: part,
				Resolved: strings.Join(resolved, "."),
			}
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

// splitKPath splits a simple kpath into its parts.
// For now, only handles simple dot-separated field paths like "users.posts".
// TODO: handle array indices and sparse indices.
func splitKPath(kp string) []string {
	return kpath.SplitAll(kp)
}

// filterState filters the state to match the given criteria and trims the result.
// It delegates to tony.FilterState so logd and docd (which applies the same
// projection over a composed read) stay identical.
func filterState(state *ir.Node, match *ir.Node) (*ir.Node, error) {
	return tony.FilterState(state, match)
}
