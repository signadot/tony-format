package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handleWatchData handles WATCH requests for data streaming.
func (s *Server) handleWatchData(w http.ResponseWriter, r *http.Request, body *api.RequestBody) {
	// Validate path
	if err := validateDataPath(body.Path); err != nil {
		writeError(w, http.StatusBadRequest, api.NewError(api.ErrCodeInvalidPath, err.Error()))
		return
	}

	// Extract fromSeq and toSeq from meta
	var fromSeq *int64
	var toSeq *int64
	if body.Meta != nil {
		if fromSeqNode, ok := body.Meta["fromSeq"]; ok {
			if fromSeqNode.Type == ir.NumberType && fromSeqNode.Int64 != nil {
				fromSeq = fromSeqNode.Int64
			}
		}
		if toSeqNode, ok := body.Meta["toSeq"]; ok {
			if toSeqNode.Type == ir.NumberType && toSeqNode.Int64 != nil {
				toSeq = toSeqNode.Int64
			}
		}
	}

	// Set up streaming response
	w.Header().Set("Content-Type", "application/x-tony")
	w.WriteHeader(http.StatusOK)

	// Stream historical diffs (if fromSeq is provided or if there are existing diffs)
	// Use listAllRelevantCommitCounts to include child path diffs for hierarchical watching
	allDiffs, err := s.listAllRelevantCommitCounts(body.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to list diffs: %v", err)))
		return
	}

	var diffsToStream []struct{ CommitCount, TxSeq int64 }
	for _, diffInfo := range allDiffs {
		if fromSeq == nil || diffInfo.CommitCount >= *fromSeq {
			if toSeq == nil || diffInfo.CommitCount <= *toSeq {
				diffsToStream = append(diffsToStream, diffInfo)
			}
		}
	}

	// Track the last commitCount we've streamed
	var lastCommitCount int64
	docCount := 0

	// Stream existing diffs
	for _, diffInfo := range diffsToStream {
		if err := s.streamDiff(w, body.Path, diffInfo.CommitCount, diffInfo.TxSeq, docCount > 0); err != nil {
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
			state, err := s.Config.Storage.CurrentSeqState()
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
			// Check for new diffs (including from child paths)
			currentDiffs, err := s.listAllRelevantCommitCounts(body.Path)
			if err != nil {
				s.Config.Log.Error("error listing relevant commits", "error", err)
				// Log error but continue watching
				continue
			}
			s.Config.Log.Debug("listed relevant", "n", len(currentDiffs))

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
				if err := s.streamDiff(w, body.Path, diffInfo.CommitCount, diffInfo.TxSeq, docCount > 0); err != nil {
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
	// Read the direct diff file (may not exist if only children have diffs)
	var diffNode *ir.Node
	var timestamp string

	diffFile, err := s.Config.Storage.ReadDiff(pathStr, commitCount, txSeq, false)
	if err == nil {
		diffNode = diffFile.Diff
		timestamp = diffFile.Timestamp
		//fmt.Fprintf(os.Stderr, "streamDiff: ReadDiff success for %s c%d t%d. Sparse: %v\n", pathStr, commitCount, txSeq, storage.HasSparseArrayTag(diffNode))
	} else {
		// No direct diff, use current timestamp
		timestamp = ""
		fmt.Fprintf(os.Stderr, "streamDiff: ReadDiff failed for %s c%d t%d: %v\n", pathStr, commitCount, txSeq, err)
	}

	// Aggregate child diffs hierarchically
	childDiff, err := s.aggregateChildDiffs(pathStr, commitCount)
	if err != nil {
		// Silently continue with just the direct diff on error
		childDiff = nil
		fmt.Fprintf(os.Stderr, "streamDiff: aggregateChildDiffs failed for %s c%d: %v\n", pathStr, commitCount, err)
	} else if childDiff != nil {
		//fmt.Fprintf(os.Stderr, "streamDiff: childDiff found for %s c%d. Sparse: %v\n", pathStr, commitCount, storage.HasSparseArrayTag(childDiff))
	}

	// Merge direct and child diffs
	finalDiff, err := mergeDiffs(diffNode, childDiff)
	if err != nil {
		fmt.Fprintf(os.Stderr, "streamDiff: mergeDiffs failed: %v\n", err)
		return err
	}

	// If no diff at all (neither direct nor children), skip streaming
	if finalDiff == nil {
		return nil
	}

	// Build document with meta and diff
	metaNode := ir.FromMap(map[string]*ir.Node{
		"seq":       &ir.Node{Type: ir.NumberType, Int64: &commitCount, Number: fmt.Sprintf("%d", commitCount)},
		"timestamp": &ir.Node{Type: ir.StringType, String: timestamp},
	})

	doc := ir.FromMap(map[string]*ir.Node{
		"meta": metaNode,
		"diff": finalDiff,
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
