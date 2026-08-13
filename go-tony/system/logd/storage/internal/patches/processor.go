package patches

import (
	"cmp"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/ir/kpath"
	"github.com/signadot/tony-format/go-tony/stream"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/tx"
)

// StreamingProcessor applies patches to streaming events without materializing
// the full document. Only subtrees that need patching are materialized.
type StreamingProcessor struct{}

// NewStreamingProcessor creates a new streaming patch processor.
func NewStreamingProcessor() *StreamingProcessor {
	return &StreamingProcessor{}
}

// ApplyPatches applies patches to base events, writing results to sink.
// Patches are applied in order for each patched path.
func (sp *StreamingProcessor) ApplyPatches(baseEvents stream.EventReader, patches []*ir.Node, sink stream.EventWriter) error {
	// Build patch value index: path → ordered patch nodes
	patchValues, err := buildPatchValueIndex(patches)
	if err != nil {
		return fmt.Errorf("failed to build patch index: %w", err)
	}

	// Create collector for detecting patched subtrees
	// We use a minimal index that just tracks which paths have patches
	patchIndex := NewPatchIndex()
	for path := range patchValues {
		// Add a non-nil empty slice so HasPatches returns true
		patchIndex.byPath[path] = make([]*dlog.Entry, 0)
	}
	collector := NewSubtreeCollector(patchIndex)

	// A patch can only be applied where the base stream passes through its root path.
	// A write that CREATES structure has no such path — the base has never heard of it —
	// so without this the patch is dropped, and since createSnapshot runs through here
	// too, the next snapshot is built from the same dropped patch and the subtree is gone
	// from the snapshot chain for good. unreached tracks those and grafts them on at the
	// deepest point of the base that does exist.
	unreached := newUnreachedPatches(patchValues)

	// A key whose value is about to be patched is held rather than written: the patch may
	// resolve the value to nothing, and then the key must not appear either. The collector
	// deliberately leaves the key to us (see SubtreeCollector.ProcessEvent), and by the
	// time the patched value is known the key would already be in the sink.
	var heldKey *stream.Event

	hasBaseEvents := false
	for {
		ev, err := baseEvents.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		hasBaseEvents = true

		// Process event through collector to detect patched paths
		collected, err := collector.ProcessEvent(ev)
		if err != nil {
			return err
		}

		// If we collected a complete subtree, apply patches and emit
		if collected != nil {
			patchedNode, err := applyPatchesToNode(collected.Node, patchValues[collected.Path])
			if err != nil {
				return err
			}
			unreached.reached(collected.Path)

			// The patches deleted this subtree. Absent is written by writing nothing —
			// which is what every layer above already reads as null state — so drop the
			// key that would have introduced it. At the document root there is no key,
			// and an array element has none either: dropping it removes the element and
			// shifts the rest, which is what deleting an array element means.
			if patchedNode == nil {
				heldKey = nil
				continue
			}

			if heldKey != nil {
				if err := sink.WriteEvent(heldKey); err != nil {
					return err
				}
				heldKey = nil
			}

			// Strip internal tags before emitting
			tx.StripPatchRootTag(patchedNode)

			// Emit patched subtree as events
			if err := emitNode(patchedNode, sink); err != nil {
				return err
			}
			continue
		}

		// If collector is actively collecting, skip emitting base events
		if collector.IsCollecting() {
			continue
		}

		// Track the base's shape so a patch whose path the base never reaches can still
		// be grafted on, and emit any graft that belongs at this point in the stream.
		handled, err := unreached.observe(ev, sink)
		if err != nil {
			return err
		}
		if handled {
			continue // the event was replaced by a graft (a scalar becoming a subtree)
		}

		// This key's value is about to be collected and patched: hold it until we know
		// whether there is a value to write.
		if collector.PendingPath() != "" {
			held := *ev
			heldKey = &held
			continue
		}

		// Not actively collecting, pass through
		if err := sink.WriteEvent(ev); err != nil {
			return err
		}
	}

	if heldKey != nil {
		return fmt.Errorf("base document ended after key %q with no value", heldKey.Key)
	}

	if hasBaseEvents && !unreached.empty() {
		return fmt.Errorf("patch paths not reachable in the base document: %v", unreached.paths())
	}

	// Handle empty base with patches: apply all patches in order starting from null
	// This matches InMemoryApplier behavior - when there's no base, we must materialize
	if !hasBaseEvents && len(patches) > 0 {
		result := ir.Null()
		for _, patch := range patches {
			if patch == nil {
				continue
			}
			// A delete leaves nil, and tony.Patch panics if handed one as the document.
			// Absent is null here, so a delete followed by a write rebuilds from null.
			if result == nil {
				result = ir.Null()
			}
			next, err := tony.Patch(result, patch)
			if err != nil {
				return err
			}
			result = next
		}
		if result == nil {
			return nil // everything folded away: no events is the null state
		}
		tx.StripPatchRootTagRecursive(result)
		if err := emitNode(result, sink); err != nil {
			return err
		}
	}

	return nil
}

