package token

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"unicode"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/format"
)

// tokenizeOne tokenizes one or more tokens from a buffer slice.
// This is the core tokenization logic extracted from Tokenize().
//
// NOTE: The original Tokenize() appends a '\n' at the end of the document
// (line 29) to ensure the document always ends with a newline. For streaming,
// TokenSource should handle this by either:
//   1. Appending a virtual '\n' when EOF is detected and last byte isn't '\n'
//   2. Or ensuring the final buffer chunk ends with '\n' when EOF is reached
//
// Parameters:
//   - d: buffer slice to tokenize from (may be partial document)
//   - i: current offset within buffer (relative offset, 0-based)
//   - bufferStartOffset: absolute offset where buffer starts in stream (for PosDoc)
//   - ts: tokenization state (cb, sb, lnIndent, etc.) - MODIFIED
//   - posDoc: position document (streaming-adapted) - MODIFIED
//   - opt: tokenization options
//   - lastToken: last token emitted (for context, e.g., multiline string detection)
//   - recentBuf: recent buffer bytes before current position (for commentPrefix, can be nil)
//   - allTokens: all tokens emitted so far (for non-streaming mode, can be nil)
//   - docPrefix: document bytes before current position (for non-streaming mode, used when recentBuf is nil)
//
// Returns:
//   - tokens: slice of tokens found (empty slice for whitespace)
//   - consumed: number of bytes consumed from buffer
//   - error: any error encountered, or io.EOF if need more buffer
func tokenizeOne(
	d []byte,           // buffer slice
	i int,              // offset in buffer (relative)
	bufferStartOffset int, // absolute offset where buffer starts in stream
	ts *tkState,        // state (modified in place)
	posDoc *PosDoc,     // position tracking (modified in place)
	opt *tokenOpts,     // options
	lastToken *Token,   // last token (for context)
	recentBuf []byte,   // recent buffer before current position (for commentPrefix, can be nil, use docPrefix if nil)
	allTokens []Token,  // all tokens so far (for non-streaming, can be nil)
	docPrefix []byte,   // document before current position (for non-streaming, can be nil)
) ([]Token, int, error) {
	n := len(d)
	if i >= n {
		return nil, 0, io.EOF
	}

	c := d[i]
	absOffset := bufferStartOffset + i // Absolute position in stream

	// Handle newline
	if c == '\n' {
		posDoc.nl(absOffset)
		i++
		if i < n && d[i] == '-' && i+1 < n && d[i+1] == '-' && i+2 < n && d[i+2] == '-' {
			tok := Token{
				Type:  TDocSep,
				Pos:   posDoc.Pos(absOffset + 1), // After newline
				Bytes: d[i : i+4],
			}
			return []Token{tok}, 4, nil
		}
		if i < n && d[i] == '\n' {
			// Consecutive newline - consume first one, skip token
			// Next call will process the second newline
			return nil, 1, nil
		}
		indent := readIndent(d[i:])
		tok := Token{
			Type:  TIndent,
			Bytes: bytes.Repeat([]byte{' '}, indent),
			Pos:   posDoc.Pos(absOffset + 1), // After newline
		}
		ts.lnIndent = indent
		ts.kvSep = false
		ts.bElt = 0
		// Return indent token, but consumed bytes includes the newline we already advanced past
		return []Token{tok}, 1 + indent, nil
	}

	// Main switch statement
	switch c {
	case ':':
		if opt.format == format.YAMLFormat &&
			i+1 < n &&
			d[i+1] != ' ' &&
			d[i+1] != '\t' &&
			d[i+1] != '\r' &&
			d[i+1] != '\n' {
			off, err := yamlPlain(d[i+1:], ts, absOffset+1, posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(d[i:i+off+1], posDoc.Pos(absOffset))
			return []Token{*tok}, off + 1, nil
		}
		tok := Token{
			Type:  TColon,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		ts.kvSep = true
		return []Token{tok}, 1, nil

	case '"', '\'':
		if opt.format == format.YAMLFormat {
			tok, off, err := YAMLQuotedString(d[i:], posDoc.Pos(absOffset))
			if err != nil {
				return nil, 0, NewTokenizeErr(err, posDoc.Pos(absOffset))
			}
			tok.Pos = posDoc.Pos(absOffset)
			return []Token{*tok}, off, nil
		}
		if opt.format == format.TonyFormat {
			indent := -1
			if lastToken != nil && lastToken.Type == TIndent {
				indent = len(lastToken.Bytes)
			} else if lastToken == nil {
				indent = 0
			}
			if indent != -1 {
				// multiline enabled string - returns multiple tokens
				toks, off, err := mString(d[i:], absOffset, indent, posDoc)
				if err != nil {
					// In streaming mode (allTokens == nil), convert ErrUnterminated to io.EOF
					// to request more buffer. Use errors.Is to properly unwrap errors.
					if allTokens == nil && errors.Is(err, ErrUnterminated) {
						return nil, 0, io.EOF
					}
					return nil, 0, err
				}
				return toks, off, nil
			}
		}

		j, err := bsEscQuoted(d[i:])
		if err != nil {
			// In streaming mode (allTokens == nil), convert ErrUnterminated to io.EOF
			// to request more buffer
			if err == ErrUnterminated && allTokens == nil {
				return nil, 0, io.EOF
			}
			return nil, 0, NewTokenizeErr(err, posDoc.Pos(absOffset))
		}
		tok := Token{
			Type:  TString,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+j],
		}
		return []Token{tok}, j, nil

	case '!':
		if opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("!", posDoc.Pos(absOffset))
		}
		start := i + 1
		for start < n {
			r, sz := utf8.DecodeRune(d[start:])
				if r == utf8.RuneError {
					return nil, 0, UnexpectedErr("bad utf8", posDoc.Pos(bufferStartOffset+start))
				}
				if unicode.IsSpace(r) {
					break
				}
				if unicode.Is(unicode.Other, r) {
					return nil, 0, UnexpectedErr("unicode other", posDoc.Pos(bufferStartOffset+start))
				}
			start += sz
		}

		if i+1 == start {
			return nil, 0, UnexpectedErr("end", posDoc.Pos(absOffset+start))
		}

		tok := Token{
			Type:  TTag,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i:start],
		}
		return []Token{tok}, start - i, nil

	case '|':
		if opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("|", posDoc.Pos(absOffset))
		}
		// Use current line indent directly (ts.lnIndent is always up-to-date)
		// mLit content is indented 2 spaces more than the line containing |
		mIndent := ts.lnIndent + 2
		if mIndent < 2 {
			// Ensure minimum indent of 2 (for root-level mLits)
			mIndent = 2
		}
		var sz int
		var err error
		// Use streaming-aware version if recentBuf is non-nil (indicates streaming mode)
		// Otherwise use non-streaming version
		if recentBuf != nil {
			// Streaming mode: use streaming-aware version that can return io.EOF
			sz, err = mLitStreaming(d[i:], mIndent, posDoc, absOffset)
		} else {
			// Non-streaming mode: use original version
			sz, err = mLit(d[i:], mIndent, posDoc, absOffset)
		}
		if err != nil {
			return nil, 0, err
		}
		idBytes := make([]byte, 0, sz+1)
		idBytes = binary.AppendUvarint(idBytes, uint64(mIndent))
		tok := Token{
			Type:  TMLit,
			Bytes: append(idBytes, d[i:i+sz]...),
			Pos:   posDoc.Pos(absOffset),
		}
		consumed := sz
		if sz > 0 {
			consumed--
		}
		return []Token{tok}, consumed, nil

	case '>':
		if opt.format != format.YAMLFormat {
			return nil, 0, UnexpectedErr(">", posDoc.Pos(absOffset))
		}
		return nil, 0, NewTokenizeErr(ErrUnsupported, posDoc.Pos(absOffset))

	case '-':
		if i == n-1 {
			return nil, 0, UnexpectedErr("end", posDoc.Pos(absOffset))
		}
		if i == 0 && n >= 3 && d[1] == '-' && d[2] == '-' {
			if opt.format == format.JSONFormat {
				return nil, 0, UnexpectedErr("-", posDoc.Pos(absOffset))
			}
			tok := Token{
				Type:  TDocSep,
				Pos:   posDoc.Pos(absOffset),
				Bytes: d[0:3],
			}
			return []Token{tok}, 3, nil
		}

		next := d[i+1]
		switch next {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if opt.format == format.YAMLFormat {
				off, err := yamlPlain(d[i+1:], ts, absOffset+1, posDoc)
				if err != nil {
					return nil, 0, err
				}
				numLen, isFloat, err := number(d[i+1 : i+1+off])
				if err == nil && numLen == off {
					tok := Token{
						Type:  TInteger,
						Pos:   posDoc.Pos(absOffset),
						Bytes: d[i : i+numLen+1],
					}
					if isFloat {
						tok.Type = TFloat
					}
					return []Token{tok}, numLen + 1, nil
				}
				tok := yamlPlainToken(d[i:i+off+1], posDoc.Pos(absOffset))
				return []Token{*tok}, off + 1, nil
			}
			numLen, isFloat, err := number(d[i+1:])
			if err != nil {
				return nil, 0, NewTokenizeErr(err, posDoc.Pos(absOffset))
			}
			tok := Token{
				Type:  TInteger,
				Pos:   posDoc.Pos(absOffset),
				Bytes: d[i : i+numLen+1],
			}
			if isFloat {
				tok.Type = TFloat
			}
			return []Token{tok}, numLen + 1, nil

		case ' ', '\n', '\t':
			if opt.format == format.JSONFormat {
				return nil, 0, UnexpectedErr("- ", posDoc.Pos(absOffset))
			}
			tok := Token{
				Type:  TArrayElt,
				Bytes: d[i : i+2],
				Pos:   posDoc.Pos(absOffset),
			}
			consumed := 1
			if next != '\n' {
				consumed = 2
			}
			ts.bElt++
			if opt.format == format.YAMLFormat {
				j := i + 2
				for j < n {
					if d[j] == ' ' {
						ts.lnIndent++
						j++
						continue
					}
					break
				}
			}
			return []Token{tok}, consumed, nil

		default:
			switch opt.format {
			case format.JSONFormat:
				return nil, 0, UnexpectedErr("n...", posDoc.Pos(absOffset))
			case format.TonyFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, 0, err
				}
				tok := Token{
					Type:  TLiteral,
					Pos:   posDoc.Pos(absOffset),
					Bytes: lit,
				}
				return []Token{tok}, len(lit), nil
			case format.YAMLFormat:
				off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
				if err != nil {
					return nil, 0, err
				}
				tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
				return []Token{*tok}, off, nil
			default:
				return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, opt.format.String()), posDoc.Pos(absOffset))
			}
		}

	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if opt.format == format.YAMLFormat {
			off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
			if err != nil {
				return nil, 0, err
			}
			numLen, isFloat, err := number(d[i : i+off])
			if err == nil && numLen == off {
				tok := Token{
					Type:  TInteger,
					Pos:   posDoc.Pos(absOffset),
					Bytes: d[i : i+numLen],
				}
				if isFloat {
					tok.Type = TFloat
				}
				return []Token{tok}, off, nil
			}
			tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
			return []Token{*tok}, off, nil
		}
		numLen, isFloat, err := number(d[i:])
		if err != nil {
			return nil, 0, NewTokenizeErr(err, posDoc.Pos(absOffset))
		}
		tok := Token{
			Type:  TInteger,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+numLen],
		}
		if isFloat {
			tok.Type = TFloat
		}
		return []Token{tok}, numLen, nil

	case '#':
		if opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("#", posDoc.Pos(absOffset))
		}
		// Calculate commentPrefix from buffer (recentBuf for streaming, docPrefix for non-streaming)
		preLen := 0
		bufForPrefix := recentBuf
		if bufForPrefix == nil && docPrefix != nil {
			// Non-streaming mode: use document prefix
			bufForPrefix = docPrefix
		}
		if bufForPrefix != nil && len(bufForPrefix) > 0 {
			// Use commentPrefix with buffer
			preLen = commentPrefix(bufForPrefix, ts.lnIndent)
		}
		end := i
		for end < n {
			r, sz := utf8.DecodeRune(d[end:])
			if r == utf8.RuneError {
				return nil, 0, UnexpectedErr("bad utf8", posDoc.Pos(bufferStartOffset+end))
			}
			if r != '\n' {
				end += sz
				continue
			}
			// Found newline - create comment token
			commentStart := i - preLen
			if commentStart < 0 {
				commentStart = 0
			}
			tok := Token{
				Type:  TComment,
				Pos:   posDoc.Pos(bufferStartOffset + end),
				Bytes: d[commentStart:end],
			}
			return []Token{tok}, end - i, nil
		}
		// Comment extends beyond buffer - need more data
		return nil, 0, io.EOF

	case ' ', '\t', '\r', '\v', '\f':
		return nil, 1, nil // Whitespace, no token

	case 'n':
		if i+4 <= n && string(d[i:i+4]) == "null" && isKeyWordPrefix(d[i:], []byte("null")) {
			tok := Token{
				Type:  TNull,
				Bytes: d[i : i+4],
				Pos:   posDoc.Pos(absOffset),
			}
			return []Token{tok}, 4, nil
		}
		switch opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", posDoc.Pos(absOffset))
		case format.TonyFormat:
			lit, err := getSingleLiteral(d[i:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   posDoc.Pos(absOffset),
				Bytes: lit,
			}
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, opt.format.String()), posDoc.Pos(absOffset))
		}

	case 't':
		if i+4 <= n && string(d[i:i+4]) == "true" && isKeyWordPrefix(d[i:], []byte("true")) {
			tok := Token{
				Type:  TTrue,
				Bytes: d[i : i+4],
				Pos:   posDoc.Pos(absOffset),
			}
			return []Token{tok}, 4, nil
		}
		switch opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", posDoc.Pos(absOffset))
		case format.TonyFormat:
			lit, err := getSingleLiteral(d[i:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   posDoc.Pos(absOffset),
				Bytes: lit,
			}
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, opt.format.String()), posDoc.Pos(absOffset))
		}

	case 'f':
		if i+5 <= n && string(d[i:i+5]) == "false" && isKeyWordPrefix(d[i:], []byte("false")) {
			tok := Token{
				Type:  TFalse,
				Bytes: d[i : i+5],
				Pos:   posDoc.Pos(absOffset),
			}
			return []Token{tok}, 5, nil
		}
		switch opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("f...", posDoc.Pos(absOffset))
		case format.TonyFormat:
			lit, err := getSingleLiteral(d[i:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   posDoc.Pos(absOffset),
				Bytes: lit,
			}
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
			return []Token{*tok}, off, nil
		default:
			return nil, 0, UnexpectedErr("f...", posDoc.Pos(absOffset))
		}

	case '<':
		if opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("<", posDoc.Pos(absOffset))
		}
		if i+1 >= n {
			return nil, 0, NewTokenizeErr(ErrUnterminated, posDoc.Pos(absOffset))
		}
		if d[i+1] != '<' {
			return nil, 0, NewTokenizeErr(ErrUnterminated, posDoc.Pos(absOffset))
		}
		tok := Token{
			Type:  TMergeKey,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+2],
		}
		return []Token{tok}, 2, nil

	case '{':
		ts.cb++
		tok := Token{
			Type:  TLCurl,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		return []Token{tok}, 1, nil

	case '}':
		ts.cb--
		tok := Token{
			Type:  TRCurl,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		return []Token{tok}, 1, nil

	case '[':
		ts.sb++
		tok := Token{
			Type:  TLSquare,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		return []Token{tok}, 1, nil

	case ']':
		ts.sb--
		tok := Token{
			Type:  TRSquare,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		return []Token{tok}, 1, nil

	case ',':
		tok := Token{
			Type:  TComma,
			Pos:   posDoc.Pos(absOffset),
			Bytes: d[i : i+1],
		}
		return []Token{tok}, 1, nil

	default:
		switch opt.format {
		case format.TonyFormat:
			lit, err := getSingleLiteral(d[i:])
			if err != nil {
				return nil, 0, NewTokenizeErr(ErrLiteral, posDoc.Pos(absOffset))
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   posDoc.Pos(absOffset),
				Bytes: lit,
			}
			return []Token{tok}, len(lit), nil
		case format.JSONFormat:
			lit, err := getSingleLiteral(d[i:])
			if err != nil {
				return nil, 0, NewTokenizeErr(ErrLiteral, posDoc.Pos(absOffset))
			}
			return nil, 0, UnexpectedErr(string(lit), posDoc.Pos(absOffset))
		case format.YAMLFormat:
			off, err := yamlPlain(d[i:], ts, absOffset, posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(d[i:i+off], posDoc.Pos(absOffset))
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, opt.format.String()), posDoc.Pos(absOffset))
		}
	}
}
