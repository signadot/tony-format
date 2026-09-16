package storage

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/index"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/snap"
)

// Listing: the direct children of a path at a commit, at a cost proportional to the page
// (3kgxprskh12krjrmndn0).
//
// A read at a path seeks the nearest snapshot at or above it and folds the writes since.
// A listing does the same, one level deep: the snapshot's DIRECTORY has a table of the
// container's children (snap/directory.go), read from where the listing left off, and
// each write since is decoded once and applied to the names at that one level -- a plain
// object adds its keys and removes its !deleted ones, a total cover replaces the names
// with the operand's, a scalar leaves none. A scope's statements go last, as they do in a
// read. What the fold holds is the tail's changes to the names, bounded by the snapshot
// policy as the value fold is; the table is never held.
//
// Where the fold cannot say -- a write above the path states it whole, a !raw, a snapshot
// from before the directory -- the listing streams the value at the path and takes the
// names as they pass: O(bytes) in time and O(1) in memory, the read without Collect.

// ChildKind is what kind of value a child is, in the terms a listing says it: which
// wildcard lists under it, or none.
type ChildKind uint8

const (
	ChildObject ChildKind = iota
	ChildSparseArray
	ChildArray
	ChildString
	ChildNumber
	ChildBool
	ChildNull
)

func (k ChildKind) String() string {
	switch k {
	case ChildObject:
		return "object"
	case ChildSparseArray:
		return "sparse array"
	case ChildArray:
		return "array"
	case ChildString:
		return "string"
	case ChildNumber:
		return "number"
	case ChildBool:
		return "bool"
	case ChildNull:
		return "null"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// IsContainer says the child has children of its own.
func (k ChildKind) IsContainer() bool {
	return k == ChildObject || k == ChildSparseArray || k == ChildArray
}

// Child is one direct child of a path: its segment as the store spells it -- a field
// (quoted as kpath quotes it), [i], {n} -- and its kind.
type Child struct {
	Segment string
	Kind    ChildKind
}

// Children lists the direct children of kp as of commit at, in the view scopeID names,
// in the store's order, starting after the segment `after` ("" for the first), calling
// fn for each until it answers false. A path that holds nothing, or a leaf, lists nothing.
func (s *Storage) Children(at int64, scopeID *string, kp, after string, fn func(Child) bool) error {
	if _, err := kpath.Parse(kp); err != nil {
		return fmt.Errorf("children of %q: %w", kp, err)
	}
	if at == 0 {
		return nil
	}
	if kp != "" && s.provenAbsent(kp) && (scopeID == nil || !s.index.Footprint().Reaches(*scopeID, kp)) {
		return nil
	}
	started := time.Now()

	// The base: the nearest snapshot at or above kp, and its table for kp.
	var base *snap.Table
	var baseKind ChildKind
	startCommit := int64(0)
	if seg, ok := s.index.SnapshotAtOrAbove(kp, at); ok {
		startCommit = seg.StartCommit + 1
		tb, kind, release, err := s.snapshotTable(seg, kp)
		if errors.Is(err, snap.ErrNoDirectory) {
			// From before there was one: the value fold, taking names.
			return s.childrenByStreaming(at, scopeID, kp, after, fn)
		}
		if err != nil {
			return err
		}
		if release != nil {
			defer release()
		}
		base, baseKind = tb, kind // nil when the snapshot holds nothing at kp
	}

	// The tail, one level deep.
	f := &nameFold{kinds: map[string]ChildKind{}, removed: map[string]bool{}, kindAt: baseKind, hasBase: base != nil}
	project := func(seg index.LogSegment) error {
		entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
		if err != nil {
			return fmt.Errorf("failed to read patch entry: %w", err)
		}
		if entry.Patch == nil {
			return nil
		}
		node, _, ok := projectAt(entry.Patch, kp)
		if !ok {
			return errBlocked
		}
		return f.apply(node)
	}
	var err error
	for seg := range s.index.Segments(kp, &startCommit, &at, nil) {
		if seg.StartCommit == seg.EndCommit {
			continue // a snapshot
		}
		if err = project(seg); err != nil {
			break
		}
	}
	if err == nil && scopeID != nil {
		err = s.projectScope(at, *scopeID, kp, project)
	}
	if errors.Is(err, errBlocked) || errors.Is(err, errNamesUnsaid) {
		return s.childrenByStreaming(at, scopeID, kp, after, fn)
	}
	if err != nil {
		return err
	}
	s.readStats.note(ReadNarrow, kp, time.Since(started))
	s.readStats.listTable.Add(1)
	return f.emit(base, after, fn)
}

// errNamesUnsaid is a write the fold cannot apply at one level -- a !raw, whose contents
// merge as data with their own tags -- so the listing is answered by streaming.
var errNamesUnsaid = errors.New("names not said at one level")

// nameFold is the tail's changes to the names at one level.
type nameFold struct {
	total   bool                 // the tail stated the whole value: the base's names are gone
	leaf    bool                 // the value is a scalar, or gone: no children
	kinds   map[string]ChildKind // names the tail added or restated, with their kind
	removed map[string]bool      // names the tail deleted
	kindAt  ChildKind            // the kind of the value at kp, as far as the fold knows
	hasBase bool
}

// apply folds one write's projection at kp into the names.
func (f *nameFold) apply(node *ir.Node) error {
	if node == nil {
		return nil // says nothing about kp
	}
	node = ir.Uncomment(node)
	if node == nil {
		return nil
	}
	switch opOf(node) {
	case "":
	case "insert", "replace":
		f.replace()
		return f.applyValue(node)
	case "delete":
		f.replace()
		f.leaf = true
		return nil
	default:
		return errNamesUnsaid
	}
	// A plain value merges where the kinds agree, and replaces otherwise: an object over
	// an array is the object.
	kind := kindOfNode(node)
	if !kind.IsContainer() {
		f.replace()
		f.leaf = true
		return nil
	}
	if f.leaf || (f.hasBase || f.total) && !sameFamily(kind, f.kindAt) {
		f.replace()
	}
	if kind == ChildArray {
		// An array is positional and is stated whole.
		f.replace()
	}
	return f.applyValue(node)
}

// replace forgets every name so far: the base's and the tail's.
func (f *nameFold) replace() {
	f.total = true
	f.leaf = false
	clear(f.kinds)
	clear(f.removed)
}

// applyValue folds a container's children in: a !delete removes the name, anything else
// adds it with its kind. A scalar has no children.
func (f *nameFold) applyValue(node *ir.Node) error {
	kind := kindOfNode(node)
	f.kindAt = kind
	if !kind.IsContainer() {
		f.leaf = true
		return nil
	}
	f.leaf = false
	switch node.Type {
	case ir.ObjectType:
		for i, field := range node.Fields {
			if i >= len(node.Values) {
				break
			}
			seg := kpath.Field(field.String).String()
			if kind == ChildSparseArray {
				if field.Int64 == nil {
					continue
				}
				seg = kpath.SparseIndex(int(*field.Int64)).String()
			}
			v := ir.Uncomment(node.Values[i])
			if v != nil && opOf(v) == "delete" {
				delete(f.kinds, seg)
				f.removed[seg] = true
				continue
			}
			delete(f.removed, seg)
			f.kinds[seg] = kindOfNode(v)
		}
	case ir.ArrayType:
		for i, v := range node.Values {
			f.kinds[kpath.Index(i).String()] = kindOfNode(ir.Uncomment(v))
		}
	}
	return nil
}

// emit merges the base's table with the tail's names, in order, from after.
func (f *nameFold) emit(base *snap.Table, after string, fn func(Child) bool) error {
	if f.leaf {
		return nil
	}
	added := make([]string, 0, len(f.kinds))
	for seg := range f.kinds {
		if after == "" || segmentCompare(seg, after) > 0 {
			added = append(added, seg)
		}
	}
	slices.SortFunc(added, segmentCompare)
	if base == nil || f.total {
		for _, seg := range added {
			if !fn(Child{Segment: seg, Kind: f.kinds[seg]}) {
				return nil
			}
		}
		return nil
	}
	if err := base.Seek(after); err != nil {
		return err
	}
	i := 0
	for {
		e, ok, err := base.Next()
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		// The tail's names that sort before this one.
		for i < len(added) && segmentCompare(added[i], e.Segment) < 0 {
			if !fn(Child{Segment: added[i], Kind: f.kinds[added[i]]}) {
				return nil
			}
			i++
		}
		if f.removed[e.Segment] {
			continue
		}
		kind := childKindOf(e.Kind)
		if k, restated := f.kinds[e.Segment]; restated {
			// A container merged into stays what it was, an object into a sparse
			// array included; anything else is what the tail made it.
			if !(k == ChildObject && sameFamily(k, kind)) {
				kind = k
			}
			if i < len(added) && added[i] == e.Segment {
				i++
			}
		}
		if !fn(Child{Segment: e.Segment, Kind: kind}) {
			return nil
		}
	}
	for ; i < len(added); i++ {
		if !fn(Child{Segment: added[i], Kind: f.kinds[added[i]]}) {
			return nil
		}
	}
	return nil
}

// snapshotTable opens the table for kp in the snapshot seg names, and the kind of the
// value there: nil and no error when the snapshot holds nothing at kp, or a leaf. release
// closes what the table reads from, and is nil when there is nothing to close.
func (s *Storage) snapshotTable(seg index.LogSegment, kp string) (tb *snap.Table, kind ChildKind, release func(), err error) {
	entry, err := s.dLog.ReadEntryAt(dlog.LogFileID(seg.LogFile), seg.LogPosition, seg.LogFileGeneration)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to read snapshot entry: %w", err)
	}
	if entry.SnapPos == nil {
		return nil, 0, nil, fmt.Errorf("snapshot entry missing SnapPos")
	}
	r, err := s.dLog.OpenReaderAt(dlog.LogFileID(seg.LogFile), *entry.SnapPos, seg.LogFileGeneration)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to open snapshot reader: %w", err)
	}
	snapshot, err := snap.Open(r)
	if err != nil {
		r.Close()
		return nil, 0, nil, fmt.Errorf("failed to open snapshot: %w", err)
	}
	tb, k, err := snapshot.TableOf(pathWithin(kp, seg.KindedPath))
	switch {
	case errors.Is(err, snap.ErrNotFound), errors.Is(err, snap.ErrNotAContainer):
		snapshot.Close()
		return nil, 0, nil, nil
	case err != nil:
		snapshot.Close()
		return nil, 0, nil, err
	}
	return tb, childKindOf(k), func() { snapshot.Close() }, nil
}

