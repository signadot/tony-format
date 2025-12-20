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

// Tokenizer provides stateful tokenization with proper buffer management
// and trailing whitespace tracking. It supports both streaming (io.Reader)
// and non-streaming ([]byte) modes.
type Tokenizer struct {
	// Tokenization state
	ts     *tkState
	posDoc *PosDoc
	opt    *tokenOpts

	// Buffer management (for streaming)
	reader  io.Reader // nil for non-streaming mode
	buffer  []byte    // current buffer
	bufPos  int       // position in buffer
	bufStart int64    // absolute offset where buffer starts

	// Trailing whitespace tracking
	// Accumulates whitespace at the end of each buffer read
	// to be prepended to the next buffer read
	trailingWhitespace []byte

	// Last token (for context, e.g., multiline string detection)
	lastToken *Token

	// Non-streaming mode
	doc    []byte // full document (non-streaming only)
	docPos int    // position in doc (non-streaming only)

	// EOF handling
	eof        bool
	trailingNL bool // whether we've added trailing newline (streaming only)
}

// NewTokenizer creates a new Tokenizer for streaming mode (from io.Reader).
func NewTokenizer(reader io.Reader, opts ...TokenOpt) *Tokenizer {
	opt := &tokenOpts{format: format.TonyFormat}
	for _, o := range opts {
		o(opt)
	}

	return &Tokenizer{
		reader: reader,
		ts:     &tkState{},
		posDoc: &PosDoc{}, // Empty PosDoc for streaming
		opt:    opt,
	}
}

// NewTokenizerFromBytes creates a new Tokenizer for non-streaming mode (from []byte).
func NewTokenizerFromBytes(doc []byte, opts ...TokenOpt) *Tokenizer {
	opt := &tokenOpts{format: format.TonyFormat}
	for _, o := range opts {
		o(opt)
	}

	// Create PosDoc with document (append newline like Tokenize does)
	posDoc := &PosDoc{d: make([]byte, len(doc), len(doc)+1)}
	copy(posDoc.d, doc)
	posDoc.d = append(posDoc.d, '\n')

	return &Tokenizer{
		doc:    posDoc.d, // Use PosDoc's d which includes trailing newline
		docPos: 0,
		ts:     &tkState{},
		posDoc: posDoc,
		opt:    opt,
	}
}

// Read reads the next chunk of data from the source.
// For streaming mode: reads from io.Reader, accumulates trailing whitespace.
// For non-streaming mode: returns remaining bytes from doc.
//
// Returns:
//   - data: bytes read (with trailing whitespace from previous read prepended if any)
//   - startOffset: absolute offset where this data starts in the stream
//   - err: io.EOF when no more data, or other error
func (t *Tokenizer) Read() (data []byte, startOffset int64, err error) {
	if t.reader != nil {
		return t.readStreaming()
	}
	return t.readNonStreaming()
}

// readStreaming reads from io.Reader with trailing whitespace accumulation.
func (t *Tokenizer) readStreaming() ([]byte, int64, error) {
	if t.eof && t.bufPos >= len(t.buffer) {
		return nil, 0, io.EOF
	}

	// Compact buffer if needed (similar to TokenSource.fillBuffer)
	if t.bufPos > 4096 && len(t.buffer) > 4096*2 {
		remaining := t.buffer[t.bufPos:]
		copy(t.buffer, remaining)
		t.buffer = t.buffer[:len(remaining)]
		t.bufStart += int64(t.bufPos)
		t.bufPos = 0
	}

	// Read more data
	readBuf := make([]byte, 4096)
	n, err := t.reader.Read(readBuf)
	if n > 0 {
		t.buffer = append(t.buffer, readBuf[:n]...)
	}

	if err == io.EOF {
		t.eof = true
		// Ensure trailing newline if needed
		if !t.trailingNL {
			if len(t.buffer) == 0 || t.buffer[len(t.buffer)-1] != '\n' {
				t.buffer = append(t.buffer, '\n')
				t.trailingNL = true
			} else {
				t.trailingNL = true
			}
		}
	} else if err != nil {
		return nil, 0, err
	}

	// If we have no data and EOF, return EOF
	if len(t.buffer) == 0 || t.bufPos >= len(t.buffer) {
		return nil, 0, io.EOF
	}

	// Extract trailing whitespace from current buffer
	// (whitespace at the end that might continue in next buffer)
	trailingWS := t.extractTrailingWhitespace(t.buffer[t.bufPos:])

	// Prepare data to return: trailing whitespace from previous read + current buffer
	result := make([]byte, 0, len(t.trailingWhitespace)+len(t.buffer[t.bufPos:]))
	result = append(result, t.trailingWhitespace...)
	result = append(result, t.buffer[t.bufPos:]...)

	// Save trailing whitespace for next read
	t.trailingWhitespace = trailingWS

	// Update position
	startOffset := t.bufStart + int64(t.bufPos)
	t.bufPos = len(t.buffer)

	// If we hit EOF from reader, return EOF to signal this is the last data
	if t.eof {
		return result, startOffset, io.EOF
	}

	return result, startOffset, nil
}

