package tony

import (
	"strings"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// Reason names what a frame observed at its node.  It is the coarse
// classification a caller switches on; the nodes carry the detail.
type Reason int

const (
	// ReasonUnmatched is the default: the node did not match and the
	// specifics are in Causes, or the match walked no further.
	ReasonUnmatched Reason = iota
	// ReasonType: the document node is of a different kind than the pattern
	// asks for -- an object where a string was wanted.
	ReasonType
	// ReasonValue: same kind, different value.
	ReasonValue
	// ReasonLength: arrays of different lengths.  Array patterns match
	// element by element, so length is decided before any element is.
	ReasonLength
	// ReasonAbsent: the pattern requires a field the document does not have.
	// This is the one distinction a caller can most often repair by itself,
	// so it is kept apart from present-but-wrong.
	ReasonAbsent
	// ReasonOp: an operator rejected the node.  Op names it and Expected is
	// the whole tagged pattern node, so the alternatives of an !or or the
	// element pattern of an !all can be rendered from it.
	ReasonOp
	// ReasonError: the pattern is malformed at this node.  Err says how.  The
	// error is also returned from Match; the frame says where it came from.
	ReasonError
)

func (r Reason) String() string {
	switch r {
	case ReasonType:
		return "type"
	case ReasonValue:
		return "value"
	case ReasonLength:
		return "length"
	case ReasonAbsent:
		return "absent"
	case ReasonOp:
		return "op"
	case ReasonError:
		return "error"
	}
	return "unmatched"
}

// A Frame is one node of a match walk: the document node, the pattern which
// judged it, the verdict, and the frames beneath.
//
// Path is a kpath into the document Match was given -- "" for its root,
// "findings[7].severity" for a node an !all reached.  Expected and Found are
// the pattern and document nodes themselves, not rendered text: a caller
// renders them for whatever audience it has.  Found is a reference into the
// document and can be an arbitrarily large subtree; String bounds what it
// prints, and a caller printing the nodes itself should do the same.
//
// Frames are not stable across versions.  Which operator produced which frame
// is a fact about how the match walked, and matches walk differently as
// operators are added.
type Frame struct {
	Path     string
	Op       string // operator tag name without the '!', "" for plain structure
	Matched  bool
	Reason   Reason
	Expected *ir.Node
	Found    *ir.Node // nil when Reason is ReasonAbsent
	Err      error    // set when Reason is ReasonError
	Causes   []*Frame

	parent *Frame
	// synthetic marks a frame whose Found is not a node of the document:
	// !field matches a field name, !tag matches a tag as an object.  Such a
	// frame explains its operator, not a place in the document, so an
	// explanation stops at the operator above it.
	synthetic bool
}

// An Explanation is why a match came out the way it did.  Pass one to
// Explaining or Tracing:
//
//	var why tony.Explanation
//	ok, err := tony.Match(doc, pat, tony.Explaining(&why))
//	for _, f := range why.Failures {
//	    fmt.Println(f.Path, f.Reason, encode.MustString(f.Expected))
//	}
type Explanation struct {
	// Matched is the verdict Match returned.
	Matched bool

	// Root is the frame of the whole match, with Causes beneath it.  It is
	// nil when the match succeeded and no trace was asked for, since a
	// successful match records nothing.
	Root *Frame

	// Failures are the frames which carry the reasons, taken from the tree:
	// the most specific frame on each failing branch of the walk.  An
	// operator which failed for one reason (an !all whose seventh element
	// failed) is passed through to that reason; an operator which failed
	// because every alternative failed (an !or) is itself the reason, since
	// its alternatives are not defects to repair.
	Failures []*Frame

	// Matches are the operator frames which matched, in walk order.  Only
	// Tracing records them: which branch of an !or matched, which elements
	// an !all accepted.  Under Explaining it is empty.
	Matches []*Frame
}

// String renders the failures one per line, or the matched operators when the
// match succeeded under Tracing.  Nodes are truncated: this is for a log line
// or a prompt, and Found can be a whole document.
func (e *Explanation) String() string {
	frames, verb := e.Failures, "expected"
	if e.Matched {
		frames, verb = e.Matches, "matched"
	}
	var sb strings.Builder
	for _, f := range frames {
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		path := f.Path
		if path == "" {
			path = "."
		}
		sb.WriteString(path)
		sb.WriteString(": ")
		if f.Op != "" {
			sb.WriteString(f.Op)
			sb.WriteString(": ")
		}
		if f.Reason != ReasonUnmatched && f.Reason != ReasonOp {
			sb.WriteString(f.Reason.String())
			sb.WriteString(": ")
		}
		if f.Err != nil {
			sb.WriteString(f.Err.Error())
			continue
		}
		sb.WriteString(verb)
		sb.WriteByte(' ')
		sb.WriteString(brief(f.Expected))
		if f.Found != nil && f.Reason != ReasonAbsent {
			sb.WriteString(", found ")
			sb.WriteString(brief(f.Found))
		}
	}
	return sb.String()
}

// briefMax bounds a rendered node in String.  Explanations go into logs and
// model prompts, where a whole document costs more than it says.
const briefMax = 120

func brief(n *ir.Node) string {
	if n == nil {
		return "nothing"
	}
	buf := &strings.Builder{}
	if err := encode.Encode(n, buf, encode.EncodeWire(true)); err != nil {
		return "?"
	}
	s := strings.TrimSpace(buf.String())
	if len(s) > briefMax {
		s = s[:briefMax] + "..."
	}
	return s
}

// explainer collects frames as a match walks.  A nil *explainer is the
// ordinary match: no frames, no cost beyond a nil check per node.
type explainer struct {
	root  *ir.Node // the document Match was given, for relative paths
	trace bool     // keep the frames of sub-matches which succeeded
	top   *Frame   // the frame of the whole match
	cur   *Frame
	// lastPath is the path of the last node reached from root.  Operators
	// may match against nodes they synthesize -- !field matches a field
	// name, !tag matches a tag as an object -- and those have no place in
	// the document; they are reported at the node the operator was applied
	// to.
	lastPath string
}

// push opens the frame for a node about to be matched.
func (e *explainer) push(doc, pattern *ir.Node) *Frame {
	path, rooted := e.pathOf(doc)
	f := &Frame{
		Path:      path,
		Expected:  pattern,
		Found:     doc,
		parent:    e.cur,
		synthetic: !rooted,
	}
	if e.cur != nil {
		e.cur.Causes = append(e.cur.Causes, f)
	} else if e.top == nil {
		e.top = f
	}
	e.cur = f
	return f
}

// pop closes a frame with its verdict.  A frame which matched is dropped
// along with everything recorded beneath it -- that one rule is what keeps
// the rejected branches of a successful !or, and the sub-matches an !not
// needed to fail, out of an explanation of something else.
func (e *explainer) pop(f *Frame, matched bool, err error) {
	f.Matched = matched
	switch {
	case err != nil:
		f.Reason, f.Err = ReasonError, err
	case !matched && f.Op != "" && f.Reason == ReasonUnmatched:
		f.Reason = ReasonOp
	}
	e.cur = f.parent
	if matched && !e.trace && f.parent != nil {
		f.parent.Causes = dropFrame(f.parent.Causes, f)
	}
}

func dropFrame(causes []*Frame, f *Frame) []*Frame {
	// frames close innermost first, so f is the last one opened here
	if n := len(causes); n > 0 && causes[n-1] == f {
		return causes[:n-1]
	}
	for i, c := range causes {
		if c == f {
			return append(causes[:i], causes[i+1:]...)
		}
	}
	return causes
}

// op records which operator judged the current node.
func (e *explainer) op(tag string) {
	if e.cur != nil {
		e.cur.Op = tag
	}
}

// fail records why the current node did not match.
func (e *explainer) fail(r Reason) {
	if e.cur != nil {
		e.cur.Reason = r
	}
}

// absent records a field the pattern requires and the document does not have.
func (e *explainer) absent(doc *ir.Node, field string, pattern *ir.Node) {
	if e.cur == nil {
		return
	}
	path, _ := e.pathOf(doc)
	e.cur.Causes = append(e.cur.Causes, &Frame{
		Path:     kpathField(path, field),
		Reason:   ReasonAbsent,
		Expected: pattern,
		parent:   e.cur,
	})
}

// pathOf is the kpath of a node relative to the document the match started
// from, or, for a node the match synthesized, the path of the node it stands
// for.
func (e *explainer) pathOf(n *ir.Node) (path string, rooted bool) {
	for p := n; p != nil; p = p.Parent {
		if p == e.root {
			path = strings.TrimPrefix(n.KPath(), e.root.KPath())
			path = strings.TrimPrefix(path, ".")
			e.lastPath = path
			return path, true
		}
	}
	return e.lastPath, false
}

func kpathField(prefix, field string) string {
	if token.KPathQuoteField(field) {
		field = token.Quote(field, true)
	}
	if prefix == "" {
		return field
	}
	return prefix + "." + field
}

// finish fills in the caller's Explanation once the walk is done.
func (e *explainer) finish(why *Explanation, matched bool) {
	why.Matched = matched
	if matched && !e.trace {
		return // a successful match records nothing
	}
	why.Root = e.top
	if !matched {
		why.Failures = appendFailures(nil, e.top)
	}
	if e.trace {
		why.Matches = appendMatches(nil, e.top)
	}
}

// appendFailures takes the reasons out of the frame tree.  A frame which did
// not match is passed through when something beneath it says more: structure
// always (every failing field of an object is a defect of its own), an
// operator only when exactly one sub-match failed against a node of the
// document.  An operator with several failing sub-matches is where the
// reading stops -- its sub-matches may be alternatives, as an !or's are, and
// only the operator knows.  So is an operator which judged something it
// synthesized rather than a node of the document.  An operator nothing is
// known about therefore reports itself, which is coarse but never wrong.
func appendFailures(dst []*Frame, f *Frame) []*Frame {
	if f == nil || f.Matched {
		return dst
	}
	var failed []*Frame
	for _, c := range f.Causes {
		if !c.Matched {
			failed = append(failed, c)
		}
	}
	switch {
	case len(failed) == 0:
		return append(dst, f)
	case f.Op == "":
		for _, c := range failed {
			dst = appendFailures(dst, c)
		}
		return dst
	case len(failed) == 1 && !failed[0].synthetic && (f.Err == nil || failed[0].Err != nil):
		// an operator which errored on its own account, rather than passing
		// up the error of what it matched, is the one to report
		return appendFailures(dst, failed[0])
	default:
		return append(dst, f)
	}
}

// appendMatches collects the operator frames which matched, in walk order.
func appendMatches(dst []*Frame, f *Frame) []*Frame {
	if f == nil {
		return dst
	}
	if f.Matched && f.Op != "" {
		dst = append(dst, f)
	}
	for _, c := range f.Causes {
		dst = appendMatches(dst, c)
	}
	return dst
}
