package server

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// handleWatchData handles WATCH requests for data streaming.
func (s *Server) handleWatchData(w http.ResponseWriter, r *http.Request, body *api.Body) {
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
	diffsToStream, err := s.listAllRelevantCommitCounts(body.Path, fromSeq, toSeq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, api.NewError("storage_error", fmt.Sprintf("failed to list diffs: %v", err)))
		return
	}

	// Track the last commitCount we've streamed
	var lastCommitCount int64
	docCount := 0

	// Stream historical diffs first
	for _, seg := range diffsToStream {
		if err := s.streamDiff(w, body.Path, seg.StartCommit, seg.StartTx, docCount > 0); err != nil {
			return
		}
		docCount++
		lastCommitCount = seg.StartCommit

		// Check if we've reached toSeq
		if toSeq != nil && lastCommitCount >= *toSeq {
			return
		}
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
			// Only get diffs from lastCommitCount+1 onwards
			from := lastCommitCount + 1
			currentDiffs, err := s.listAllRelevantCommitCounts(body.Path, &from, toSeq)
			if err != nil {
				s.Config.Log.Error("error listing relevant commits", "error", err)
				// Log error but continue watching
				continue
			}
			s.Config.Log.Debug("listed relevant", "n", len(currentDiffs))

			// Stream any new diffs
			for _, seg := range currentDiffs {
				// Stream this diff (we already filtered by commit range above)
				if err := s.streamDiff(w, body.Path, seg.StartCommit, seg.StartTx, docCount > 0); err != nil {
					return
				}
				docCount++
				lastCommitCount = seg.StartCommit

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
	} else {
		// No direct diff, use current timestamp
		timestamp = ""
	}

	// Aggregate child diffs hierarchically
	childDiff, err := s.aggregateChildDiffs(pathStr, commitCount)
	if err != nil {
		// Silently continue with just the direct diff on error
		childDiff = nil
	}

	// Merge direct and child diffs
	finalDiff, err := mergeDiffs(diffNode, childDiff)
	if err != nil {
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
