package snap

import (
	"fmt"
	"io"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
)

// FromSnap writes the index and chunks to w resulting from applying patches
// to the ir.Node represented by s. patches are associated with kpaths representing
// the path at which the patches will be applied in order.
// If patches target chunked containers, those containers are reconstructed from chunks,
// patches are applied, and the result is either kept as a regular node (if small) or
// re-chunked (if large).
func FromSnap(w io.Writer, s Snapshot, patches []*ir.Node) (int, error) {
	// Load the base snapshot index
	indexNode, err := s.Index()
	if err != nil {
		return 0, fmt.Errorf("failed to get index: %w", err)
	}

	// Reconstruct the base node, loading chunked containers if patches target them
	baseNode, err := reconstructFromIndexWithPatches(s, indexNode, patches)
	if err != nil {
		return 0, fmt.Errorf("failed to reconstruct base snapshot: %w", err)
	}

	// Apply patches at their kpaths
	patchedNode := baseNode
	for kpath, patchList := range patches {
		targetNode, err := patchedNode.GetKPath(kpath)
		if err != nil {
			// Path doesn't exist - skip patches for non-existent paths
			continue
		}

		// Apply each patch sequentially
		currentNode := targetNode
		for _, patch := range patchList {
			patched, err := tony.Patch(currentNode, patch)
			if err != nil {
				return 0, fmt.Errorf("failed to apply patch at %s: %w", kpath, err)
			}
			currentNode = patched
		}

		patchedNode, err = setNodeAtKPath(patchedNode, kpath, currentNode)
		if err != nil {
			return 0, fmt.Errorf("failed to set patched node at %s: %w", kpath, err)
		}
	}

	// Write the new snapshot
	return WriteFromIR(w, patchedNode)
}

// reconstructFromIndexWithPatches reconstructs nodes, loading chunked containers
// if patches target them or anything inside them.
func reconstructFromIndexWithPatches(s Snapshot, indexNode *ir.Node, patches map[string][]*ir.Node) (*ir.Node, error) {
	hasSnapLoc := ir.TagHas(indexNode.Tag, "!snap-loc")
	hasSnapRange := ir.TagHas(indexNode.Tag, "!snap-range")
	if hasSnapLoc || hasSnapRange {
		return s.Load(indexNode)
	}

	// Check if any patches target this chunked container or its children
	if ir.TagHas(indexNode.Tag, "!snap-chunks") {
		// If there are patches, assume they might target this container
		// In a more sophisticated implementation, we'd parse kpaths to check
		// if they actually target this container or its children
		needsReconstruction := len(patches) > 0

		if needsReconstruction {
			// Reconstruct the chunked container by loading all its chunks
			return reconstructChunkedContainerForPatching(s, indexNode)
		}

		// No patches target this container - return index structure as-is
		return indexNode.Clone(), nil
	}

	// Regular node - reconstruct children
	node := &ir.Node{
		Type:   indexNode.Type,
		Tag:    indexNode.Tag,
		Fields: make([]*ir.Node, len(indexNode.Fields)),
		Values: make([]*ir.Node, len(indexNode.Values)),
	}

	// Copy simple fields
	node.String = indexNode.String
	node.Bool = indexNode.Bool
	node.Number = indexNode.Number
	if indexNode.Int64 != nil {
		val := *indexNode.Int64
		node.Int64 = &val
	}
	if indexNode.Float64 != nil {
		val := *indexNode.Float64
		node.Float64 = &val
	}

	// Reconstruct children
	for i := 0; i < len(indexNode.Values); i++ {
		hasField := i < len(indexNode.Fields)
		if hasField {
			node.Fields[i] = indexNode.Fields[i].Clone()
		}
		child, err := reconstructFromIndexWithPatches(s, indexNode.Values[i], patches)
		if err != nil {
			return nil, err
		}
		node.Values[i] = child
		child.Parent = node
		child.ParentIndex = i
		if hasField && indexNode.Fields[i].Type != ir.NullType {
			child.ParentField = indexNode.Fields[i].String
		}
	}

	return node, nil
}

// loadRangeNode loads a !snap-range node and merges its children into the container
func loadRangeNode(s Snapshot, childIndex *ir.Node, node *ir.Node) error {
	rangeNode, err := s.Load(childIndex)
	if err != nil {
		return err
	}
	if rangeNode.Type == node.Type {
		node.Fields = append(node.Fields, rangeNode.Fields...)
		node.Values = append(node.Values, rangeNode.Values...)
	}
	return nil
}

// addChunkedChild adds a nested chunked container child as-is (not reconstructed)
func addChunkedChild(indexNode *ir.Node, node *ir.Node, i int) {
	if i < len(indexNode.Fields) {
		node.Fields = append(node.Fields, indexNode.Fields[i].Clone())
	}
	node.Values = append(node.Values, indexNode.Values[i].Clone())
}

