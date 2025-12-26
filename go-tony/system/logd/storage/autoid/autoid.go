package autoid

import (
	"math/bits"
	"strconv"
)

// Generate returns a monotonic ID for the given commit and index.
// IDs sort lexicographically in commit order.
//
// Format: FormatLex encoding - length-prefixed hexadecimal numbers.
// The length prefix is a letter: 'a'=1 hex digit, 'b'=2 hex digits, ..., 'p'=16 hex digits.
// This ensures lexicographic ordering matches numeric ordering.
//
// Examples:
//   - commit=1, index=0    → "a1a0"
//   - commit=16, index=0   → "b10a0"
//   - commit=255, index=0  → "bffa0"
//   - commit=256, index=5  → "c100a5"
//   - commit=1000000, index=99 → "ef4240b63"
//
// Sorting: "a1a0" < "a1a1" < "afa0" < "b10a0" < "c100a5"
func Generate(commit int64, index int) string {
	return formatLex(commit) + formatLex(int64(index))
}

// hexDigits returns the number of hex digits needed to represent n.
// Uses bits.Len64 for efficient calculation.
func hexDigits(n uint64) int {
	if n == 0 {
		return 1
	}
	// bits.Len64 gives bit length, divide by 4 (rounding up) for hex digits
	return (bits.Len64(n) + 3) / 4
}

// formatLex encodes a non-negative integer using length-prefixed hex format.
// The length prefix is a letter where 'a'=1 hex digit, 'b'=2 hex digits, etc.
// This ensures lexicographic sorting matches numeric sorting.
func formatLex(n int64) string {
	if n < 0 {
		panic("formatLex: negative numbers not supported")
	}

	length := hexDigits(uint64(n))
	prefix := byte('a' + length - 1)
	return string(prefix) + strconv.FormatInt(n, 16)
}
