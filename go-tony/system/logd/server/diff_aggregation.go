package server

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// aggregateChildDiffs builds a hierarchical diff for a parent path
// by combining diffs from all child paths at the given commitCount.
// Returns nil if no child diffs exist.
func (s *Server) aggregateChildDiffs(pathStr string, commitCount int64) (*ir.Node, error) {
	// 1. List all child paths
	children, err := s.storage.ListChildPaths(pathStr)
	if err != nil {
		return nil, err
	}

	if len(children) == 0 {
		return nil, nil
	}

	// 2. Check if this path is a sparse array
	meta, _ := s.storage.ReadPathMetadata(pathStr)
	isSparseArray := meta != nil && meta.IsSparseArray

	// 3. Build aggregated diff
	var aggregated map[string]*ir.Node
	var uint32Keys map[uint32]*ir.Node

	if isSparseArray {
		uint32Keys = make(map[uint32]*ir.Node)
	} else {
		aggregated = make(map[string]*ir.Node)
	}

	// 4. Process each child
	for _, childPath := range children {
		// Extract the child segment from path
		childSegment := extractLastSegment(childPath)

		// Check if child has diff at this commitCount
		childDiff, err := s.readChildDiffAtCommit(childPath, commitCount)
		if err != nil || childDiff == nil {
			continue
		}

		// Add to aggregated diff
		if isSparseArray {
			// Parse segment as uint32
			if key, err := parseUint32(childSegment); err == nil {
				uint32Keys[key] = childDiff
			}
			// Silently skip invalid uint32 keys in sparse arrays
		} else {
			aggregated[childSegment] = childDiff
		}
	}

	// 5. Build result node
	if isSparseArray && len(uint32Keys) > 0 {
		return ir.FromIntKeysMap(uint32Keys), nil
	} else if len(aggregated) > 0 {
		return ir.FromMap(aggregated), nil
	}

	return nil, nil
}

// readChildDiffAtCommit reads the diff for a child path at a specific commitCount.
// This recursively aggregates the child's own children as well.
func (s *Server) readChildDiffAtCommit(childPath string, commitCount int64) (*ir.Node, error) {
	// Find the diff file for this path at the given commitCount
	diffs, err := s.storage.ListDiffs(childPath)
	if err != nil {
		return nil, err
	}

	// Look for exact commitCount match
	var txSeq int64
	found := false
	for _, d := range diffs {
		if d.CommitCount == commitCount {
			txSeq = d.TxSeq
			found = true
			break
		}
	}

	if !found {
		// No direct diff at this commitCount, but check for child diffs
		return s.aggregateChildDiffs(childPath, commitCount)
	}

	// Read the direct diff
	diffFile, err := s.storage.ReadDiff(childPath, commitCount, txSeq, false)
	if err != nil {
		return nil, err
	}

	// Aggregate child diffs hierarchically
	childDiff, err := s.aggregateChildDiffs(childPath, commitCount)
	if err != nil {
		// Silently continue with just the direct diff on error
		childDiff = nil
	}

	// Merge direct diff with child diffs
	return mergeDiffs(diffFile.Diff, childDiff)
}

// mergeDiffs combines a direct diff with aggregated child diffs.
// If both are objects, merges their fields.
// Returns error if trying to merge incompatible types (e.g. sparse array vs regular object).
func mergeDiffs(direct, children *ir.Node) (*ir.Node, error) {
	if direct == nil {
		return children, nil
	}
	if children == nil {
		return direct, nil
	}

	// Both exist - need to merge
	// If both are objects, merge fields
	if direct.Type == ir.ObjectType && children.Type == ir.ObjectType {
		// Check for sparse array tag consistency
		baseIsSparse := storage.HasSparseArrayTag(direct)
		childIsSparse := storage.HasSparseArrayTag(children)

		if baseIsSparse != childIsSparse {
			return nil, fmt.Errorf("cannot merge sparse array and regular object: direct sparse=%v, children sparse=%v", baseIsSparse, childIsSparse)
		}

		// Build combined map
		combined := make(map[string]*ir.Node)

		// Add direct fields
		for i, field := range direct.Fields {
			if i < len(direct.Values) {
				combined[field.String] = direct.Values[i]
			}
		}

		// Add/override with children fields
		for i, field := range children.Fields {
			if i < len(children.Values) {
				combined[field.String] = children.Values[i]
			}
		}

		// Check if we should use sparse array format
		if baseIsSparse {
			// Convert to uint32 keys
			uint32Map := make(map[uint32]*ir.Node)
			for k, v := range combined {
				if key, err := strconv.ParseUint(k, 10, 32); err == nil {
					uint32Map[uint32(key)] = v
				}
			}
			return ir.FromIntKeysMap(uint32Map), nil
		}

		return ir.FromMap(combined), nil
	}

	// Can't merge different types - direct takes precedence
	return direct, nil
}