// addRegularChild reconstructs and adds a regular (non-chunked) child
func addRegularChild(
	s Snapshot,
	indexNode *ir.Node,
	childIndex *ir.Node,
	node *ir.Node,
	i int,
) error {
	child, err := reconstructFromIndexWithPatches(s, childIndex, nil)
	if err != nil {
		return err
	}

	hasField := i < len(indexNode.Fields)
	if hasField {
		field := indexNode.Fields[i]
		if field.Type == ir.NullType {
			return nil
		}
		node.Fields = append(node.Fields, field.Clone())
	}
	node.Values = append(node.Values, child)
	child.Parent = node
	child.ParentIndex = len(node.Values) - 1
	return nil
}

// reconstructChunkedContainerForPatching reconstructs a chunked container by loading
// all its chunks. This is only done when patches target the container, and the patches
// are assumed to fit in memory.
func reconstructChunkedContainerForPatching(s Snapshot, indexNode *ir.Node) (*ir.Node, error) {
	// Reconstruct container from chunks (preserve original tag like !sparsearray)
	node := &ir.Node{
		Type:   indexNode.Type,
		Tag:    indexNode.Tag, // Preserve tag (will have !snap-chunks, but also !sparsearray if applicable)
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	for i := 0; i < len(indexNode.Values); i++ {
		childIndex := indexNode.Values[i]

		if ir.TagHas(childIndex.Tag, "!snap-range") {
			if err := loadRangeNode(s, childIndex, node); err != nil {
				return nil, err
			}
			continue
		}

		if ir.TagHas(childIndex.Tag, "!snap-chunks") {
			// Nested chunked container - keep as-is for now
			// Could be enhanced to check if patches target it
			addChunkedChild(indexNode, node, i)
			continue
		}

		if err := addRegularChild(s, indexNode, childIndex, node, i); err != nil {
			return nil, err
		}
	}

	// Remove !snap-chunks tag, keep other tags like !sparsearray
	node.Tag = ir.TagRemove(node.Tag, "!snap-chunks")

	return node, nil
}

// setNodeAtKPath sets a node at a given kpath in the tree
func setNodeAtKPath(root *ir.Node, kpath string, value *ir.Node) (*ir.Node, error) {
	if kpath == "" {
		return value, nil
	}

	kp, err := ir.ParseKPath(kpath)
	if err != nil {
		return nil, err
	}

	return setNodeAtKPathRecursive(root, kp, value)
}

// updateObjectField updates an existing field in an object
func updateObjectField(result *ir.Node, fieldName string, kp *ir.KPath, value *ir.Node) error {
	for i, field := range result.Fields {
		if field.String != fieldName {
			continue
		}
		child, err := setNodeAtKPathRecursive(result.Values[i], kp.Next, value)
		if err != nil {
			return err
		}
		result.Values[i] = child
		child.Parent = result
		child.ParentIndex = i
		child.ParentField = fieldName
		return nil
	}
	return fmt.Errorf("field not found")
}

// addObjectField adds a new field to an object
func addObjectField(result *ir.Node, fieldName string, kp *ir.KPath, value *ir.Node) error {
	child, err := setNodeAtKPathRecursive(ir.Null(), kp.Next, value)
	if err != nil {
		return err
	}
	result.Fields = append(result.Fields, ir.FromString(fieldName))
	result.Values = append(result.Values, child)
	child.Parent = result
	child.ParentIndex = len(result.Values) - 1
	child.ParentField = fieldName
	return nil
}

// setObjectField sets a field in an object (updates existing or adds new)
func setObjectField(result *ir.Node, kp *ir.KPath, value *ir.Node) (*ir.Node, error) {
	if result.Type != ir.ObjectType {
		return nil, fmt.Errorf("expected object at path")
	}

	fieldName := *kp.Field
	if err := updateObjectField(result, fieldName, kp, value); err == nil {
		return result, nil
	}
	if err := addObjectField(result, fieldName, kp, value); err != nil {
		return nil, err
	}
	return result, nil
}

// setArrayElement sets an element in an array
func setArrayElement(result *ir.Node, kp *ir.KPath, value *ir.Node) (*ir.Node, error) {
	if result.Type != ir.ArrayType {
		return nil, fmt.Errorf("expected array at path")
	}

	index := *kp.Index
	if index < 0 || index >= len(result.Values) {
		return nil, fmt.Errorf("index out of bounds")
	}

	child, err := setNodeAtKPathRecursive(result.Values[index], kp.Next, value)
	if err != nil {
		return nil, err
	}
	result.Values[index] = child
	child.Parent = result
	child.ParentIndex = index

	return result, nil
}

func setNodeAtKPathRecursive(node *ir.Node, kp *ir.KPath, value *ir.Node) (*ir.Node, error) {
	if kp == nil {
		return value, nil
	}

	result := node.Clone()

	if kp.Field != nil {
		return setObjectField(result, kp, value)
	}

	if kp.Index != nil {
		return setArrayElement(result, kp, value)
	}

	return result, nil
}
