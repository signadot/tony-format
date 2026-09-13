package lsp

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// The protocol's positions are a line and a column in UTF-16 code units: its
// default encoding, and the only one this server speaks (it negotiates none). The
// tokenizer's columns are bytes (token.PosDoc.LineCol), and the token search in
// semantic_tokens.go works in runes. Three units for one column, and the sync
// mixed them: a byte offset was used as a rune index, so an edit after a
// multi-byte character landed in the wrong place, and formatting wrote the
// server's corrupted copy back over the editor's (4ynqp7wqh12krg32msn0 item 28).
// Everything that meets a protocol position converts here.

// lineStart is the byte offset of line's first byte, or the document's end past
// its last line.
func lineStart(content string, line int) int {
	off := 0
	for l := 0; l < line; l++ {
		i := strings.IndexByte(content[off:], '\n')
		if i < 0 {
			return len(content)
		}
		off += i + 1
	}
	return off
}

// lineAt is the text of line without its newline.
func lineAt(content string, line int) string {
	rest := content[lineStart(content, line):]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// byteOffset is the byte offset in content of the protocol position (line, col).
// A column past the line's end is the line's end, and a line past the last is the
// document's end, which is where an insert at the end of the document lands.
func byteOffset(content string, line, col int) int {
	return lineStart(content, line) + byteCol(lineAt(content, line), col)
}

// units is the width of r in UTF-16 code units; a rune UTF-16 cannot encode is
// written as one replacement unit.
func units(r rune) int {
	if u := utf16.RuneLen(r); u > 0 {
		return u
	}
	return 1
}

// byteCol is the byte index in line of the UTF-16 column col, clamped to the
// line's end.
func byteCol(line string, col int) int {
	seen := 0
	for i, r := range line {
		if seen >= col {
			return i
		}
		seen += units(r)
	}
	return len(line)
}

// runeCol is the rune index in line of the UTF-16 column col, clamped.
func runeCol(line string, col int) int {
	seen, n := 0, 0
	for _, r := range line {
		if seen >= col {
			return n
		}
		seen += units(r)
		n++
	}
	return n
}

// runeIndex is the rune index in line of the byte index b, clamped.
func runeIndex(line string, b int) int {
	if b > len(line) {
		b = len(line)
	}
	return utf8.RuneCountInString(line[:b])
}

// utf16Col is the UTF-16 column of the rune index n in line, clamped.
func utf16Col(line string, n int) int {
	seen, i := 0, 0
	for _, r := range line {
		if i >= n {
			break
		}
		seen += units(r)
		i++
	}
	return seen
}
