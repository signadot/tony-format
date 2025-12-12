package snap

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

// WriteFromIR writes the index and chunks to w representing node and returns the
// number of bytes written.
func WriteFromIR(w io.Writer, node *ir.Node) (int, error) {
	return writeFromIRWithThreshold(w, node, DefaultChunkThreshold)
}

// writeFromIRWithThreshold writes with a custom chunk threshold
//
// On-disk format:
//
//	[4 bytes: uint32 index length][index node bytes][data chunks...]
//
// The index length is written first as a big-endian uint32, followed by
// the encoded index node, then the data chunks sequentially. Offsets in
// the index are relative to the start of the data section (after index).
func writeFromIRWithThreshold(w io.Writer, node *ir.Node, threshold int64) (int, error) {
	// Build index and collect data chunks
	indexNode, chunks, err := buildIndex(node, threshold)
	if err != nil {
		return 0, fmt.Errorf("failed to build index: %w", err)
	}

	// Encode index node to buffer to get its size
	var indexBuf bytes.Buffer
	if err := encode.Encode(indexNode, &indexBuf); err != nil {
		return 0, fmt.Errorf("failed to encode index: %w", err)
	}
	indexBytes := indexBuf.Bytes()
	indexLength := uint32(len(indexBytes))

	// Write index length (4 bytes, big-endian uint32)
	lengthBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(lengthBytes, indexLength)
	if _, err := w.Write(lengthBytes); err != nil {
		return 0, fmt.Errorf("failed to write index length: %w", err)
	}

	// Write index node
	if _, err := w.Write(indexBytes); err != nil {
		return 0, fmt.Errorf("failed to write index: %w", err)
	}

	// Write data chunks in order
	dataBytes := 0
	for _, chunk := range chunks {
		n, err := w.Write(chunk.data)
		if err != nil {
			return 0, fmt.Errorf("failed to write chunk: %w", err)
		}
		dataBytes += n
	}

	// Total: 4 (length) + index size + data size
	return 4 + len(indexBytes) + dataBytes, nil
}

type chunkInfo struct {
	data   []byte
	offset int64
	size   int64
}

func buildIndex(node *ir.Node, threshold int64) (*ir.Node, []chunkInfo, error) {
	var chunks []chunkInfo
	var currentOffset int64

	indexNode, err := buildIndexRecursive(node, threshold, &chunks, &currentOffset)
	if err != nil {
		return nil, nil, err
	}

	return indexNode, chunks, nil
}

// encodeNode encodes a node and returns both the bytes and size.
// This is used when we need the actual encoded data, not just the size.
func encodeNode(node *ir.Node) ([]byte, int64, error) {
	var buf bytes.Buffer
	if err := encode.Encode(node, &buf); err != nil {
		return nil, 0, err
	}
	data := buf.Bytes()
	return data, int64(len(data)), nil
}

func buildIndexRecursive(node *ir.Node, threshold int64, chunks *[]chunkInfo, offset *int64) (*ir.Node, error) {
	isContainer := node.Type == ir.ObjectType || node.Type == ir.ArrayType
	if isContainer {
		// Check data size to decide between interior node vs chunked
		dataSize, err := estimateNodeSize(node)
		if err != nil {
			return nil, err
		}

		if dataSize < threshold {
			// Small container - use interior node (recursively contains snap nodes)
			return buildSmallContainerIndex(node, threshold, chunks, offset)
		}

		// Large container - need to chunk with !snap-chunks
		return buildChunkedIndex(node, threshold, chunks, offset)
	}

	// Leaf node - check if we should use !snap-loc
	estimatedSize, err := estimateNodeSize(node)
	if err != nil {
		return nil, err
	}

	if estimatedSize < threshold {
		return node.Clone(), nil
	}

	// Large leaf - encode to get the actual bytes
	data, actualSize, err := encodeNode(node)
	if err != nil {
		return nil, err
	}
	chunk := chunkInfo{
		data:   data,
		offset: *offset,
		size:   actualSize,
	}
	*chunks = append(*chunks, chunk)

	// Create !snap-loc node: [offset, size]
	locNode := ir.FromSlice([]*ir.Node{
		&ir.Node{Type: ir.NumberType, Int64: &chunk.offset},
		&ir.Node{Type: ir.NumberType, Int64: &chunk.size},
	})
	locNode.Tag = "!snap-loc"

	*offset += actualSize
	return locNode, nil
}

