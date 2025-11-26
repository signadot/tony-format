package storage

import (
	"fmt"
	"strconv"
	"strings"
)

// formatDiffFilename formats a diff filename.
// For pending files (ext="pending"): {txSeq}.pending
// For committed files (ext="diff"): {commitCount}-{txSeq}.diff
func formatDiffFilename(commitCount, txSeq int64, ext string) string {
	if ext == "pending" {
		// Pending files don't have commit count prefix
		return fmt.Sprintf("%s.%s", FormatLexInt(txSeq), ext)
	}
	// Committed files have commit count prefix
	return fmt.Sprintf("%s-%s.%s", FormatLexInt(commitCount), FormatLexInt(txSeq), ext)
}

// parseDiffFilename parses a diff filename to extract commitCount and txSeq.
// Handles both formats: {txSeq}.pending and {commitCount}-{txSeq}.diff
func parseDiffFilename(filename string) (commitCount, txSeq int64, ext string, err error) {
	// Remove extension
	parts := strings.Split(filename, ".")
	if len(parts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid filename format: expected 'txSeq.pending' or 'commitCount-txSeq.diff'")
	}
	ext = parts[1]

	// Check if it's a pending file (no commit count prefix)
	if ext == "pending" {
		txSeq, err = ParseLexInt(parts[0])
		if err != nil {
			return 0, 0, "", fmt.Errorf("invalid transaction seq: %w", err)
		}
		return 0, txSeq, ext, nil
	}

	// For .diff files, split commitCount and txSeq
	seqParts := strings.Split(parts[0], "-")
	if len(seqParts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid filename format: expected 'commitCount-txSeq.diff'")
	}

	commitCount, err = ParseLexInt(seqParts[0])
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid commit count: %w", err)
	}

	txSeq, err = ParseLexInt(seqParts[1])
	if err != nil {
		return 0, 0, "", fmt.Errorf("invalid transaction seq: %w", err)
	}

	return commitCount, txSeq, ext, nil
}

func FormatLexInt(v int64) string {
	d := strconv.FormatUint(uint64(v), 10)
	prefix := rune('a' + len(d) - 1)
	return string(prefix) + d
}

func ParseLexInt(v string) (int64, error) {
	if len(v) < 2 {
		return 0, fmt.Errorf("%q too short", v)
	}
	if 'a' <= v[0] && v[0] <= 's' {
		u, err := strconv.ParseUint(v[1:], 10, 64)
		if err != nil {
			return 0, err
		}
		return int64(u), nil

	}
	return 0, fmt.Errorf("invalid leading character in %q, expecting a-s", v)
}