// buildPatchValueIndex builds a map from path to the ordered nodes to apply there.
//
// Patch roots come from the entries' !logd-patch-root tags, at whatever depth the
// client wrote them. Roots that are dominated — an ancestor path also carries a root,
// in this entry or any other in the range — are NOT dropped. The subtree at the
// dominating path is collected and patched as a unit, so a dropped descendant root is a
// write that silently disappears: with a snapshot in the base, an ancestor write erased
// every descendant write made since that snapshot.
//
// Instead, each entry is navigated to the dominating path and contributes whatever it
// holds there, in commit order. docs/streaming_patch_processor.md describes this as
// applying the patches sequentially at the dominating path, which is what the worked
// example in that doc does; only the implementation diverged.
//
// Every path here is parsed exactly once, into `parsed`, and every comparison after
// that is between parsed forms. The paths arrive as strings whose structure the walk
// below already knows, and each one is compared many times — against every other root
// for dominance, and once per entry for containment. Re-parsing at each comparison put
// kpath.parseKFrag at half the CPU of a server replaying a long delta log, with mallocgc
// under it (issue ps8kfs9dh12kr777fnn0).
func buildPatchValueIndex(patches []*ir.Node) (map[string][]*ir.Node, error) {
	rootPaths := make(map[string]bool)
	entryRoots := make([]map[string]*ir.Node, len(patches))
	for i, patch := range patches {
		if patch == nil {
			continue
		}
		roots := map[string]*ir.Node{}
		walkAndCollectPatchRoots(patch, "", func(node *ir.Node, path string) {
			roots[path] = node
			rootPaths[path] = true
		})
		entryRoots[i] = roots
	}
	if len(rootPaths) == 0 {
		return map[string][]*ir.Node{}, nil
	}

	parsed := make(map[string]*kpath.KPath, len(rootPaths))
	for path := range rootPaths {
		if path == "" {
			parsed[path] = nil // the document root, which a nil KPath denotes
			continue
		}
		kp, err := kpath.Parse(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse path %q: %w", path, err)
		}
		parsed[path] = kp
	}

	applyPaths, dominator := maximalPaths(parsed)
	isApplyPath := make(map[string]bool, len(applyPaths))
	for _, path := range applyPaths {
		isApplyPath[path] = true
	}

	// Commit order is the order of `patches`, so the outer loop must be over entries.
	// Within one entry the order is immaterial: an entry contributes at most one node
	// per apply path, so no apply path sees two of its writes out of order.
	result := make(map[string][]*ir.Node, len(applyPaths))
	folded := make(map[string]bool, len(applyPaths))
	for i, patch := range patches {
		if patch == nil {
			continue
		}
		clear(folded)
		for rootPath, node := range entryRoots[i] {
			// A root exactly at an apply path contributes that node, as it always has.
			// Re-deriving it by navigating from the entry root would be equivalent only
			// for plain field paths — GetKPath does not resolve a sparse-index segment
			// ("items{1}") back to the node the walk found, so navigating unconditionally
			// silently dropped sparse-array patches.
			if isApplyPath[rootPath] {
				result[rootPath] = append(result[rootPath], node)
				continue
			}
			// Otherwise this root was dominated: navigate to the applied path so the
			// write is folded in rather than discarded. Several dominated roots in one
			// entry fold to the same subtree, which is contributed once.
			path, ok := dominator[rootPath]
			if !ok || folded[path] {
				continue
			}
			folded[path] = true
			if sub, ok := subtreeAt(patch, path); ok {
				result[path] = append(result[path], sub)
			}
		}
	}
	return result, nil
}