func buildChunkedIndex(node *ir.Node, threshold int64, chunks *[]chunkInfo, offset *int64) (*ir.Node, error) {
	if node.Type == ir.ObjectType {
		return buildChunkedObjectIndex(node, threshold, chunks, offset)
	}
	if node.Type == ir.ArrayType {
		return buildChunkedArrayIndex(node, threshold, chunks, offset)
	}
	return buildSmallContainerIndex(node, threshold, chunks, offset)
}

// objectRangeState tracks the state of building ranges for chunked objects
type objectRangeState struct {
	rangeStart      int64
	rangeStartIndex int
	rangeFields     []*ir.Node
	rangeValues     []*ir.Node
}

// finalizeObjectRange finalizes a pending range and adds it to the index
func finalizeObjectRange(
	state *objectRangeState,
	indexNode *ir.Node,
	isSparseArray bool,
	chunks *[]chunkInfo,
	offset *int64,
) error {
	if len(state.rangeValues) == 0 {
		return nil
	}

	rangeData := buildRangeData(state.rangeValues, state.rangeFields, isSparseArray)
	chunk := chunkInfo{
		data:   rangeData,
		offset: state.rangeStart,
		size:   int64(len(rangeData)),
	}
	*chunks = append(*chunks, chunk)
	*offset += chunk.size

	// Create !snap-range node: !snap-range(from,to) [offset, size]
	rangeEndIndex := state.rangeStartIndex + len(state.rangeValues)
	rangeStartIdx := int64(state.rangeStartIndex)
	rangeEndIdx := int64(rangeEndIndex)
	rangeNode := ir.FromSlice([]*ir.Node{
		&ir.Node{Type: ir.NumberType, Int64: &state.rangeStart},
		&ir.Node{Type: ir.NumberType, Int64: &chunk.size},
	})
	rangeNode.Tag = fmt.Sprintf("!snap-range(%d,%d)", rangeStartIdx, rangeEndIdx)
	indexNode.Fields = append(indexNode.Fields, &ir.Node{Type: ir.NullType})
	indexNode.Values = append(indexNode.Values, rangeNode)

	// Reset range state
	state.rangeStart = *offset
	state.rangeStartIndex = len(indexNode.Values)
	state.rangeFields = nil
	state.rangeValues = nil

	return nil
}

// addDirectChild adds a child directly to the index (not in a range)
func addDirectChild(
	field *ir.Node,
	value *ir.Node,
	indexNode *ir.Node,
	threshold int64,
	chunks *[]chunkInfo,
	offset *int64,
) error {
	childIndex, err := buildIndexRecursive(value, threshold, chunks, offset)
	if err != nil {
		return err
	}
	indexNode.Fields = append(indexNode.Fields, field.Clone())
	indexNode.Values = append(indexNode.Values, childIndex)
	return nil
}

// addToObjectRange adds a large container child to the pending range
func addToObjectRange(
	field *ir.Node,
	value *ir.Node,
	state *objectRangeState,
) error {
	state.rangeFields = append(state.rangeFields, field)
	state.rangeValues = append(state.rangeValues, value)
	return nil
}

