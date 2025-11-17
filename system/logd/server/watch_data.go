package server

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/system/logd/api"
)

// handleWatchData handles WATCH requests for data streaming.
func (s *Server) handleWatchData(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Extract path
	pathStr, err := extractPathString(body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, fmt.Sprintf("invalid path: %v", err)))
		return
	}

	// Validate path
	if err := validateDataPath(pathStr); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Extract fromSeq and toSeq from meta
	var fromSeq *int64
	var toSeq *int64
	if body.Meta != nil && body.Meta.Type == ir.ObjectType {
		for i, field := range body.Meta.Fields {
			if i >= len(body.Meta.Values) {
				continue
			}
			value := body.Meta.Values[i]
			switch field.String {
			case "fromSeq":
				if value != nil && value.Type == ir.NumberType && value.Int64 != nil {
					fromSeq = value.Int64
				}
			case "toSeq":
				if value != nil && value.Type == ir.NumberType && value.Int64 != nil {
					toSeq = value.Int64
				}
			}
		}
	}

	// Set up streaming response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)

	// Determine the starting commit count for snapshot lookup
	var snapshotTargetCommitCount int64
	if fromSeq != nil {
		snapshotTargetCommitCount = *fromSeq
	} else {
		// If no fromSeq, check if we have any diffs to determine where to start
		diffList, err := s.storage.ListDiffs(pathStr)
		if err != nil {
			writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to list diffs: %v", err)))
			return
		}
		if len(diffList) > 0 {
			snapshotTargetCommitCount = diffList[len(diffList)-1].CommitCount
		} else {
			snapshotTargetCommitCount = 0
		}
	}

	// Check for a snapshot to start from
	var snapshotCommitCount int64
	var snapshotState *ir.Node
	snapshotCommitCount, err = s.storage.FindNearestSnapshot(pathStr, snapshotTargetCommitCount)
	if err == nil && snapshotCommitCount > 0 {
		snapshot, err := s.storage.ReadSnapshot(pathStr, snapshotCommitCount)
		if err == nil {
			snapshotState = snapshot.State
		}
	}

	// List all diffs for this path
	diffList, err := s.storage.ListDiffs(pathStr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to list diffs: %v", err)))
		return
	}

	// Filter diffs based on fromSeq and toSeq, and exclude those covered by snapshot
	var diffsToStream []struct{ CommitCount, TxSeq int64 }
	for _, diff := range diffList {
		include := true
		// Skip diffs covered by snapshot
		if snapshotCommitCount > 0 && diff.CommitCount <= snapshotCommitCount {
			include = false
		}
		if fromSeq != nil && diff.CommitCount < *fromSeq {
			include = false
		}
		if toSeq != nil && diff.CommitCount > *toSeq {
			include = false
		}
		if include {
			diffsToStream = append(diffsToStream, diff)
		}
	}

	// Track the last commitCount we've streamed
	var lastCommitCount int64
	docCount := 0

	// Stream snapshot if we have one and it's within the requested range
	if snapshotState != nil {
		// Check if snapshot is within range
		shouldStreamSnapshot := true
		if fromSeq != nil && snapshotCommitCount < *fromSeq {
			shouldStreamSnapshot = false
		}
		if toSeq != nil && snapshotCommitCount > *toSeq {
			shouldStreamSnapshot = false
		}

		if shouldStreamSnapshot {
			// Stream snapshot as first document
			if err := s.streamSnapshot(w, pathStr, snapshotCommitCount, snapshotState, false); err != nil {
				return
			}
			docCount++
			lastCommitCount = snapshotCommitCount
		}
	}

	// Stream existing diffs (only those after snapshot)
	for _, diffInfo := range diffsToStream {
		if err := s.streamDiff(w, pathStr, diffInfo.CommitCount, diffInfo.TxSeq, docCount > 0); err != nil {
			return
		}
		docCount++
		lastCommitCount = diffInfo.CommitCount
	}

	// Update lastCommitCount if we didn't stream anything yet
	if docCount == 0 {
		if fromSeq != nil {
			lastCommitCount = *fromSeq - 1 // Start watching from the next one
		} else {
			// Get current state to know where to start watching
			state, err := s.storage.CurrentSeqState()
			if err != nil {
				writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to get current state: %v", err)))
				return
			}
			lastCommitCount = state.CommitCount
		}
	}

	// If toSeq is provided, we're done (history mode)
	if toSeq != nil {
		return
	}

	// Real-time watching: poll for new diffs
	ctx := r.Context()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected
			return
		case <-ticker.C:
			// Check for new diffs
			currentDiffs, err := s.storage.ListDiffs(pathStr)
			if err != nil {
				// Log error but continue watching
				continue
			}

			// Find new diffs (commitCount > lastCommitCount)
			// Since currentDiffs is sorted by commitCount, we can stream them in order
			// This ensures the client receives diffs in commitCount order, which is required
			// for correct state reconstruction
			var newDiffs []struct{ CommitCount, TxSeq int64 }
			for _, diff := range currentDiffs {
				if diff.CommitCount > lastCommitCount {
					if toSeq == nil || diff.CommitCount <= *toSeq {
						newDiffs = append(newDiffs, diff)
					}
				}
			}

			// Stream new diffs in commitCount order
			// Since currentDiffs is already sorted, newDiffs will be in order too
			for _, diffInfo := range newDiffs {
				// Ensure we don't skip any commitCounts - each diff must be streamed
				// in sequence for correct state reconstruction
				if err := s.streamDiff(w, pathStr, diffInfo.CommitCount, diffInfo.TxSeq, docCount > 0); err != nil {
					return
				}
				docCount++
				lastCommitCount = diffInfo.CommitCount

				// Check if we've reached toSeq
				if toSeq != nil && lastCommitCount >= *toSeq {
					return
				}
			}
		}
	}
}

