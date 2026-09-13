package storage

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"time"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/patches"
	"github.com/signadot/tony-format/go-tony/token"
)

// A precondition is evaluated against what its pattern names, and nothing more.
//
// It was evaluated against the whole value at its path, so a write whose precondition
// asked only that the parent be an object or nothing, or that one named child be absent
// -- the two questions a client asks before writing a whole entity -- cost a read of
// every child of the parent: 80ms at 3,000 children for a write that touched one of
// them (mgn9szemh12krh7zmsn0). The pattern says which parts of the value its answer
// depends on: a shape test needs the value's kind, a field pattern needs the named
// fields and nothing beside them, and only a pattern over the value as a whole -- an
// array, an operator this file does not know -- needs all of it. A read answers a
// path's kind from its first event and a never-written child from the index without
// opening the log (Read), so both questions are answered without building the value.
//
// What is built is a node the matcher answers the same for as it would the whole value:
// the kind, the tag, the named fields present, each materialized by its own need. The
// matcher's rule that a field pattern names fields the value must have, and says nothing
// of the rest, is what makes the fields it does not name safe to leave out.

// need says how much of the value at a path a pattern's answer depends on.
type need int

const (
	// needWhole: the value, collected. The pattern's answer depends on all of it, or on
	// something this file does not analyze.
	needWhole need = iota
	// needKind: the value's kind and tag, and a scalar's value. No child is asked about.
	needKind
	// needFields: the kind, and the named fields, each by its own need.
	needFields
)

type needs struct {
	kind   need
	fields map[string]*needs
}

var whole = &needs{kind: needWhole}

// needsOf analyzes a pattern, in the store's form (tx.LowerMatches).
func needsOf(p *ir.Node) *needs {
	if p == nil {
		return whole
	}
	if p.Type == ir.CommentType {
		if len(p.Values) == 1 {
			return needsOf(p.Values[0])
		}
		return whole
	}
	tag := ir.StripPresentation(p.Tag)
	if tag == "" {
		switch p.Type {
		case ir.ObjectType:
			if len(p.Fields) == 0 {
				return &needs{kind: needKind} // any object, and nothing else
			}
			fields := make(map[string]*needs, len(p.Fields))
			for i, f := range p.Fields {
				if f.Type != ir.StringType {
					return whole // an int key, or a merge key: not a field by name
				}
				fields[f.String] = mergeNeeds(fields[f.String], needsOf(p.Values[i]))
			}
			return &needs{kind: needFields, fields: fields}
		case ir.ArrayType:
			return whole
		default:
			// A scalar pattern needs a scalar's value and no more than a container's
			// kind, which is what needKind materializes.
			return &needs{kind: needKind}
		}
	}
	head, _, rest := ir.TagArgs(tag)
	switch head {
	case "!irtype":
		return &needs{kind: needKind}
	case "!not":
		inner := *p // the pattern under the operator, shallow: read only
		inner.Tag = rest
		return needsOf(&inner)
	case "!or", "!and":
		if p.Type != ir.ArrayType {
			return whole
		}
		acc := &needs{kind: needKind}
		for _, alt := range p.Values {
			acc = mergeNeeds(acc, needsOf(alt))
		}
		return acc
	default:
		return whole
	}
}

// mergeNeeds is what two patterns over one value need together.
func mergeNeeds(a, b *needs) *needs {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.kind == needWhole || b.kind == needWhole {
		return whole
	}
	if a.kind == needKind && b.kind == needKind {
		return a
	}
	out := &needs{kind: needFields, fields: make(map[string]*needs, len(a.fields)+len(b.fields))}
	for k, v := range a.fields {
		out.fields[k] = v
	}
	for k, v := range b.fields {
		out.fields[k] = mergeNeeds(out.fields[k], v)
	}
	return out
}

// stateFor answers a node the matcher answers pattern the same for as it would the whole
// value at kp: what the pattern names, read under the write budget. Nil is absent.
func (s *Storage) stateFor(commit int64, scopeID *string, kp string, pattern *ir.Node) (*ir.Node, error) {
	return s.materialize(commit, scopeID, kp, needsOf(pattern))
}

func (s *Storage) materialize(commit int64, scopeID *string, kp string, n *needs) (*ir.Node, error) {
	if n.kind == needWhole {
		return s.stateAt(commit, scopeID, kp)
	}
	shape, err := s.kindAt(commit, scopeID, kp)
	if err != nil {
		return nil, err
	}
	if shape == nil {
		return nil, nil
	}
	switch shape.Type {
	case ir.ObjectType:
		node := &ir.Node{Type: ir.ObjectType, Tag: shape.Tag}
		if n.kind != needFields {
			return node, nil
		}
		names := make([]string, 0, len(n.fields))
		for name := range n.fields {
			names = append(names, name)
		}
		sort.Strings(names)
		var kvs []ir.KeyVal
		for _, name := range names {
			child, err := s.materialize(commit, scopeID, fieldPath(kp, name), n.fields[name])
			if err != nil {
				return nil, err
			}
			if child == nil {
				continue // the field is not there, and the node says so by not having it
			}
			kvs = append(kvs, ir.KeyVal{Key: ir.FromString(name), Val: child})
		}
		if len(kvs) == 0 {
			return node, nil
		}
		return ir.FromKeyValsAt(node, kvs), nil
	case ir.ArrayType:
		// A field pattern names no element and a shape test needs no more: an array
		// of no elements answers both as the whole one would.
		return &ir.Node{Type: ir.ArrayType, Tag: shape.Tag}, nil
	default:
		return shape, nil // a scalar, or a null: the value
	}
}