// readNonStreaming returns remaining bytes from doc.
func (t *Tokenizer) readNonStreaming() ([]byte, int64, error) {
	if t.docPos >= len(t.doc) {
		return nil, 0, io.EOF
	}

	data := t.doc[t.docPos:]
	startOffset := int64(t.docPos)
	t.docPos = len(t.doc)

	return data, startOffset, nil
}

// extractTrailingWhitespace extracts whitespace (spaces, tabs) from the end of data.
// This whitespace will be prepended to the next buffer read.
// Note: newlines are NOT considered whitespace for this purpose.
// Example: "hello   \n" -> returns "   " (spaces before newline)
// Example: "hello\n" -> returns nil (no whitespace before newline)
func (t *Tokenizer) extractTrailingWhitespace(data []byte) []byte {
	if len(data) == 0 {
		return nil
	}

	// Find the last newline (if any)
	lastNewline := -1
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == '\n' {
			lastNewline = i
			break
		}
	}

	// If there's a newline at the end, extract whitespace BEFORE it
	if lastNewline >= 0 && lastNewline == len(data)-1 {
		// Newline is at the end - look backwards for whitespace before it
		wsEnd := lastNewline
		wsStart := lastNewline
		for i := lastNewline - 1; i >= 0; i-- {
			if data[i] == ' ' || data[i] == '\t' {
				wsStart = i
				continue
			}
			// Found non-whitespace - return accumulated whitespace
			if wsStart < wsEnd {
				return data[wsStart:wsEnd]
			}
			return nil
		}
		// Reached start - return accumulated whitespace
		if wsStart < wsEnd {
			return data[wsStart:wsEnd]
		}
		return nil
	}

	// No newline - extract trailing whitespace from the end
	wsEnd := len(data)
	wsStart := len(data)
	for i := len(data) - 1; i >= 0; i-- {
		if data[i] == ' ' || data[i] == '\t' {
			wsStart = i
			continue
		}
		// Non-whitespace byte - return accumulated whitespace
		if wsStart < wsEnd {
			return data[wsStart:wsEnd]
		}
		return nil
	}

	// Reached start - return accumulated whitespace
	if wsStart < wsEnd {
		return data[wsStart:wsEnd]
	}

	return nil
}