// childrenByStreaming lists by reading the value at kp and taking the names as they pass:
// the fallback where the fold cannot say, and the read without Collect.
func (s *Storage) childrenByStreaming(at int64, scopeID *string, kp, after string, fn func(Child) bool) error {
	s.readStats.listStream.Add(1)
	c, err := s.Read(at, scopeID, kp)
	if err != nil {
		return err
	}
	defer c.Close()
	if c.Presence() != Present {
		return nil
	}
	depth := 0
	var kind ChildKind // the value's own kind: what names its children
	var pending *Child
	var next string
	n := 0
	emit := func() bool {
		if pending == nil {
			return true
		}
		ch := *pending
		pending = nil
		if after != "" && segmentCompare(ch.Segment, after) <= 0 {
			return true
		}
		return fn(ch)
	}
	for {
		ev, err := c.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch ev.Type {
		case stream.EventHeadComment, stream.EventLineComment:
			continue
		case stream.EventKey:
			if depth == 1 {
				next = kpath.Field(ev.Key).String()
			}
			continue
		case stream.EventIntKey:
			if depth == 1 {
				next = kpath.SparseIndex(int(ev.IntKey)).String()
				kind = ChildSparseArray
			} else if depth == 2 && pending != nil && pending.Kind == ChildObject {
				pending.Kind = ChildSparseArray
			}
			continue
		}
		if depth == 0 {
			// The value itself.
			switch ev.Type {
			case stream.EventBeginObject:
				kind = ChildObject
			case stream.EventBeginArray:
				kind = ChildArray
			default:
				return nil // a scalar: no children
			}
			depth = 1
			continue
		}
		if depth == 1 && ev.IsValueStart() {
			if !emit() {
				return nil
			}
			seg := next
			if kind == ChildArray {
				seg = kpath.Index(n).String()
			}
			n++
			pending = &Child{Segment: seg, Kind: kindOfEvent(ev)}
		}
		switch ev.Type {
		case stream.EventBeginObject, stream.EventBeginArray:
			depth++
		case stream.EventEndObject, stream.EventEndArray:
			depth--
			if depth == 0 {
				emit()
				return nil
			}
		}
	}
}