// maximalPaths returns the paths with no strict ancestor in the set — the paths at
// which subtrees will be collected and patched — sorted for determinism, along with
// the apply path that dominates each of the remaining paths. It takes the paths
// already parsed (a nil KPath being the document root).
//
// A path is dominated by at most one apply path: the ancestors of a path form a
// chain, so two apply paths dominating it would be comparable, and an apply path
// with a strict ancestor in the set is not maximal.
//
// Both answers come out of one pass over the paths in segment order, in which an
// ancestor immediately precedes its descendants (see comparePaths). Whatever lies
// between a maximal path and a later path it dominates is a descendant of it too,
// and so was skipped — which is why the last maximal path seen is the only candidate
// each path has to test against. Comparing every pair instead is quadratic in the
// number of distinct paths a delta-log range touches, which for a long range is the
// whole cost of reading (issue ps8kfs9dh12kr777fnn0).
func maximalPaths(parsed map[string]*kpath.KPath) ([]string, map[string]string) {
	ordered := make([]string, 0, len(parsed))
	for path := range parsed {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return comparePaths(parsed[ordered[i]], parsed[ordered[j]]) < 0
	})

	out := make([]string, 0, len(ordered))
	dominator := make(map[string]string)
	var maxPath string
	var maxKP *kpath.KPath
	haveMax := false
	for _, path := range ordered {
		kp := parsed[path]
		if haveMax {
			if anc, eq := maxKP.AncestorOrEqual(kp); anc && !eq {
				dominator[path] = maxPath
				continue
			}
		}
		out = append(out, path)
		maxPath, maxKP, haveMax = path, kp, true
	}
	sort.Strings(out)
	return out, dominator
}

// comparePaths orders paths lexicographically by segment. The order this produces is
// what maximalPaths relies on: a path sorts immediately before its descendants, and
// anything sorting between a path and one of its descendants is a descendant too.
// Ordering the path STRINGS would not do — '-' sorts below the '.' that starts a
// child segment, so "a-b" would land between "a" and "a.b" without being under "a".
func comparePaths(a, b *kpath.KPath) int {
	for a != nil && b != nil {
		if c := compareSegments(a, b); c != 0 {
			return c
		}
		a, b = a.Next, b.Next
	}
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1 // a is a prefix of b, so an ancestor: it comes first
	default:
		return 1
	}
}

// compareSegments totally orders two path segments: by kind first (so only like is
// compared with like), then by the segment's own value.
func compareSegments(a, b *kpath.KPath) int {
	if ra, rb := segmentRank(a), segmentRank(b); ra != rb {
		return cmp.Compare(ra, rb)
	}
	switch {
	case a.Field != nil:
		return strings.Compare(*a.Field, *b.Field)
	case a.Index != nil:
		return cmp.Compare(*a.Index, *b.Index)
	case a.SparseIndex != nil:
		return cmp.Compare(*a.SparseIndex, *b.SparseIndex)
	case a.Key != nil:
		return strings.Compare(*a.Key, *b.Key)
	}
	return 0 // same kind, both wildcards
}