// TokenizeOne tokenizes one or more tokens from a buffer slice.
// This is the core tokenization logic, adapted to use Tokenizer's state
// and lineStartOffset for comment prefix calculation (no recentBuf/docPrefix fallback).
//
// Parameters:
//   - data: buffer slice to tokenize from (may be partial document)
//   - pos: current offset within buffer (relative offset, 0-based)
//   - bufferStartOffset: absolute offset where buffer starts in stream (for PosDoc and lineStartOffset calculation)
//
// Returns:
//   - tokens: slice of tokens found (empty slice for whitespace)
//   - consumed: number of bytes consumed from buffer
//   - error: any error encountered, or io.EOF if need more buffer
func (t *Tokenizer) TokenizeOne(data []byte, pos int, bufferStartOffset int64) ([]Token, int, error) {
	n := len(data)
	if pos >= n {
		return nil, 0, io.EOF
	}

	c := data[pos]
	absOffset := bufferStartOffset + int64(pos) // Absolute position in stream

	// Handle newline
	if c == '\n' {
		t.posDoc.nl(int(absOffset))
		pos++
		if pos < n && data[pos] == '-' && pos+1 < n && data[pos+1] == '-' && pos+2 < n && data[pos+2] == '-' {
			tok := Token{
				Type:  TDocSep,
				Pos:   t.posDoc.Pos(int(absOffset + 1)), // After newline
				Bytes: data[pos : pos+4],
			}
			return []Token{tok}, 4, nil
		}
		if pos < n && data[pos] == '\n' {
			// Consecutive newline - consume first one, skip token
			// Next call will process the second newline
			return nil, 1, nil
		}
		indent := readIndent(data[pos:])
		tok := Token{
			Type:  TIndent,
			Bytes: bytes.Repeat([]byte{' '}, indent),
			Pos:   t.posDoc.Pos(int(absOffset + 1)), // After newline
		}
		t.ts.lnIndent = indent
		t.ts.lineStartOffset = absOffset + 1 // Line starts after newline
		t.ts.kvSep = false
		t.ts.hasValue = false
		t.ts.bElt = 0
		// Return indent token, but consumed bytes includes the newline we already advanced past
		return []Token{tok}, 1 + indent, nil
	}

	// Main switch statement
	switch c {
	case ':':
		if t.opt.format == format.YAMLFormat &&
			pos+1 < n &&
			data[pos+1] != ' ' &&
			data[pos+1] != '\t' &&
			data[pos+1] != '\r' &&
			data[pos+1] != '\n' {
			off, err := yamlPlain(data[pos+1:], t.ts, int(absOffset+1), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(data[pos:pos+off+1], t.posDoc.Pos(int(absOffset)))
			return []Token{*tok}, off + 1, nil
		}
		tok := Token{
			Type:  TColon,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		t.ts.kvSep = true
		return []Token{tok}, 1, nil

	case '"', '\'':
		if t.opt.format == format.YAMLFormat {
			tok, off, err := YAMLQuotedString(data[pos:], t.posDoc.Pos(int(absOffset)))
			if err != nil {
				return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
			}
			tok.Pos = t.posDoc.Pos(int(absOffset))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		}
		if t.opt.format == format.TonyFormat {
			indent := -1
			if t.lastToken != nil && t.lastToken.Type == TIndent {
				indent = len(t.lastToken.Bytes)
			} else if t.lastToken == nil {
				indent = 0
			}
			if indent != -1 {
				// multiline enabled string - returns multiple tokens
				toks, off, err := mString(data[pos:], int(absOffset), indent, t.posDoc)
				if err != nil {
					// In streaming mode (reader != nil), convert ErrUnterminated to io.EOF
					if t.reader != nil && errors.Is(err, ErrUnterminated) {
						return nil, 0, io.EOF
					}
					return nil, 0, err
				}
				t.ts.hasValue = true
				return toks, off, nil
			}
		}

		j, err := bsEscQuoted(data[pos:])
		if err != nil {
			// In streaming mode, convert ErrUnterminated to io.EOF
			if err == ErrUnterminated && t.reader != nil {
				return nil, 0, io.EOF
			}
			return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
		}
		tok := Token{
			Type:  TString,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+j],
		}
		t.ts.hasValue = true
		return []Token{tok}, j, nil

	case '!':
		if t.opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("!", t.posDoc.Pos(int(absOffset)))
		}
		start := pos + 1
		for start < n {
			r, sz := utf8.DecodeRune(data[start:])
			if r == utf8.RuneError {
				return nil, 0, UnexpectedErr("bad utf8", t.posDoc.Pos(int(bufferStartOffset)+start))
			}
			if unicode.IsSpace(r) {
				break
			}
			if unicode.Is(unicode.Other, r) {
				return nil, 0, UnexpectedErr("unicode other", t.posDoc.Pos(int(bufferStartOffset)+start))
			}
			start += sz
		}

		if pos+1 == start {
			return nil, 0, UnexpectedErr("end", t.posDoc.Pos(int(absOffset)+start))
		}

		tok := Token{
			Type:  TTag,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos:start],
		}
		return []Token{tok}, start - pos, nil

	case '|':
		if t.opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("|", t.posDoc.Pos(int(absOffset)))
		}
		// Use current line indent directly (ts.lnIndent is always up-to-date)
		// mLit content is indented 2 spaces more than the line containing |
		mIndent := t.ts.lnIndent + 2
		if mIndent < 2 {
			// Ensure minimum indent of 2 (for root-level mLits)
			mIndent = 2
		}
		var sz int
		var err error
		// Use streaming-aware version if reader is non-nil (indicates streaming mode)
		if t.reader != nil {
			// Streaming mode: use streaming-aware version that can return io.EOF
			sz, err = mLitStreaming(data[pos:], mIndent, t.posDoc, int(absOffset))
		} else {
			// Non-streaming mode: use original version
			sz, err = mLit(data[pos:], mIndent, t.posDoc, int(absOffset))
		}
		if err != nil {
			return nil, 0, err
		}
		idBytes := make([]byte, 0, sz+1)
		idBytes = binary.AppendUvarint(idBytes, uint64(mIndent))
		tok := Token{
			Type:  TMLit,
			Bytes: append(idBytes, data[pos:pos+sz]...),
			Pos:   t.posDoc.Pos(int(absOffset)),
		}
		consumed := sz
		if sz > 0 {
			consumed--
		}
		t.ts.hasValue = true
		return []Token{tok}, consumed, nil

	case '>':
		if t.opt.format != format.YAMLFormat {
			return nil, 0, UnexpectedErr(">", t.posDoc.Pos(int(absOffset)))
		}
		return nil, 0, NewTokenizeErr(ErrUnsupported, t.posDoc.Pos(int(absOffset)))

	case '-':
		if pos == n-1 {
			return nil, 0, UnexpectedErr("end", t.posDoc.Pos(int(absOffset)))
		}
		if pos == 0 && n >= 3 && data[1] == '-' && data[2] == '-' {
			if t.opt.format == format.JSONFormat {
				return nil, 0, UnexpectedErr("-", t.posDoc.Pos(int(absOffset)))
			}
			tok := Token{
				Type:  TDocSep,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: data[0:3],
			}
			return []Token{tok}, 3, nil
		}

		next := data[pos+1]
		switch next {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if t.opt.format == format.YAMLFormat {
			off, err := yamlPlain(data[pos+1:], t.ts, int(absOffset+1), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			numLen, isFloat, err := number(data[pos+1 : pos+1+off])
			if err == nil && numLen == off {
				tok := Token{
					Type:  TInteger,
					Pos:   t.posDoc.Pos(int(absOffset)),
					Bytes: data[pos : pos+numLen+1],
				}
				if isFloat {
					tok.Type = TFloat
				}
				t.ts.hasValue = true
				return []Token{tok}, numLen + 1, nil
			}
			tok := yamlPlainToken(data[pos:pos+off+1], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off + 1, nil
		}
			numLen, isFloat, err := number(data[pos+1:])
			if err != nil {
				return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
			}
			tok := Token{
				Type:  TInteger,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: data[pos : pos+numLen+1],
			}
			if isFloat {
				tok.Type = TFloat
			}
			t.ts.hasValue = true
			return []Token{tok}, numLen + 1, nil

		case ' ', '\n', '\t':
			if t.opt.format == format.JSONFormat {
				return nil, 0, UnexpectedErr("- ", t.posDoc.Pos(int(absOffset)))
			}
			tok := Token{
				Type:  TArrayElt,
				Bytes: data[pos : pos+2],
				Pos:   t.posDoc.Pos(int(absOffset)),
			}
			consumed := 1
			if next != '\n' {
				consumed = 2
			}
			t.ts.bElt++
			if t.opt.format == format.YAMLFormat {
				j := pos + 2
				for j < n {
					if data[j] == ' ' {
						t.ts.lnIndent++
						j++
						continue
					}
					break
				}
			}
			return []Token{tok}, consumed, nil

		default:
			switch t.opt.format {
			case format.JSONFormat:
				return nil, 0, UnexpectedErr("n...", t.posDoc.Pos(int(absOffset)))
			case format.TonyFormat:
				lit, err := getSingleLiteral(data[pos:])
				if err != nil {
					return nil, 0, err
				}
				tok := Token{
					Type:  TLiteral,
					Pos:   t.posDoc.Pos(int(absOffset)),
					Bytes: lit,
				}
				t.ts.hasValue = true
				return []Token{tok}, len(lit), nil
			case format.YAMLFormat:
				off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
				if err != nil {
					return nil, 0, err
				}
				tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
				t.ts.hasValue = true
				return []Token{*tok}, off, nil
			default:
				return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, t.opt.format.String()), t.posDoc.Pos(int(absOffset)))
			}
		}

	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		if t.opt.format == format.YAMLFormat {
			off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			numLen, isFloat, err := number(data[pos : pos+off])
			if err == nil && numLen == off {
				tok := Token{
					Type:  TInteger,
					Pos:   t.posDoc.Pos(int(absOffset)),
					Bytes: data[pos : pos+numLen],
				}
				if isFloat {
					tok.Type = TFloat
				}
				t.ts.hasValue = true
				return []Token{tok}, off, nil
			}
			tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		}
		numLen, isFloat, err := number(data[pos:])
		if err != nil {
			return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
		}
		tok := Token{
			Type:  TInteger,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+numLen],
		}
		if isFloat {
			tok.Type = TFloat
		}
		t.ts.hasValue = true
		return []Token{tok}, numLen, nil

	case '#':
		if t.opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("#", t.posDoc.Pos(int(absOffset)))
		}
		// Calculate commentPrefix using lineStartOffset (NO fallback to recentBuf/docPrefix)
		preLen := 0
		// lineStartOffset can be 0 for the first line, so check >= 0
		if t.ts.lineStartOffset >= 0 {
			// Calculate relative position of line start in current buffer
			lineStartRelPos := int(t.ts.lineStartOffset - bufferStartOffset)
			if lineStartRelPos >= 0 && lineStartRelPos < pos {
				// Line start is in current buffer - use it directly
				// lineStartOffset points to after newline (or start of doc for first line),
				// so we use bytes from lineStartRelPos to pos
				prefixBytes := data[lineStartRelPos:pos]
				preLen = commentPrefix(prefixBytes, t.ts.lnIndent)
			}
			// If lineStartRelPos < 0 (line start before buffer) or >= pos (line start after current pos),
			// we cannot calculate prefix - this should not happen in normal operation,
			// but if it does, preLen remains 0 (no prefix)
		}
		end := pos
		for end < n {
			r, sz := utf8.DecodeRune(data[end:])
			if r == utf8.RuneError {
				return nil, 0, UnexpectedErr("bad utf8", t.posDoc.Pos(int(bufferStartOffset)+end))
			}
			if r != '\n' {
				end += sz
				continue
			}
			// Found newline - create comment token
			commentStart := pos - preLen
			if commentStart < 0 {
				commentStart = 0
			}
			// Use TLineComment if this comment follows a colon or value on the same line
			var tokType TokenType = TComment
			if t.ts.kvSep || t.ts.hasValue {
				tokType = TLineComment
			}
			tok := Token{
				Type:  tokType,
				Pos:   t.posDoc.Pos(int(bufferStartOffset) + commentStart),
				Bytes: data[commentStart:end],
			}
			return []Token{tok}, end - pos, nil
		}
		// Comment extends beyond buffer - need more data
		return nil, 0, io.EOF

	case ' ', '\t', '\r', '\v', '\f':
		return nil, 1, nil // Whitespace, no token

	case 'n':
		if pos+4 <= n && string(data[pos:pos+4]) == "null" && isKeyWordPrefix(data[pos:], []byte("null")) {
			tok := Token{
				Type:  TNull,
				Bytes: data[pos : pos+4],
				Pos:   t.posDoc.Pos(int(absOffset)),
			}
			t.ts.hasValue = true
			return []Token{tok}, 4, nil
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteral(data[pos:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: lit,
			}
			t.ts.hasValue = true
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, t.opt.format.String()), t.posDoc.Pos(int(absOffset)))
		}

	case 't':
		if pos+4 <= n && string(data[pos:pos+4]) == "true" && isKeyWordPrefix(data[pos:], []byte("true")) {
			tok := Token{
				Type:  TTrue,
				Bytes: data[pos : pos+4],
				Pos:   t.posDoc.Pos(int(absOffset)),
			}
			t.ts.hasValue = true
			return []Token{tok}, 4, nil
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteral(data[pos:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: lit,
			}
			t.ts.hasValue = true
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, t.opt.format.String()), t.posDoc.Pos(int(absOffset)))
		}

	case 'f':
		if pos+5 <= n && string(data[pos:pos+5]) == "false" && isKeyWordPrefix(data[pos:], []byte("false")) {
			tok := Token{
				Type:  TFalse,
				Bytes: data[pos : pos+5],
				Pos:   t.posDoc.Pos(int(absOffset)),
			}
			t.ts.hasValue = true
			return []Token{tok}, 5, nil
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("f...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteral(data[pos:])
			if err != nil {
				return nil, 0, err
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: lit,
			}
			t.ts.hasValue = true
			return []Token{tok}, len(lit), nil
		case format.YAMLFormat:
			off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		default:
			return nil, 0, UnexpectedErr("f...", t.posDoc.Pos(int(absOffset)))
		}

	case '<':
		if t.opt.format == format.JSONFormat {
			return nil, 0, UnexpectedErr("<", t.posDoc.Pos(int(absOffset)))
		}
		if pos+1 >= n {
			return nil, 0, NewTokenizeErr(ErrUnterminated, t.posDoc.Pos(int(absOffset)))
		}
		if data[pos+1] != '<' {
			return nil, 0, NewTokenizeErr(ErrUnterminated, t.posDoc.Pos(int(absOffset)))
		}
		tok := Token{
			Type:  TMergeKey,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+2],
		}
		return []Token{tok}, 2, nil

	case '{':
		t.ts.cb++
		tok := Token{
			Type:  TLCurl,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		return []Token{tok}, 1, nil

	case '}':
		t.ts.cb--
		tok := Token{
			Type:  TRCurl,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		t.ts.hasValue = true
		return []Token{tok}, 1, nil

	case '[':
		t.ts.sb++
		tok := Token{
			Type:  TLSquare,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		return []Token{tok}, 1, nil

	case ']':
		t.ts.sb--
		tok := Token{
			Type:  TRSquare,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		t.ts.hasValue = true
		return []Token{tok}, 1, nil

	case ',':
		tok := Token{
			Type:  TComma,
			Pos:   t.posDoc.Pos(int(absOffset)),
			Bytes: data[pos : pos+1],
		}
		return []Token{tok}, 1, nil

	default:
		switch t.opt.format {
		case format.TonyFormat:
			lit, err := getSingleLiteral(data[pos:])
			if err != nil {
				return nil, 0, NewTokenizeErr(ErrLiteral, t.posDoc.Pos(int(absOffset)))
			}
			tok := Token{
				Type:  TLiteral,
				Pos:   t.posDoc.Pos(int(absOffset)),
				Bytes: lit,
			}
			t.ts.hasValue = true
			return []Token{tok}, len(lit), nil
		case format.JSONFormat:
			lit, err := getSingleLiteral(data[pos:])
			if err != nil {
				return nil, 0, NewTokenizeErr(ErrLiteral, t.posDoc.Pos(int(absOffset)))
			}
			return nil, 0, UnexpectedErr(string(lit), t.posDoc.Pos(int(absOffset)))
		case format.YAMLFormat:
			off, err := yamlPlain(data[pos:], t.ts, int(absOffset), t.posDoc)
			if err != nil {
				return nil, 0, err
			}
			tok := yamlPlainToken(data[pos:pos+off], t.posDoc.Pos(int(absOffset)))
			t.ts.hasValue = true
			return []Token{*tok}, off, nil
		default:
			return nil, 0, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, t.opt.format.String()), t.posDoc.Pos(int(absOffset)))
		}
	}
}
