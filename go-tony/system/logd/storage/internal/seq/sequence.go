package seq

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	seqFile = "seq"
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
	Commit int64 // Monotonic commit count
	TxSeq  int64 // Transaction sequence number
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

// NextCommit atomically increments and returns the next commit count.
func (s *Seq) NextCommit() (int64, error) {
	s.Lock()
	defer s.Unlock()
	return s.NextCommitLocked()
}

func (s *Seq) NextCommitLocked() (int64, error) {
	state, err := s.ReadStateLocked()
	if err != nil {
		return 0, err
	}

	// Increment commit count
	state.Commit++

	// Write atomically
	if err := s.WriteStateLocked(state); err != nil {
		return 0, err
	}
	return state.Commit, nil
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
			return &State{Commit: 0, TxSeq: 0}, nil
		}
		return nil, err
	}

	if len(data) < seqFileSize {
		return nil, fmt.Errorf("invalid sequence file size: expected %d bytes, got %d", seqFileSize, len(data))
	}

	state := &State{
		Commit: int64(binary.LittleEndian.Uint64(data[commitCountOffset:])),
		TxSeq:  int64(binary.LittleEndian.Uint64(data[txSeqOffset:])),
	}

	// Mask to 56 bits (clear top 8 bits)
	state.Commit &= 0x00FFFFFFFFFFFFFF
	state.TxSeq &= 0x00FFFFFFFFFFFFFF

	return state, nil
}

// WriteStateLocked writes the sequence state to disk atomically.
// Caller must hold seqMu lock.
func (s *Seq) WriteStateLocked(state *State) error {
	file := filepath.Join(s.Root, "meta", seqFile)

	// Ensure values fit in 56 bits
	state.Commit &= 0x00FFFFFFFFFFFFFF
	state.TxSeq &= 0x00FFFFFFFFFFFFFF

	// Write to temp file first, then rename atomically
	tmpFile := file + ".tmp"

	data := make([]byte, seqFileSize)
	binary.LittleEndian.PutUint64(data[commitCountOffset:], uint64(state.Commit))
	binary.LittleEndian.PutUint64(data[txSeqOffset:], uint64(state.TxSeq))

	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpFile, file); err != nil {
		os.Remove(tmpFile) // Clean up on error
		return err
	}
	return nil
}