// segmentRank ranks a segment by kind, keeping each kind's wildcard distinct from
// its concrete form. Patch roots come from a walk of the entry and so are concrete;
// the wildcard ranks are here so the order is total whatever it is handed.
func segmentRank(s *kpath.KPath) int {
	switch {
	case s.FieldAll:
		return 0
	case s.Field != nil:
		return 1
	case s.IndexAll:
		return 2
	case s.Index != nil:
		return 3
	case s.SparseIndexAll:
		return 4
	case s.SparseIndex != nil:
		return 5
	default:
		return 6 // key
	}
}

// subtreeAt returns the entry's node at path, and whether the entry reaches it.
func subtreeAt(patch *ir.Node, path string) (*ir.Node, bool) {
	if path == "" {
		return patch, true
	}
	sub, err := patch.GetKPath(path)
	if err != nil || sub == nil {
		return nil, false
	}
	return sub, true
}

// walkAndCollectPatchRoots walks the IR tree and collects nodes with PatchRootTag.
func walkAndCollectPatchRoots(node *ir.Node, path string, fn func(node *ir.Node, path string)) {
	if tx.HasPatchRootTag(node) {
		fn(node, path)
		return // Don't recurse into patched subtrees
	}

	switch node.Type {
	case ir.ObjectType:
		for i, field := range node.Fields {
			childPath := buildChildPath(path, field)
			walkAndCollectPatchRoots(node.Values[i], childPath, fn)
		}
	case ir.ArrayType:
		for i, value := range node.Values {
			childPath := path + "[" + strconv.Itoa(i) + "]"
			walkAndCollectPatchRoots(value, childPath, fn)
		}
	}
}

// buildChildPath constructs the child path for an object field.
func buildChildPath(parentPath string, field *ir.Node) string {
	switch field.Type {
	case ir.StringType:
		if parentPath == "" {
			return field.String
		}
		return parentPath + "." + field.String
	case ir.NumberType:
		idx := fieldToInt64(field)
		return parentPath + "{" + strconv.FormatInt(idx, 10) + "}"
	default:
		panic("unsupported field key type in patch tree")
	}
}

// fieldToInt64 extracts int64 from a number field.
func fieldToInt64(field *ir.Node) int64 {
	if field.Int64 != nil {
		return *field.Int64
	}
	if field.Float64 != nil {
		return int64(*field.Float64)
	}
	panic("number field has no numeric value")
}

// applyPatchesToNode applies a sequence of patches to a base node.
func applyPatchesToNode(base *ir.Node, patches []*ir.Node) (*ir.Node, error) {
	result := base
	for _, patch := range patches {
		// tony.Patch reports a delete by returning nil, and panics if handed one as the
		// document. Absent is null here, matching the empty-base branch, so a delete
		// followed by a write rebuilds from null instead of crashing. Folding dominated
		// patch roots into their dominating path made that sequence reachable: a delete
		// and a later write to the same subtree now arrive in one list.
		if result == nil {
			result = ir.Null()
		}
		next, err := tony.Patch(result, patch)
		if err != nil {
			return nil, err
		}
		result = next
	}
	return result, nil
}

// emitNode converts a node to events and writes them to the sink.
func emitNode(node *ir.Node, sink stream.EventWriter) error {
	events, err := stream.NodeToEvents(node)
	if err != nil {
		return err
	}
	for i := range events {
		if err := sink.WriteEvent(&events[i]); err != nil {
			return err
		}
	}
	return nil
}

