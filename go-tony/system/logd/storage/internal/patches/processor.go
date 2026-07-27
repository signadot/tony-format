package patches

import (
	"fmt"
	"io"
	"sort"
	"strconv"

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

		// Not actively collecting, pass through
		if err := sink.WriteEvent(ev); err != nil {
			return err
		}
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
			var err error
			result, err = tony.Patch(result, patch)
			if err != nil {
				return err
			}
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

	applyPaths, err := maximalPaths(rootPaths)
	if err != nil {
		return nil, err
	}

	// Commit order is the order of `patches`, so the outer loop must be over entries.
	result := make(map[string][]*ir.Node, len(applyPaths))
	for i, patch := range patches {
		if patch == nil {
			continue
		}
		for _, path := range applyPaths {
			// An entry with a root exactly here contributes that node, as it always has.
			// Re-deriving it by navigating from the entry root would be equivalent only
			// for plain field paths — GetKPath does not resolve a sparse-index segment
			// ("items{1}") back to the node the walk found, so navigating unconditionally
			// silently dropped sparse-array patches.
			if node, ok := entryRoots[i][path]; ok {
				result[path] = append(result[path], node)
				continue
			}
			// Otherwise this entry only matters here if it wrote BELOW this path and was
			// dominated: navigate to the applied path so its write is folded in rather
			// than discarded.
			below, err := hasRootBelow(entryRoots[i], path)
			if err != nil {
				return nil, err
			}
			if !below {
				continue
			}
			if sub, ok := subtreeAt(patch, path); ok {
				result[path] = append(result[path], sub)
			}
		}
	}
	return result, nil
}

// hasRootBelow reports whether any of the entry's patch roots lies strictly under path.
func hasRootBelow(roots map[string]*ir.Node, path string) (bool, error) {
	if len(roots) == 0 {
		return false, nil
	}
	var anc *kpath.KPath
	if path != "" {
		var err error
		anc, err = kpath.Parse(path)
		if err != nil {
			return false, fmt.Errorf("failed to parse path %q: %w", path, err)
		}
	}
	for rootPath := range roots {
		if rootPath == path {
			continue
		}
		if path == "" {
			return true, nil // every non-root path is under the document root
		}
		kp, err := kpath.Parse(rootPath)
		if err != nil {
			return false, fmt.Errorf("failed to parse path %q: %w", rootPath, err)
		}
		if isAnc, eq := anc.AncestorOrEqual(kp); isAnc && !eq {
			return true, nil
		}
	}
	return false, nil
}

// maximalPaths returns the paths with no strict ancestor in the set — the paths at
// which subtrees will be collected and patched. The result is sorted for determinism.
func maximalPaths(paths map[string]bool) ([]string, error) {
	parsed := make(map[string]*kpath.KPath, len(paths))
	for path := range paths {
		if path == "" {
			parsed[path] = nil // root
		} else {
			kp, err := kpath.Parse(path)
			if err != nil {
				return nil, fmt.Errorf("failed to parse path %q: %w", path, err)
			}
			parsed[path] = kp
		}
	}

	out := make([]string, 0, len(parsed))
	for path, kp := range parsed {
		if !isDominated(kp, path, parsed) {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out, nil
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

// isDominated returns true if any other path in parsed is an ancestor of kp.
func isDominated(kp *kpath.KPath, path string, parsed map[string]*kpath.KPath) bool {
	for otherPath, otherKp := range parsed {
		if otherPath == path {
			continue // skip self
		}
		if anc, eq := otherKp.AncestorOrEqual(kp); anc && !eq {
			return true
		}
	}
	return false
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
	path    string
	isArray bool
	idx     int
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
		return false, u.graftInto(top, sink)
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

// graftInto emits the pending paths whose deepest existing ancestor is this container.
func (u *unreachedPatches) graftInto(f unreachedFrame, sink stream.EventWriter) error {
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
			return fmt.Errorf("cannot graft %q into %q: %w", path, f.path, err)
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
		value, err := node.GetKPath(seg)
		if err != nil {
			return fmt.Errorf("graft of %q lost its key: %w", seg, err)
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
