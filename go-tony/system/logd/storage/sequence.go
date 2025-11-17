package storage

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
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

// SeqState represents the sequence number state.
type SeqState struct {
	CommitCount int64 // Monotonic commit count
	TxSeq       int64 // Transaction sequence number
}

// NextTxSeq atomically increments and returns the next transaction sequence number.
func (s *Storage) NextTxSeq() (int64, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	
	state, err := s.readSeqStateLocked()
	if err != nil {
		return 0, err
	}
	
	// Increment transaction seq
	state.TxSeq++
	
	// Write atomically
	if err := s.writeSeqStateLocked(state); err != nil {
		return 0, err
	}
	
	return state.TxSeq, nil
}

// NextCommitCount atomically increments and returns the next commit count.
func (s *Storage) NextCommitCount() (int64, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	
	state, err := s.readSeqStateLocked()
	if err != nil {
		return 0, err
	}
	
	// Increment commit count
	state.CommitCount++
	
	// Write atomically
	if err := s.writeSeqStateLocked(state); err != nil {
		return 0, err
	}
	
	return state.CommitCount, nil
}

// CurrentSeqState returns the current sequence state without incrementing.
func (s *Storage) CurrentSeqState() (*SeqState, error) {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	return s.readSeqStateLocked()
}

// readSeqState reads the sequence state from disk.
// Caller must hold seqMu lock.
func (s *Storage) readSeqStateLocked() (*SeqState, error) {
	file := filepath.Join(s.root, "meta", seqFile)
	
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, start at 0
			return &SeqState{CommitCount: 0, TxSeq: 0}, nil
		}
		return nil, err
	}
	
	if len(data) < seqFileSize {
		return nil, fmt.Errorf("invalid sequence file size: expected %d bytes, got %d", seqFileSize, len(data))
	}
	
	state := &SeqState{
		CommitCount: int64(binary.LittleEndian.Uint64(data[commitCountOffset:])),
		TxSeq:       int64(binary.LittleEndian.Uint64(data[txSeqOffset:])),
	}
	
	// Mask to 56 bits (clear top 8 bits)
	state.CommitCount &= 0x00FFFFFFFFFFFFFF
	state.TxSeq &= 0x00FFFFFFFFFFFFFF
	
	return state, nil
}

// writeSeqStateLocked writes the sequence state to disk atomically.
// Caller must hold seqMu lock.
func (s *Storage) writeSeqStateLocked(state *SeqState) error {
	file := filepath.Join(s.root, "meta", seqFile)
	
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
	
	return nil
}
