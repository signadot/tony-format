package token

import (
	"fmt"
	"io"
)

// mLitStreaming is a streaming-aware version of mLit that can return io.EOF
// when it needs more buffer to determine if a multiline literal is terminated.
func mLitStreaming(d []byte, indent int, posDoc *PosDoc, off int) (int, error) {
	if len(d) < 2 {
		return 0, io.EOF // Need more buffer to determine if unterminated
	}
	if d[0] != '|' {
		return 0, fmt.Errorf("unexpected %q", string(d[0]))
	}
	start := 2
	format := d[1]
	switch format {
	case MLitChomp, MLitKeep:
		if len(d) < 3 {
			return 0, io.EOF // Need more buffer
		}
		start++
		posDoc.nl(off + 2)
	case '\n':
		posDoc.nl(off + 1)
	default:
		return 0, UnexpectedErr(string(format), posDoc.Pos(off+1))
	}
	rest, err := scanLinesStreaming(d[start:], posDoc, off+start, indent, format)
	if err != nil {
		return 0, err
	}
	return start + rest, nil
}

// scanLinesStreaming is a streaming-aware version of scanLines that returns io.EOF
// when it needs more buffer to determine if the multiline literal is terminated.
func scanLinesStreaming(d []byte, posDoc *PosDoc, off, indent int, format byte) (int, error) {
	i := 0
	n := len(d)
	for i < n {
		end, lineSz, err := scanLine(d[i:], indent)
		if err != nil {
			return 0, err
		}
		if end {
			break
		}
		// Advance i first
		i += lineSz
		posDoc.nl(i + off - 1)
		// If we've consumed all buffer and haven't found the end, we need more buffer
		if i == n && lineSz > 0 {
			// We've scanned some bytes but haven't found the end yet - need more buffer
			return i, io.EOF
		}
	}
	if i == 0 {
		// No lines scanned - check if we have any data at all
		if n == 0 {
			return 0, io.EOF // Need more buffer
		}
		// We have data but couldn't scan a line - malformed
		return 0, NewTokenizeErr(ErrMalformedMLit, posDoc.Pos(off))
	}
	// Check for newline if we actually found the end
	if i > 0 && i <= len(d) && d[i-1] != '\n' {
		// If we consumed all buffer, we need more buffer
		if i == n {
			return i, io.EOF
		}
		return 0, NewTokenizeErr(ErrMalformedMLit, posDoc.Pos(off+i))
	}
	if format != MLitKeep {
		return i, nil
	}
	trailing := i
	trailIndent := 0
	for trailing < n {
		c := d[trailing]
		trailing++
		switch c {
		case '\r':
		case '\n':
			posDoc.nl(off + trailing - 1)
			i = trailing - 1
			trailIndent = 0
		case ' ':
			trailIndent++
			if trailIndent > indent {
				e := fmt.Errorf("%w: indent %d > %d", ErrMalformedMLit,
					trailIndent, indent)
				return 0, NewTokenizeErr(e, posDoc.Pos(off+i))
			}
		default:
			goto done
		}
	}
	// If we consumed all buffer while scanning trailing whitespace, we need more buffer
	if trailing == n && format == MLitKeep {
		return i, io.EOF
	}
done:
	return i, nil
}
