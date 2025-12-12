package snap

import (
	"github.com/signadot/tony-format/go-tony/ir"
	"io"
)

const (
	// DefaultChunkThreshold is the default size threshold (4KB) for chunking containers
	DefaultChunkThreshold = 4096
)

// ReaderAtCloser combines io.ReaderAt and io.Closer
type ReaderAtCloser interface {
	io.ReaderAt
	io.Closer
}

type Snapshot interface {
	Index() (*ir.Node, error)
	// Load loads a !snap-loc or !snap-range node
	Load(*ir.Node) (*ir.Node, error)
	Close() error
}

// snapshotImpl is the concrete implementation of Snapshot
type snapshotImpl struct {
	indexNode *ir.Node
	reader    io.ReaderAt
	readerPos int64 // Position where data starts (after index)
}

func (s *snapshotImpl) Index() (*ir.Node, error) {
	return s.indexNode.Clone(), nil
}

func (s *snapshotImpl) Close() error {
	if closer, ok := s.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