// sameFamily says two kinds merge rather than replace: an object and a sparse array are
// both objects to a merge.
func sameFamily(a, b ChildKind) bool {
	if a == b {
		return true
	}
	return (a == ChildObject || a == ChildSparseArray) && (b == ChildObject || b == ChildSparseArray)
}

func kindOfNode(n *ir.Node) ChildKind {
	n = ir.Uncomment(n)
	if n == nil {
		return ChildNull
	}
	switch n.Type {
	case ir.ObjectType:
		if len(n.Fields) > 0 && n.Fields[0].Type == ir.NumberType {
			return ChildSparseArray
		}
		return ChildObject
	case ir.ArrayType:
		return ChildArray
	case ir.StringType:
		return ChildString
	case ir.NumberType:
		return ChildNumber
	case ir.BoolType:
		return ChildBool
	}
	return ChildNull
}

func kindOfEvent(ev *stream.Event) ChildKind {
	switch ev.Type {
	case stream.EventBeginObject:
		return ChildObject
	case stream.EventBeginArray:
		return ChildArray
	case stream.EventString:
		return ChildString
	case stream.EventInt, stream.EventFloat:
		return ChildNumber
	case stream.EventBool:
		return ChildBool
	}
	return ChildNull
}

func childKindOf(k snap.Kind) ChildKind {
	switch k {
	case snap.KindObject:
		return ChildObject
	case snap.KindSparseArray:
		return ChildSparseArray
	case snap.KindArray:
		return ChildArray
	case snap.KindString:
		return ChildString
	case snap.KindNumber:
		return ChildNumber
	case snap.KindBool:
		return ChildBool
	}
	return ChildNull
}

// segmentCompare orders two segments as the store orders keys: as kpath compares them,
// which is the order the tables are searched in.
func segmentCompare(a, b string) int {
	ka, errA := kpath.Parse(a)
	kb, errB := kpath.Parse(b)
	if errA != nil || errB != nil || ka == nil || kb == nil {
		switch {
		case a < b:
			return -1
		case a > b:
			return 1
		}
		return 0
	}
	return ka.Compare(kb)
}
