package patches

import (
	"fmt"
	"io"
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

	for {
		ev, err := baseEvents.ReadEvent()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

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

		// Not actively collecting, pass through
		if err := sink.WriteEvent(ev); err != nil {
			return err
		}
	}

	return nil
}

// buildPatchValueIndex builds a map from path to ordered patch nodes.
// Filters out dominated paths (child paths where an ancestor also has a patch).
func buildPatchValueIndex(patches []*ir.Node) (map[string][]*ir.Node, error) {
	result := make(map[string][]*ir.Node)

	for _, patch := range patches {
		if patch == nil {
			continue
		}
		walkAndCollectPatchRoots(patch, "", func(node *ir.Node, path string) {
			result[path] = append(result[path], node)
		})
	}

	return filterDominatedPaths(result)
}

// filterDominatedPaths removes paths that are dominated by ancestor paths.
// A path is dominated if any of its ancestors also has patches.
// Returns the original map and an error if path parsing fails.
func filterDominatedPaths(patchValues map[string][]*ir.Node) (map[string][]*ir.Node, error) {
	if len(patchValues) <= 1 {
		return patchValues, nil
	}

	// Parse all paths once
	parsed := make(map[string]*kpath.KPath, len(patchValues))
	for path := range patchValues {
		if path == "" {
			parsed[path] = nil // root path
		} else {
			kp, err := kpath.Parse(path)
			if err != nil {
				return nil, fmt.Errorf("failed to parse path %q: %w", path, err)
			}
			parsed[path] = kp
		}
	}

	// Check each path for domination
	result := make(map[string][]*ir.Node)
	for path, kp := range parsed {
		if !isDominated(kp, path, parsed) {
			result[path] = patchValues[path]
		}
	}

	return result, nil
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
		var err error
		result, err = tony.Patch(result, patch)
		if err != nil {
			return nil, err
		}
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
