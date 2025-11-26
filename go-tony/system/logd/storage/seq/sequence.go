package seq

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

const (
	seqFile      = "seq"
	pathSeqsFile = "path-seqs.tony"
	// Fixed width offsets in binary file:
	// Offset 0-7: commit count (8 bytes, little-endian int64, using 56 bits)
	// Offset 8-15: transaction seq (8 bytes, little-endian int64, using 56 bits)
	commitCountOffset = 0
	txSeqOffset       = 8
	seqFileSize       = 16
)

type Seq struct {
	sync.Mutex
	Root string
}

func NewSeq(root string) *Seq {
	return &Seq{Root: root}
}

// State represents the sequence number state.
type State struct {
	CommitCount int64            // Monotonic commit count
	TxSeq       int64            // Transaction sequence number
	PathSeqs    map[string]int64 // Per-path last commit seq
}

// NextTxSeq atomically increments and returns the next transaction sequence number.
func (s *Seq) NextTxSeq() (int64, error) {
	s.Lock()
	defer s.Unlock()

	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, err
	}

	// Increment transaction seq
	state.TxSeq++

	// Write atomically
	if err := s.WriteStateLocked(state); err != nil {
		return 0, err
	}

	return state.TxSeq, nil
}

// NextCommitCount atomically increments and returns the next commit count.
func (s *Seq) NextCommitCount() (int64, error) {
	s.Lock()
	defer s.Unlock()

	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, err
	}

	// Increment commit count
	state.CommitCount++

	// Write atomically
	if err := s.WriteStateLocked(state); err != nil {
		return 0, err
	}

	return state.CommitCount, nil
}

// NextCommitCountIfPathSeqMatches conditionally increments the commit count
// only if the current sequence for the given path matches expectedSeq.
// Returns (commitCount, matched, error).
// If expectedSeq is nil, always increments (unconditional write).
func (s *Seq) NextCommitCountIfPathSeqMatches(
	path string,
	expectedSeq *int64,
) (int64, bool, error) {
	s.Lock()
	defer s.Unlock()

	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, false, err
	}

	// Check path seq if expected value provided
	if expectedSeq != nil {
		currentSeq, exists := state.PathSeqs[path]
		if !exists {
			currentSeq = -1 // Path never written
		}
		if currentSeq != *expectedSeq {
			return 0, false, nil // Not matched
		}
	}

	// Increment commit count
	state.CommitCount++

	// Update path seq
	if state.PathSeqs == nil {
		state.PathSeqs = make(map[string]int64)
	}
	state.PathSeqs[path] = state.CommitCount

	// Write atomically
	if err := s.WriteStateLocked(state); err != nil {
		return 0, false, err
	}

	return state.CommitCount, true, nil
}

// GetPathSeq returns the last commit sequence for a given path.
// Returns -1 if the path has never been written.
func (s *Seq) GetPathSeq(path string) (int64, error) {
	s.Lock()
	defer s.Unlock()

	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, err
	}

	if seq, exists := state.PathSeqs[path]; exists {
		return seq, nil
	}
	return -1, nil
}

// CurrentSeqState returns the current sequence state without incrementing.
func (s *Seq) CurrentSeqState() (*State, error) {
	s.Lock()
	defer s.Unlock()
	return s.ReadStateLocked()
}

// readSeqState reads the sequence state from disk.
// Caller must hold seqMu lock.
func (s *Seq) ReadStateLocked() (*State, error) {
	file := filepath.Join(s.Root, "meta", seqFile)

	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, start at 0
			return &State{CommitCount: 0, TxSeq: 0, PathSeqs: make(map[string]int64)}, nil
		}
		return nil, err
	}

	if len(data) < seqFileSize {
		return nil, fmt.Errorf("invalid sequence file size: expected %d bytes, got %d", seqFileSize, len(data))
	}

	state := &State{
		CommitCount: int64(binary.LittleEndian.Uint64(data[commitCountOffset:])),
		TxSeq:       int64(binary.LittleEndian.Uint64(data[txSeqOffset:])),
		PathSeqs:    make(map[string]int64),
	}

	// Mask to 56 bits (clear top 8 bits)
	state.CommitCount &= 0x00FFFFFFFFFFFFFF
	state.TxSeq &= 0x00FFFFFFFFFFFFFF

	// Read path-seqs.tony if it exists
	pathSeqsFilePath := filepath.Join(s.Root, "meta", pathSeqsFile)
	pathSeqsData, err := os.ReadFile(pathSeqsFilePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read path-seqs file: %w", err)
	}

	if len(pathSeqsData) > 0 {
		node, err := parse.Parse(pathSeqsData)
		if err != nil {
			return nil, fmt.Errorf("failed to parse path-seqs.tony: %w", err)
		}
		state.PathSeqs = nodeToPathSeqs(node)
	}

	return state, nil
}

