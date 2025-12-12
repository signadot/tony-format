package snap

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func (s *snapshotImpl) Load(indexNode *ir.Node) (*ir.Node, error) {
	if ir.TagHas(indexNode.Tag, "!snap-loc") {
		return s.loadSnapLoc(indexNode)
	}
	if ir.TagHas(indexNode.Tag, "!snap-range") {
		return s.loadSnapRange(indexNode)
	}
	return nil, fmt.Errorf("node is not a !snap-loc or !snap-range node")
}

func (s *snapshotImpl) loadSnapLoc(indexNode *ir.Node) (*ir.Node, error) {
	// !snap-loc [offset size] - array with two int64 values
	if indexNode.Type != ir.ArrayType {
		return nil, fmt.Errorf("!snap-loc node must be array with [offset, size]")
	}
	if len(indexNode.Values) != 2 {
		return nil, fmt.Errorf("!snap-loc node must be array with [offset, size]")
	}

	offsetNode := indexNode.Values[0]
	sizeNode := indexNode.Values[1]

	if offsetNode.Int64 == nil {
		return nil, fmt.Errorf("!snap-loc offset and size must be int64")
	}
	if sizeNode.Int64 == nil {
		return nil, fmt.Errorf("!snap-loc offset and size must be int64")
	}

	offset := *offsetNode.Int64
	size := *sizeNode.Int64

	// Read data from reader
	data := make([]byte, size)
	_, err := s.reader.ReadAt(data, s.readerPos+offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read snap-loc data: %w", err)
	}

	// Parse the data as an ir.Node
	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snap-loc data: %w", err)
	}

	return node, nil
}

func (s *snapshotImpl) loadSnapRange(indexNode *ir.Node) (*ir.Node, error) {
	// !snap-range(from,to) [offset size] - tag with (from,to) args and array with [offset, size]
	// Parse tag arguments for (from, to) - these indicate which children are in the range
	_, args, _ := ir.TagArgs(indexNode.Tag)
	if len(args) != 2 {
		return nil, fmt.Errorf("!snap-range tag must have (from, to) arguments")
	}

	// Parse array [offset, size] - these indicate where the data is stored
	if indexNode.Type != ir.ArrayType {
		return nil, fmt.Errorf("!snap-range node must be array with [offset, size]")
	}
	if len(indexNode.Values) != 2 {
		return nil, fmt.Errorf("!snap-range node must be array with [offset, size]")
	}

	offsetNode := indexNode.Values[0]
	sizeNode := indexNode.Values[1]

	if offsetNode.Int64 == nil {
		return nil, fmt.Errorf("!snap-range offset and size must be int64")
	}
	if sizeNode.Int64 == nil {
		return nil, fmt.Errorf("!snap-range offset and size must be int64")
	}

	offset := *offsetNode.Int64
	size := *sizeNode.Int64

	// Read data from reader
	data := make([]byte, size)
	_, err := s.reader.ReadAt(data, s.readerPos+offset)
	if err != nil {
		return nil, fmt.Errorf("failed to read snap-range data: %w", err)
	}

	// Parse the data as an ir.Node
	node, err := parse.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse snap-range data: %w", err)
	}

	return node, nil
}