// unreachedPatches grafts patch roots the base stream never passes through.
//
// The streaming processor applies a patch when the base's current path equals the
// patch's root path. A write that creates structure — a new key, a whole new subtree —
// has no such path in the base, so it is never applied. This tracks those paths and
// emits them at the deepest point of the base that does exist: as a new key just before
// its parent container closes, or in place of a scalar that the patch turns into a
// subtree.
//
// Grafted keys must land where they SORT, not at the end: storage keeps object keys
// sorted, and createSnapshot runs through this same code, so an unsorted read result
// becomes an unsorted snapshot. See system/logd/storage/docs/KEY_ORDER.md.
//
// Only plain field segments can be grafted. A missing keyed-array element ("items{42}")
// or array index would have to be created inside an array whose elements have already
// streamed past, and doing that correctly needs the key field, which lives in the
// schema, not here. Those return an error rather than silently dropping the write —
// silently dropping is the bug this exists to fix.
type unreachedPatches struct {
	values     map[string][]*ir.Node
	pending    map[string]bool
	stack      []unreachedFrame
	nextSeg    string // object key awaiting its value
	nextIsInt  bool   // that key is an int key, pathed as "{n}"
	sawIntKeys bool   // the innermost container uses int keys
}

type unreachedFrame struct {
	path     string
	isArray  bool
	idx      int
	lastKey  string // previous key seen in this container
	unsorted bool   // this container's keys did not arrive in order
}

func newUnreachedPatches(patchValues map[string][]*ir.Node) *unreachedPatches {
	pending := make(map[string]bool, len(patchValues))
	for path := range patchValues {
		pending[path] = true
	}
	return &unreachedPatches{values: patchValues, pending: pending}
}

func (u *unreachedPatches) empty() bool { return len(u.pending) == 0 }