// errKindUnknown says the index's account of a path does not decide its kind: the
// cursor read decides it.
var errKindUnknown = errors.New("kind not decided from the index")

// kindAt answers the kind of the value at kp as of commit, in the view scopeID names:
// nil for absent, and otherwise a node of the kind and tag there, holding a scalar's
// value and none of a container's children.
//
// A read at kp folds every write under kp before its first event comes out -- the
// fold is what a read is -- so the kind of a path with three thousand children under
// it cost three thousand entries decoded, and a precondition asking only whether the
// path holds an object paid that on every write. The index says it without the
// entries: the base snapshot's first event gives the kind the fold starts from;
// statements at or above kp, which are the few writes that state what stands there,
// are decoded and applied; and a write strictly below kp is not decoded, since all it
// can do to kp's kind is make a container of what was not one. A write the index
// cannot classify that way, or an operator the projection cannot see through, hands
// the question to the read (kindByRead), which decides it as it always did.
func (s *Storage) kindAt(commit int64, scopeID *string, kp string) (*ir.Node, error) {
	shape, err := s.kindFromIndex(commit, scopeID, kp)
	if errors.Is(err, errKindUnknown) {
		return s.kindByRead(commit, scopeID, kp)
	}
	return shape, err
}

func (s *Storage) kindFromIndex(commit int64, scopeID *string, kp string) (*ir.Node, error) {
	if kp != "" && s.provenAbsent(kp) && (scopeID == nil || !s.index.Footprint().Reaches(*scopeID, kp)) {
		return nil, nil
	}
	started := time.Now()
	base, startCommit, _, err := s.findSubtreeBaseReader(commit, kp)
	if err != nil {
		return nil, err
	}
	state, err := firstValue(base)
	base.Close()
	if err != nil {
		return nil, err
	}

	// apply folds one statement into the kind: a write at or above kp is what it
	// states at kp, applied; a write below kp is a container where there was none.
	apply := func(seg index.LogSegment) error {
		below := seg.KindedPath != kp
		if !below && seg.Spine {
			below = true // indexed at kp because it passed through: the write is under kp
		}
		if below && len(seg.KindedPath) < len(kp) {
			below = false // an ancestor's own statement, which the iterator yields only for writes
		}
		if below {
			switch {
			case state == nil, state.Type != ir.ObjectType && state.Type != ir.ArrayType:
				state = &ir.Node{Type: ir.ObjectType}
			case state.Type == ir.ArrayType:
				return errKindUnknown // a positional write keeps it; a field write replaces it
			}
			return nil
		}
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			return nil
		}
		at, _, ok := projectAt(entry.Patch, kp)
		if !ok {
			return errKindUnknown
		}
		if at == nil {
			return nil
		}
		base := state
		if base == nil {
			base = ir.Null()
		}
		next, err := tony.Patch(base, at)
		if err != nil {
			return errKindUnknown
		}
		state = next
		return nil
	}
	for seg := range s.index.Segments(kp, &startCommit, &commit, nil) {
		if seg.StartCommit == seg.EndCommit {
			continue // a snapshot
		}
		if err := apply(seg); err != nil {
			return nil, err
		}
	}
	if scopeID != nil {
		if err := s.projectScope(commit, *scopeID, kp, apply); err != nil {
			return nil, err
		}
	}
	s.readStats.note(ReadNarrow, kp, time.Since(started))
	if state == nil {
		return nil, nil
	}
	switch state.Type {
	case ir.ObjectType, ir.ArrayType:
		return &ir.Node{Type: state.Type, Tag: state.Tag}, nil
	}
	return state, nil
}

// firstValue is the value a base reader's first event names: nil for no events, an
// empty container of the kind and tag, or the scalar.
func firstValue(r patches.EventReadCloser) (*ir.Node, error) {
	for {
		ev, err := r.ReadEvent()
		if err == io.EOF {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch ev.Type {
		case stream.EventHeadComment, stream.EventLineComment:
			continue
		case stream.EventBeginObject:
			return &ir.Node{Type: ir.ObjectType, Tag: ev.Tag}, nil
		case stream.EventBeginArray:
			return &ir.Node{Type: ir.ArrayType, Tag: ev.Tag}, nil
		}
		return stream.EventsToNode([]stream.Event{*ev})
	}
}

// kindByRead answers kindAt by the read: the first event of a cursor at kp.
func (s *Storage) kindByRead(commit int64, scopeID *string, kp string) (*ir.Node, error) {
	cur, err := s.Read(commit, scopeID, kp)
	if err != nil {
		return nil, err
	}
	defer cur.Close()
	if cur.Presence() == Absent {
		return nil, nil
	}
	return firstValue(cursorReader{cur})
}

// cursorReader reads a Cursor as an EventReadCloser.
type cursorReader struct{ c Cursor }

func (r cursorReader) ReadEvent() (*stream.Event, error) { return r.c.Next() }
func (r cursorReader) Close() error                      { return r.c.Close() }

// fieldPath is the path of field name under kp, the name quoted as a segment when it
// has to be.
func fieldPath(kp, name string) string {
	seg := name
	if token.KPathQuoteField(name) {
		seg = token.Quote(name, true)
	}
	return kpath.Join(kp, seg)
}