// extractLastSegment extracts the last path segment from a path.
// Example: "/root/child" -> "child"
func extractLastSegment(path string) string {
	// Remove trailing slash if present
	path = strings.TrimSuffix(path, "/")

	// Find last slash
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return path
	}

	return path[idx+1:]
}

// parseUint32 parses a string as a uint32.
func parseUint32(s string) (uint32, error) {
	val, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(val), nil
}

// buildNestedDiff wraps a child diff in parent structure.
// Example: parentPath="/root", childPath="/root/sparse/123", diff={foo: "bar"}
// Returns: {sparse: {123: {foo: "bar"}}} with correct types
func (s *Server) buildNestedDiff(parentPath, childPath string, childDiff *ir.Node) (*ir.Node, error) {
	// Get path difference
	segments := getPathDifference(parentPath, childPath)
	if len(segments) == 0 {
		return childDiff, nil
	}

	// Build nested structure bottom-up
	result := childDiff
	currentPath := childPath

	for i := len(segments) - 1; i >= 0; i-- {
		segment := segments[i]

		// Get parent of current segment
		parentOfSegment := filepath.Dir(currentPath)

		// Check if parent is sparse array
		meta, _ := s.storage.ReadPathMetadata(parentOfSegment)
		isSparseArray := meta != nil && meta.IsSparseArray

		if isSparseArray {
			// Use uint32 key
			if key, err := parseUint32(segment); err == nil {
				result = ir.FromIntKeysMap(map[uint32]*ir.Node{key: result})
			} else {
				// Fallback to string key
				result = ir.FromMap(map[string]*ir.Node{segment: result})
			}
		} else {
			// Use string key
			result = ir.FromMap(map[string]*ir.Node{segment: result})
		}

		currentPath = parentOfSegment
	}

	return result, nil
}

// getPathDifference returns the path segments between parent and child.
// Example: parent="/root", child="/root/sparse/123" -> ["sparse", "123"]
func getPathDifference(parent, child string) []string {
	// Normalize paths
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)

	// Get relative path
	rel, err := filepath.Rel(parent, child)
	if err != nil || rel == "." {
		return nil
	}

	// Split into segments
	return strings.Split(rel, string(filepath.Separator))
}

// listAllRelevantCommitCounts returns all commitCounts that affect a path,
// including diffs from child paths. This is used for hierarchical watching.
func (s *Server) listAllRelevantCommitCounts(pathStr string) ([]struct{ CommitCount, TxSeq int64 }, error) {
	return s.listAllRelevantCommitCountsWithDepth(pathStr, 0, 10)
}

// listAllRelevantCommitCountsWithDepth is the internal implementation with depth limiting.
func (s *Server) listAllRelevantCommitCountsWithDepth(pathStr string, depth, maxDepth int) ([]struct{ CommitCount, TxSeq int64 }, error) {

	// Get direct diffs at this path
	directDiffs, err := s.storage.ListDiffs(pathStr)
	if err != nil {
		return nil, err
	}

	// Collect all commit/txSeq pairs (don't deduplicate yet - we need all txSeq values)
	var allDiffs []struct{ CommitCount, TxSeq int64 }
	for _, d := range directDiffs {
		allDiffs = append(allDiffs, struct{ CommitCount, TxSeq int64 }{d.CommitCount, d.TxSeq})
	}

	// Get child paths and their diffs
	children, err := s.storage.ListChildPaths(pathStr)
	if err == nil && len(children) > 0 {
		for _, childPath := range children {
			childDiffs, err := s.listAllRelevantCommitCountsWithDepth(childPath, depth+1, maxDepth)
			if err != nil {
				continue
			}
			// Add child diffs, but only if we don't already have a direct diff at that commit count
			for _, d := range childDiffs {
				// Check if we have a direct diff at this commitCount
				hasDirect := false
				for _, direct := range directDiffs {
					if direct.CommitCount == d.CommitCount {
						hasDirect = true
						break
					}
				}
				// Only add child diff if no direct diff exists at this commitCount
				if !hasDirect {
					allDiffs = append(allDiffs, d)
				}
			}
		}
	}

	// Sort by commitCount, then by txSeq
	for i := 0; i < len(allDiffs); i++ {
		for j := i + 1; j < len(allDiffs); j++ {
			if allDiffs[i].CommitCount > allDiffs[j].CommitCount ||
				(allDiffs[i].CommitCount == allDiffs[j].CommitCount && allDiffs[i].TxSeq > allDiffs[j].TxSeq) {
				allDiffs[i], allDiffs[j] = allDiffs[j], allDiffs[i]
			}
		}
	}

	return allDiffs, nil
}