// WriteStateLocked writes the sequence state to disk atomically.
// Caller must hold seqMu lock.
func (s *Seq) WriteStateLocked(state *State) error {
	file := filepath.Join(s.Root, "meta", seqFile)

	// Ensure values fit in 56 bits
	state.CommitCount &= 0x00FFFFFFFFFFFFFF
	state.TxSeq &= 0x00FFFFFFFFFFFFFF

	// Write to temp file first, then rename atomically
	tmpFile := file + ".tmp"

	data := make([]byte, seqFileSize)
	binary.LittleEndian.PutUint64(data[commitCountOffset:], uint64(state.CommitCount))
	binary.LittleEndian.PutUint64(data[txSeqOffset:], uint64(state.TxSeq))

	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, file); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}

	// Write path-seqs.tony
	pathSeqsFilePath := filepath.Join(s.Root, "meta", pathSeqsFile)
	pathSeqsTmpFile := pathSeqsFilePath + ".tmp"

	node := pathSeqsToNode(state.PathSeqs)

	// Encode to buffer
	var buf []byte
	if len(state.PathSeqs) > 0 {
		// Use encode package to write Tony format
		tmpFile, err := os.Create(pathSeqsTmpFile)
		if err != nil {
			return fmt.Errorf("failed to create path-seqs temp file: %w", err)
		}

		if err := encode.Encode(node, tmpFile); err != nil {
			tmpFile.Close()
			os.Remove(pathSeqsTmpFile)
			return fmt.Errorf("failed to encode path-seqs: %w", err)
		}

		if err := tmpFile.Close(); err != nil {
			os.Remove(pathSeqsTmpFile)
			return fmt.Errorf("failed to close path-seqs temp file: %w", err)
		}
	} else {
		// Write empty file for empty map
		if err := os.WriteFile(pathSeqsTmpFile, buf, 0644); err != nil {
			return fmt.Errorf("failed to write empty path-seqs file: %w", err)
		}
	}

	if err := os.Rename(pathSeqsTmpFile, pathSeqsFilePath); err != nil {
		os.Remove(pathSeqsTmpFile)
		return fmt.Errorf("failed to rename path-seqs file: %w", err)
	}

	return nil
}

// nodeToPathSeqs converts a Tony IR node to a map[string]int64.
// Expects a flat object with string keys and int values.
func nodeToPathSeqs(node *ir.Node) map[string]int64 {
	if node == nil || node.Type != ir.ObjectType {
		return make(map[string]int64)
	}

	result := make(map[string]int64, len(node.Fields))
	for i, field := range node.Fields {
		if field.Type != ir.StringType {
			continue
		}
		path := field.String
		value := node.Values[i]
		if value.Type == ir.NumberType && value.Int64 != nil {
			result[path] = *value.Int64
		}
	}
	return result
}

// pathSeqsToNode converts a map[string]int64 to a Tony IR node.
// Creates a flat object with string keys and int values.
func pathSeqsToNode(pathSeqs map[string]int64) *ir.Node {
	if len(pathSeqs) == 0 {
		return ir.FromMap(map[string]*ir.Node{})
	}

	m := make(map[string]*ir.Node, len(pathSeqs))
	for path, seq := range pathSeqs {
		m[path] = ir.FromInt(seq)
	}
	return ir.FromMap(m)
}