// streamDiff streams a single diff document.
func (s *Server) streamDiff(w http.ResponseWriter, pathStr string, commitCount, txSeq int64, needSeparator bool) error {
	// Read the diff file
	diffFile, err := s.storage.ReadDiff(pathStr, commitCount, txSeq, false)
	if err != nil {
		return err
	}

	// Build document with meta and diff
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       &ir.Node{Type: ir.NumberType, Int64: &commitCount, Number: fmt.Sprintf("%d", commitCount)},
		"timestamp": &ir.Node{Type: ir.StringType, String: diffFile.Timestamp},
	})

	doc := ir.FromMap(map[string]*ir.Node{
		"meta": metaNode,
		"diff": diffFile.Diff,
	})

	// Write document separator if needed
	if needSeparator {
		if _, err := io.WriteString(w, "---\n"); err != nil {
			return err
		}
	}

	// Encode and write document
	if err := encode.Encode(doc, w); err != nil {
		return err
	}

	// Flush to ensure data is sent immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}

// streamSnapshot streams a snapshot document.
func (s *Server) streamSnapshot(w http.ResponseWriter, pathStr string, commitCount int64, state *ir.Node, needSeparator bool) error {
	// Read snapshot to get timestamp
	snapshot, err := s.storage.ReadSnapshot(pathStr, commitCount)
	if err != nil {
		return err
	}

	// Build document with meta and diff (snapshot state as diff from null)
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       &ir.Node{Type: ir.NumberType, Int64: &commitCount, Number: fmt.Sprintf("%d", commitCount)},
		"timestamp": &ir.Node{Type: ir.StringType, String: snapshot.Timestamp},
	})

	// Convert state to diff format (as insert operations from null)
	// For now, we'll represent the snapshot state directly as the diff
	// The client can reconstruct state by applying this as if it were a diff from null
	doc := ir.FromMap(map[string]*ir.Node{
		"meta": metaNode,
		"diff": state,
	})

	// Write document separator if needed
	if needSeparator {
		if _, err := io.WriteString(w, "---\n"); err != nil {
			return err
		}
	}

	// Encode and write document
	if err := encode.Encode(doc, w); err != nil {
		return err
	}

	// Flush to ensure data is sent immediately
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	return nil
}