func (u *unreachedPatches) paths() []string {
	out := make([]string, 0, len(u.pending))
	for p := range u.pending {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (u *unreachedPatches) reached(path string) { delete(u.pending, path) }

// observe follows the base stream's shape and emits grafts. It reports whether it
// consumed the event; if not, the caller passes the event through as usual.
func (u *unreachedPatches) observe(ev *stream.Event, sink stream.EventWriter) (bool, error) {
	switch ev.Type {
	case stream.EventKey:
		u.nextSeg = ev.Key
		u.nextIsInt = false
		if len(u.pending) > 0 && len(u.stack) > 0 {
			top := &u.stack[len(u.stack)-1]
			if top.lastKey > ev.Key {
				// Keys out of order: this base predates the storage invariant, so
				// position cannot be inferred. Fall back to grafting at the close.
				top.unsorted = true
			}
			top.lastKey = ev.Key
			if !top.unsorted {
				// Storage keeps objects sorted, so a grafted key belongs where it sorts,
				// not at the end. Keys arrive in order, so anything sorting before this
				// one can be emitted now and nothing later can collide with it.
				if err := u.graftUpTo(*top, ev.Key, sink); err != nil {
					return false, err
				}
			}
		}
		return false, nil
	case stream.EventIntKey:
		// A sparse array's keys are int-keyed object fields, pathed as "{n}" — the same
		// form buildChildPath produces. Treating them as values (they are not) made a
		// pending patch look like it lived under the key event, which replaced the key
		// with a materialized node and corrupted the stream.
		u.nextSeg = "{" + strconv.FormatInt(ev.IntKey, 10) + "}"
		u.nextIsInt = true
		return false, nil
	case stream.EventHeadComment, stream.EventLineComment:
		return false, nil // comments carry no path
	case stream.EventBeginObject, stream.EventBeginArray:
		path := u.childPath()
		u.stack = append(u.stack, unreachedFrame{path: path, isArray: ev.Type == stream.EventBeginArray})
		u.nextSeg = ""
		return false, nil
	case stream.EventEndObject, stream.EventEndArray:
		if len(u.stack) == 0 {
			return false, nil
		}
		top := u.stack[len(u.stack)-1]
		u.stack = u.stack[:len(u.stack)-1]
		u.advanceIndex()
		if len(u.pending) == 0 {
			return false, nil
		}
		// Anything still pending under this container is missing from the base: this is
		// the last chance to add it, just before the container closes.
		return false, u.graftUpTo(top, "", sink)
	default:
		// A scalar. If a patch lives underneath it, the patch turns this value into a
		// subtree, so the base's scalar must be replaced rather than followed by a
		// duplicate key.
		path := u.childPath()
		u.advanceIndex()
		if len(u.pending) == 0 {
			return false, nil
		}
		return u.replaceScalar(path, ev, sink)
	}
}

// childPath is the path of the value about to be read.
func (u *unreachedPatches) childPath() string {
	if len(u.stack) == 0 {
		return "" // document root
	}
	top := u.stack[len(u.stack)-1]
	if top.isArray {
		return top.path + "[" + strconv.Itoa(top.idx) + "]"
	}
	if u.nextIsInt {
		return top.path + u.nextSeg // "{n}" attaches without a dot
	}
	if top.path == "" {
		return u.nextSeg
	}
	return top.path + "." + u.nextSeg
}

func (u *unreachedPatches) advanceIndex() {
	if len(u.stack) == 0 {
		return
	}
	top := &u.stack[len(u.stack)-1]
	if top.isArray {
		top.idx++
	}
}

// graftUpTo emits the pending paths whose deepest existing ancestor is this container
// and whose key sorts before `before`. An empty `before` means "everything left", which
// is what the container's closing event asks for.
func (u *unreachedPatches) graftUpTo(f unreachedFrame, before string, sink stream.EventWriter) error {
	// Group by first missing segment: two pending paths under the same missing key must
	// become one key with a merged value, not the same key twice.
	groups := map[string][]string{}
	var order []string
	for path := range u.pending {
		rest, ok := remainderUnder(f.path, path)
		if !ok {
			continue
		}
		seg, _, err := splitFieldSegment(rest)
		if err != nil {
			if before != "" {
				// Mid-container: this path may still be reached by an element the base
				// has not streamed yet. Only the container's close proves it absent, and
				// that is where the error belongs.
				continue
			}
			return fmt.Errorf("cannot graft %q into %q: %w", path, f.path, err)
		}
		if before != "" && seg >= before {
			// seg == before is the base's own key: the graft belongs deeper, under it.
			continue
		}
		if _, seen := groups[seg]; !seen {
			order = append(order, seg)
		}
		groups[seg] = append(groups[seg], path)
	}
	if len(groups) == 0 {
		return nil
	}
	if f.isArray {
		return fmt.Errorf("cannot graft %v into the array at %q: creating array elements needs the key field", order, f.path)
	}
	sort.Strings(order)

	for _, seg := range order {
		node := ir.Null()
		for _, path := range groups[seg] {
			rest, _ := remainderUnder(f.path, path)
			nested, err := nestUnder(rest, u.values[path])
			if err != nil {
				return err
			}
			delete(u.pending, path)
			if nested == nil {
				continue // nothing to add here
			}
			node, err = tony.Patch(node, nested)
			if err != nil {
				return err
			}
		}
		if node == nil || node.Type == ir.NullType {
			continue // every path under this key folded away
		}
		// nestUnder built the value from the container's perspective, so it already
		// carries the key: emit the field name, then the value under it.
		// The fold can leave this key with nothing under it — a write and a later delete
		// of the same path net out, and the key is simply absent from the folded node,
		// exactly as it would be if the entries had been applied to the document
		// directly. Nothing to graft, so nothing to emit.
		value, err := node.GetKPath(seg)
		if err != nil || value == nil {
			continue
		}
		tx.StripPatchRootTagRecursive(value)
		if err := sink.WriteEvent(&stream.Event{Type: stream.EventKey, Key: seg}); err != nil {
			return err
		}
		if err := emitNode(value, sink); err != nil {
			return err
		}
	}
	return nil
}

// replaceScalar handles a scalar in the base that a patch underneath turns into a
// subtree. Returns true when the scalar event has been replaced.
func (u *unreachedPatches) replaceScalar(path string, ev *stream.Event, sink stream.EventWriter) (bool, error) {
	var under []string
	for p := range u.pending {
		if _, ok := remainderUnder(path, p); ok {
			under = append(under, p)
		}
	}
	if len(under) == 0 {
		return false, nil
	}
	sort.Strings(under)

	base, err := stream.EventsToNode([]stream.Event{*ev})
	if err != nil {
		return false, fmt.Errorf("materializing the scalar at %q: %w", path, err)
	}
	replaced := false
	for _, p := range under {
		rest, _ := remainderUnder(path, p)
		nested, err := nestUnder(rest, u.values[p])
		if err != nil {
			return false, err
		}
		delete(u.pending, p)
		if nested == nil {
			continue // a delete under a scalar leaves the scalar as it was
		}
		if base == nil {
			base = ir.Null()
		}
		base, err = tony.Patch(base, nested)
		if err != nil {
			return false, err
		}
		replaced = true
	}
	if !replaced {
		return false, nil
	}
	if base == nil {
		base = ir.Null()
	}
	tx.StripPatchRootTagRecursive(base)
	return true, emitNode(base, sink)
}

// remainderUnder returns path's remainder below container, and whether path is strictly
// under it. Segment boundaries are respected, so "ab" is not under "a".
func remainderUnder(container, path string) (string, bool) {
	if container == "" {
		if path == "" {
			return "", false
		}
		return path, true
	}
	if len(path) <= len(container) || path[:len(container)] != container {
		return "", false
	}
	switch path[len(container)] {
	case '.':
		return path[len(container)+1:], true
	case '{', '[':
		// A keyed or indexed segment: under the container, but not graftable. Returning
		// it lets graftInto produce a real error instead of dropping the write.
		return path[len(container):], true
	default:
		return "", false
	}
}

// splitFieldSegment splits a remainder into its first plain field segment and the rest.
func splitFieldSegment(rest string) (seg, tail string, err error) {
	if rest == "" {
		return "", "", fmt.Errorf("empty path remainder")
	}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '.':
			return rest[:i], rest[i+1:], nil
		case '{', '[':
			if i == 0 {
				return "", "", fmt.Errorf("segment %q is not a plain field", rest)
			}
			return "", "", fmt.Errorf("segment %q is keyed or indexed, which cannot be created here", rest)
		}
	}
	if name, isField := kpath.SegmentFieldName(rest); isField {
		return name, "", nil
	}
	return rest, "", nil
}