func buildChunkedObjectIndex(node *ir.Node, threshold int64, chunks *[]chunkInfo, offset *int64) (*ir.Node, error) {
	// Tag container with !snap-chunks (preserve original tags like !sparsearray)
	indexNode := &ir.Node{
		Type:   node.Type,
		Tag:    ir.TagCompose("!snap-chunks", nil, node.Tag),
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	state := &objectRangeState{
		rangeStart:      *offset,
		rangeStartIndex: 0,
		rangeFields:     make([]*ir.Node, 0),
		rangeValues:     make([]*ir.Node, 0),
	}

	isSparseArray := ir.TagHas(node.Tag, "!sparsearray")

	for i := 0; i < len(node.Fields); i++ {
		field := node.Fields[i]
		value := node.Values[i]

		childDataSize, err := estimateNodeSize(value)
		if err != nil {
			return nil, err
		}

		isChildContainer := value.Type == ir.ObjectType || value.Type == ir.ArrayType

		if childDataSize < threshold {
			// Small child - include directly as interior node
			// TODO: If the pending range is tiny (below threshold), don't create a chunk for it.
			if err := finalizeObjectRange(state, indexNode, isSparseArray, chunks, offset); err != nil {
				return nil, err
			}
			if err := addDirectChild(field, value, indexNode, threshold, chunks, offset); err != nil {
				return nil, err
			}
		} else if !isChildContainer {
			// Large leaf - build as !snap-loc (not a range)
			if err := finalizeObjectRange(state, indexNode, isSparseArray, chunks, offset); err != nil {
				return nil, err
			}
			if err := addDirectChild(field, value, indexNode, threshold, chunks, offset); err != nil {
				return nil, err
			}
		} else {
			// Large container child - add to range or include directly
			// Check if we should finalize current range before adding this
			if len(state.rangeValues) > 0 {
				// Estimate if adding this would exceed threshold
				var tempNode *ir.Node
				if isSparseArray && field.Type == ir.NumberType && field.Int64 != nil {
					tempNode = ir.FromIntKeysMap(map[uint32]*ir.Node{uint32(*field.Int64): value})
				} else {
					tempNode = ir.FromMap(map[string]*ir.Node{field.String: value})
				}
				pairSize, err := estimateNodeSize(tempNode)
				if err != nil {
					return nil, err
				}

				currentRangeSize := int64(0)
				for _, v := range state.rangeValues {
					sz, err := estimateNodeSize(v)
					if err != nil {
						return nil, err
					}
					currentRangeSize += sz
				}

				if currentRangeSize+pairSize >= threshold {
					// Finalize current range before adding this
					if err := finalizeObjectRange(state, indexNode, isSparseArray, chunks, offset); err != nil {
						return nil, err
					}
				}
			}

			// Start new range if needed
			if len(state.rangeValues) == 0 {
				state.rangeStartIndex = len(indexNode.Values)
			}

			// Add to range
			if err := addToObjectRange(field, value, state); err != nil {
				return nil, err
			}
		}
	}

	// Finalize last range if any
	if err := finalizeObjectRange(state, indexNode, isSparseArray, chunks, offset); err != nil {
		return nil, err
	}

	return indexNode, nil
}

// arrayRangeState tracks the state of building ranges for chunked arrays
type arrayRangeState struct {
	rangeStart      int64
	rangeStartIndex int
	rangeChildren   []*ir.Node
}

// finalizeArrayRange finalizes a pending range and adds it to the index
func finalizeArrayRange(
	state *arrayRangeState,
	indexNode *ir.Node,
	chunks *[]chunkInfo,
	offset *int64,
) error {
	if len(state.rangeChildren) == 0 {
		return nil
	}

	rangeData := buildArrayRangeData(state.rangeChildren)
	chunk := chunkInfo{
		data:   rangeData,
		offset: state.rangeStart,
		size:   int64(len(rangeData)),
	}
	*chunks = append(*chunks, chunk)
	*offset += chunk.size

	// Create !snap-range node: !snap-range(from,to) [offset, size]
	rangeEndIndex := state.rangeStartIndex + len(state.rangeChildren)
	rangeStartIdx := int64(state.rangeStartIndex)
	rangeEndIdx := int64(rangeEndIndex)
	rangeNode := ir.FromSlice([]*ir.Node{
		&ir.Node{Type: ir.NumberType, Int64: &state.rangeStart},
		&ir.Node{Type: ir.NumberType, Int64: &chunk.size},
	})
	rangeNode.Tag = fmt.Sprintf("!snap-range(%d,%d)", rangeStartIdx, rangeEndIdx)
	indexNode.Values = append(indexNode.Values, rangeNode)

	// Reset range state
	state.rangeStart = *offset
	state.rangeStartIndex = len(indexNode.Values)
	state.rangeChildren = nil

	return nil
}

// addDirectArrayChild adds a child directly to the array index (not in a range)
func addDirectArrayChild(
	value *ir.Node,
	indexNode *ir.Node,
	threshold int64,
	chunks *[]chunkInfo,
	offset *int64,
) error {
	childIndex, err := buildIndexRecursive(value, threshold, chunks, offset)
	if err != nil {
		return err
	}
	indexNode.Values = append(indexNode.Values, childIndex)
	return nil
}

// addToArrayRange adds a large container child to the pending range
func addToArrayRange(
	value *ir.Node,
	state *arrayRangeState,
) error {
	state.rangeChildren = append(state.rangeChildren, value)
	return nil
}

func buildChunkedArrayIndex(node *ir.Node, threshold int64, chunks *[]chunkInfo, offset *int64) (*ir.Node, error) {
	// Tag container with !snap-chunks
	indexNode := &ir.Node{
		Type:   node.Type,
		Tag:    "!snap-chunks",
		Fields: make([]*ir.Node, 0),
		Values: make([]*ir.Node, 0),
	}

	state := &arrayRangeState{
		rangeStart:      *offset,
		rangeStartIndex: 0,
		rangeChildren:   make([]*ir.Node, 0),
	}

	for i := 0; i < len(node.Values); i++ {
		value := node.Values[i]

		childDataSize, err := estimateNodeSize(value)
		if err != nil {
			return nil, err
		}

		isChildContainer := value.Type == ir.ObjectType || value.Type == ir.ArrayType

		if childDataSize < threshold {
			// Small child - include directly as interior node
			if err := finalizeArrayRange(state, indexNode, chunks, offset); err != nil {
				return nil, err
			}
			if err := addDirectArrayChild(value, indexNode, threshold, chunks, offset); err != nil {
				return nil, err
			}
		} else if !isChildContainer {
			// Large leaf - build as !snap-loc (not a range)
			if err := finalizeArrayRange(state, indexNode, chunks, offset); err != nil {
				return nil, err
			}
			if err := addDirectArrayChild(value, indexNode, threshold, chunks, offset); err != nil {
				return nil, err
			}
		} else {
			// Large container child - add to range or include directly
			// Check if we should finalize current range before adding this
			if len(state.rangeChildren) > 0 {
				// Estimate if adding this would exceed threshold
				currentRangeSize := int64(0)
				for _, v := range state.rangeChildren {
					sz, err := estimateNodeSize(v)
					if err != nil {
						return nil, err
					}
					currentRangeSize += sz
				}

				if currentRangeSize+childDataSize >= threshold {
					// Finalize current range before adding this
					if err := finalizeArrayRange(state, indexNode, chunks, offset); err != nil {
						return nil, err
					}
				}
			}

			// Start new range if needed
			if len(state.rangeChildren) == 0 {
				state.rangeStartIndex = len(indexNode.Values)
			}

			// Add to range
			if err := addToArrayRange(value, state); err != nil {
				return nil, err
			}
		}
	}

	// Finalize last range if any
	if err := finalizeArrayRange(state, indexNode, chunks, offset); err != nil {
		return nil, err
	}

	return indexNode, nil
}

func buildSmallContainerIndex(node *ir.Node, threshold int64, chunks *[]chunkInfo, offset *int64) (*ir.Node, error) {
	indexNode := &ir.Node{
		Type:   node.Type,
		Fields: make([]*ir.Node, len(node.Fields)),
		Values: make([]*ir.Node, len(node.Values)),
	}

	for i := 0; i < len(node.Fields); i++ {
		indexNode.Fields[i] = node.Fields[i].Clone()
		childIndex, err := buildIndexRecursive(node.Values[i], threshold, chunks, offset)
		if err != nil {
			return nil, err
		}
		indexNode.Values[i] = childIndex
	}

	return indexNode, nil
}

func buildRangeData(values []*ir.Node, fields []*ir.Node, isSparseArray bool) []byte {
	var buf bytes.Buffer
	if isSparseArray {
		// Build sparse array with numeric keys
		sparseMap := make(map[uint32]*ir.Node)
		for i, value := range values {
			if i >= len(fields) {
				break
			}
			field := fields[i]
			if field.Type == ir.NumberType && field.Int64 != nil {
				sparseMap[uint32(*field.Int64)] = value
			}
		}
		tempNode := ir.FromIntKeysMap(sparseMap)
		encode.Encode(tempNode, &buf)
	} else {
		// Regular object with string keys
		tempNode := ir.FromMap(map[string]*ir.Node{})
		for i, value := range values {
			if i >= len(fields) {
				break
			}
			tempNode.Fields = append(tempNode.Fields, fields[i])
			tempNode.Values = append(tempNode.Values, value)
		}
		encode.Encode(tempNode, &buf)
	}
	return buf.Bytes()
}

func buildArrayRangeData(children []*ir.Node) []byte {
	var buf bytes.Buffer
	encode.Encode(ir.FromSlice(children), &buf)
	return buf.Bytes()
}