// nestUnder folds the patch nodes as seen from a container, wrapping EACH of them in
// the objects named by rest before applying it — not folding them first and wrapping the
// result. The difference shows when a write is later deleted: folding first collapses
// "{b: {c: 1}} then {b: {c: !delete}}" to nothing, while the log's own semantics leave
// "{b: {}}" behind, because the delete removes c and not the b that the earlier write
// created. Wrapping each node keeps every step identical to applying the entries to the
// document directly, which is what the reference does.
//
// Returns (nil, nil) when the fold leaves nothing at all — deleting a path the base
// never had is a no-op, not something to graft on.
func nestUnder(rest string, values []*ir.Node) (*ir.Node, error) {
	var segs []string
	for rest != "" {
		seg, tail, err := splitFieldSegment(rest)
		if err != nil {
			return nil, err
		}
		segs = append(segs, seg)
		rest = tail
	}

	var node *ir.Node = ir.Null()
	for _, pn := range values {
		wrapped := pn
		for i := len(segs) - 1; i >= 0; i-- {
			wrapped = ir.FromKeyVals([]ir.KeyVal{{Key: ir.FromString(segs[i]), Val: wrapped}})
		}
		if node == nil {
			node = ir.Null()
		}
		next, err := tony.Patch(node, wrapped)
		if err != nil {
			return nil, err
		}
		node = next
	}
	return node, nil
}
