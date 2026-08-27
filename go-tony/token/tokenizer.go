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
	reader   io.Reader // nil for non-streaming mode
	buffer   []byte    // current buffer
	bufPos   int       // position in buffer
	bufStart int64     // absolute offset where buffer starts

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

	// drained is set by whoever owns the buffer (TokenSource, or readStreaming
	// below) once the reader has returned io.EOF and the trailing newline has
	// been supplied: no more data is coming, so a scan that reaches the end of
	// the buffer must terminate there instead of asking for a refill that will
	// never arrive.
	//
	// Without it the streaming scanners are selected by reader != nil alone,
	// which never stops being true, and any construct terminated by end of
	// input rather than by a delimiter — a multiline literal at the end of a
	// document — is asked for forever and silently dropped.
	drained bool
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

	// The scanners want a document which ends in a newline, and one which already
	// does must not be given a second: `a: |+\n  x\n` is a value of "x\n", and
	// the appended newline made it "x\n\n" -- which the encoder writes out, and
	// the next read grows again, so a keep-chomped block scalar gained newlines
	// on every round trip.  The streaming path has always added it only when it
	// was missing; this is the same rule (75g1kbpdh12krs09gdn0).
	posDoc := &PosDoc{d: make([]byte, len(doc), len(doc)+1)}
	copy(posDoc.d, doc)
	if len(posDoc.d) == 0 || posDoc.d[len(posDoc.d)-1] != '\n' {
		posDoc.d = append(posDoc.d, '\n')
	}

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
		t.drained = true
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

// yamlPlainAt scans a plain YAML scalar at the current position, asking for more
// data when the scan reached the end of the buffer while more can still arrive: a
// plain scalar ends at a delimiter, so one which runs to the end of the buffer
// may continue in the next read.  Reading it as complete chopped every scalar
// which met a read boundary (75g1kbpdh12krs09gdn0).
func (t *Tokenizer) yamlPlainAt(d []byte, off int) (int, error) {
	sz, ranOut, err := yamlPlainRun(d, t.ts, off, t.posDoc)
	if err != nil {
		return 0, err
	}
	if ranOut && t.reader != nil && !t.drained {
		return 0, io.EOF
	}
	return sz, nil
}

// needsMoreData reports whether a scan failed only because the buffer ran out
// under it, and more data can still arrive to complete the construct — in which
// case the caller signals io.EOF to have the buffer grown and the scan retried.
//
// Once the reader is drained the same errors are real: the buffer holds the
// whole document, so the string really is unterminated and the rune really is
// truncated.
func (t *Tokenizer) needsMoreData(err error) bool {
	if t.reader == nil || t.drained {
		return false
	}
	return errors.Is(err, ErrUnterminated) || errors.Is(err, ErrPartialRune)
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
		// What follows a newline decides both of the things below -- whether this
		// is a document separator, and how far the next line is indented -- so a
		// buffer which ends AT the newline cannot answer either. It answered
		// anyway: `a: 1\n---\nb: 2\n` read one byte at a time reached here with
		// nothing after the newline, took the indent branch, consumed the newline,
		// and left the separator to be scanned as a literal.
		if pos >= n && t.reader != nil && !t.drained {
			return nil, 0, io.EOF
		}
		// A separator is `---` and the newline which ends it: four bytes. Deciding
		// on fewer than that is two bugs, and the quiet one is worse. The guard
		// established only that the third dash was in the buffer, and then read a
		// fourth byte: `a: 1\n---\nb: 2\n` in 4-byte reads PANICKED with a slice
		// out of range, and in 3-byte reads came back with `---` as a literal --
		// two documents silently merged into one, with no error anywhere. Whether
		// this is a separator is not knowable until the bytes are here.
		if pos < n && data[pos] == '-' {
			if pos+4 > n {
				if t.reader != nil && !t.drained {
					return nil, 0, io.EOF // need more data to tell
				}
			} else if data[pos+1] == '-' && data[pos+2] == '-' {
				tok := Token{
					Type:  TDocSep,
					Pos:   t.posDoc.Pos(int(absOffset + 1)), // After newline
					Bytes: data[pos : pos+4],
				}
				return []Token{tok}, 4, nil
			}
		}
		if pos < n && data[pos] == '\n' {
			// Consecutive newline - consume first one, skip token
			// Next call will process the second newline
			return nil, 1, nil
		}
		indent := readIndent(data[pos:])
		// An indent run which reaches the buffer end may continue in the next
		// read, and the indent is the structure: reporting it short puts the line
		// at the wrong depth, silently.
		if pos+indent >= n && t.reader != nil && !t.drained {
			return nil, 0, io.EOF
		}
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
		// In YAML a ':' with no space after it is part of a plain scalar (`:9091`),
		// and the byte after it is what says so: a buffer ending at the colon
		// cannot tell, and calling it a key separator split `:9091` into a colon
		// and a scalar.
		if t.opt.format == format.YAMLFormat && pos+1 >= n && t.reader != nil && !t.drained {
			return nil, 0, io.EOF
		}
		if t.opt.format == format.YAMLFormat &&
			pos+1 < n &&
			data[pos+1] != ' ' &&
			data[pos+1] != '\t' &&
			data[pos+1] != '\r' &&
			data[pos+1] != '\n' {
			off, err := t.yamlPlainAt(data[pos+1:], int(absOffset+1))
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
				if t.needsMoreData(err) {
					return nil, 0, io.EOF // the closing quote may be in the next read
				}
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
				toks, off, err := mString(data[pos:], int(absOffset), indent, t.posDoc, t.reader != nil && !t.drained)
				if err != nil {
					if t.needsMoreData(err) {
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
			if t.needsMoreData(err) {
				return nil, 0, io.EOF
			}
			// j is the offset of the offending byte within the token.
			return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)+j))
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
		// A tag ends at whitespace OR where the flow grammar it sits inside resumes.
		//
		// The tag production is YAML's -- any non-whitespace unicode -- which is right
		// where a tag is a URI and wrong here, where `,`, `[`, `]`, `{` and `}` belong
		// to the grammar around it. Running to whitespace alone absorbed them into the
		// NAME: `{a !delete, b: 1}` tagged a null `!delete,`, which names no operator,
		// so it was stored as data and `a` was not deleted -- silently, and past both
		// guards a consumer has, since an unknown tag is what neither can look up
		// (pkj422gkh12kr24gj1n0).
		//
		// Only at paren depth 0. Inside a tag COMPONENT those characters are legitimate
		// content -- `!get-path(a[0])` is an addressing form with no other spelling, and
		// `!tag(a,b)` is two arguments -- so the depth is what tells a separator from a
		// bracket in a kpath. An unmatched `(` never returns to 0 and the scan runs to
		// whitespace as it always did.
		depth := 0
	tagScan:
		for start < n {
			r, sz := utf8.DecodeRune(data[start:])
			if r == utf8.RuneError {
				if t.reader != nil && !t.drained && partialRune(data[start:]) {
					return nil, 0, io.EOF // tag continues in the next read
				}
				return nil, 0, UnexpectedErr("bad utf8", t.posDoc.Pos(int(bufferStartOffset)+start))
			}
			if unicode.IsSpace(r) {
				break
			}
			if unicode.Is(unicode.Other, r) {
				return nil, 0, UnexpectedErr("unicode other", t.posDoc.Pos(int(bufferStartOffset)+start))
			}
			switch r {
			case '(':
				depth++
			case ')':
				if depth > 0 {
					depth--
				}
			case ',', '[', ']', '{', '}':
				if depth == 0 {
					break tagScan
				}
			}
			start += sz
		}

		if t.reader != nil && !t.drained && start == n {
			// The tag runs to the end of the buffer with no whitespace to end
			// it, so it may continue in the next read. Emitting it here cuts it
			// at the refill boundary and re-tokenizes the tail as a separate
			// literal — silently, since both halves are well formed.
			return nil, 0, io.EOF
		}

		if pos+1 == start {
			return nil, 0, UnexpectedErr("end", t.posDoc.Pos(int(bufferStartOffset)+start))
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
		// The content is one level in from the line's OWN level, and each '- ' on
		// the line is a level: `- - |` opens its literal inside two arrays, so its
		// content starts at 4. Counting only lnIndent read `- - |` as though the
		// markers were not there and put two columns of the content into the value
		// -- and accepted content dedented past the '|' itself
		// (crcz1erdh12ks8cvj1n0). YAML reads these the same way.
		bElt := t.ts.bElt
		if bElt < 1 {
			bElt = 1 // no marker: the value of a field, one level in from its line
		}
		mIndent := t.ts.lnIndent + 2*bElt
		if mIndent < 2 {
			// Ensure minimum indent of 2 (for root-level mLits)
			mIndent = 2
		}
		var sz int
		var err error
		// A multiline literal ends where its indentation ends, so a literal
		// running to the end of the buffer may or may not be complete: only the
		// bytes after it can say. Use the streaming-aware version, which asks
		// for those bytes with io.EOF, while more data can still arrive; once
		// the reader is drained the buffer holds the whole document and the
		// literal ends where it ends.
		if t.reader != nil && !t.drained {
			sz, err = mLitStreaming(data[pos:], mIndent, t.posDoc, int(absOffset))
		} else {
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
		// A comment on the opening line is a line comment on the literal. mLit
		// consumed it along with the rest and mLitToString skips it, so the value
		// is already right; without a token for it the parser has nothing to
		// attach and the comment was accepted and then dropped, `-c` and all.
		if _, _, _, cmt, _ := openLine(data[pos : pos+sz]); cmt >= 0 {
			end := cmt
			for end < sz && data[pos+end] != '\n' {
				end++
			}
			// The space before the '#' belongs to the comment, the same as it does
			// for a comment anywhere else (commentStart, above, backs over preLen).
			start := cmt
			for start > 0 && (data[pos+start-1] == ' ' || data[pos+start-1] == '\t') {
				start--
			}
			return []Token{tok, {
				Type:  TLineComment,
				Bytes: data[pos+start : pos+end],
				Pos:   t.posDoc.Pos(int(absOffset) + start),
			}}, consumed, nil
		}
		return []Token{tok}, consumed, nil

	case '>':
		if t.opt.format != format.YAMLFormat {
			return nil, 0, UnexpectedErr(">", t.posDoc.Pos(int(absOffset)))
		}
		return nil, 0, NewTokenizeErr(ErrUnsupported, t.posDoc.Pos(int(absOffset)))

	case '-':
		if pos == n-1 {
			// '-' is the last byte of the buffer: the number continues in the next
			// chunk. At true EOF the trailing newline follows it, so this only
			// happens mid-stream — request more data rather than erroring.
			return nil, 0, io.EOF
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
				off, err := t.yamlPlainAt(data[pos+1:], int(absOffset+1))
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
			numLen, isFloat, numErr := numberStreaming(data[pos+1:])
			if numErr == io.EOF {
				return nil, 0, io.EOF // number may continue past the buffer; need more data
			}
			// numLen+1 accounts for the '-', which is part of the literal run.
			lit, err := digitLeadingLiteral(data[pos:], numLen+1)
			if err == io.EOF {
				return nil, 0, io.EOF // literal may continue past the buffer; need more data
			}
			if err != nil {
				return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
			}
			if lit != nil {
				if tok, ok := t.digitLeadingToken(lit, int(absOffset)); ok {
					return []Token{tok}, len(lit), nil
				}
				return nil, 0, DigitLeadingErr(lit, t.posDoc.Pos(int(absOffset)))
			}
			if numErr != nil {
				return nil, 0, NewTokenizeErr(numErr, t.posDoc.Pos(int(absOffset)))
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
				lit, err := getSingleLiteralStreaming(data[pos:])
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
				off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
			off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
		numLen, isFloat, numErr := numberStreaming(data[pos:])
		if numErr == io.EOF {
			return nil, 0, io.EOF // number may continue past the buffer; need more data
		}
		// numErr is held rather than returned: the run decides first. "007" is a
		// leading zero, but "007m" is a quantity, and only the run tells them apart.
		lit, err := digitLeadingLiteral(data[pos:], numLen)
		if err == io.EOF {
			return nil, 0, io.EOF // literal may continue past the buffer; need more data
		}
		if err != nil {
			return nil, 0, NewTokenizeErr(err, t.posDoc.Pos(int(absOffset)))
		}
		if lit != nil {
			if tok, ok := t.digitLeadingToken(lit, int(absOffset)); ok {
				return []Token{tok}, len(lit), nil
			}
			return nil, 0, DigitLeadingErr(lit, t.posDoc.Pos(int(absOffset)))
		}
		if numErr != nil {
			return nil, 0, NewTokenizeErr(numErr, t.posDoc.Pos(int(absOffset)))
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
				if t.reader != nil && partialRune(data[end:]) {
					// Comment continues in the next read, same as reaching the
					// buffer end without a newline below.
					return nil, 0, io.EOF
				}
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
		if pos+4 < n && string(data[pos:pos+4]) == "null" {
			kw, partial := isKeyWordPrefix(data[pos:], []byte("null"))
			if partial && t.reader != nil {
				return nil, 0, io.EOF // rune after the keyword is cut off
			}
			if kw {
				tok := Token{
					Type:  TNull,
					Bytes: data[pos : pos+4],
					Pos:   t.posDoc.Pos(int(absOffset)),
				}
				t.ts.hasValue = true
				return []Token{tok}, 4, nil
			}
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteralStreaming(data[pos:])
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
			off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
		if pos+4 < n && string(data[pos:pos+4]) == "true" {
			kw, partial := isKeyWordPrefix(data[pos:], []byte("true"))
			if partial && t.reader != nil {
				return nil, 0, io.EOF // rune after the keyword is cut off
			}
			if kw {
				tok := Token{
					Type:  TTrue,
					Bytes: data[pos : pos+4],
					Pos:   t.posDoc.Pos(int(absOffset)),
				}
				t.ts.hasValue = true
				return []Token{tok}, 4, nil
			}
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("n...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteralStreaming(data[pos:])
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
			off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
		if pos+5 < n && string(data[pos:pos+5]) == "false" {
			kw, partial := isKeyWordPrefix(data[pos:], []byte("false"))
			if partial && t.reader != nil {
				return nil, 0, io.EOF // rune after the keyword is cut off
			}
			if kw {
				tok := Token{
					Type:  TFalse,
					Bytes: data[pos : pos+5],
					Pos:   t.posDoc.Pos(int(absOffset)),
				}
				t.ts.hasValue = true
				return []Token{tok}, 5, nil
			}
		}
		switch t.opt.format {
		case format.JSONFormat:
			return nil, 0, UnexpectedErr("f...", t.posDoc.Pos(int(absOffset)))
		case format.TonyFormat:
			lit, err := getSingleLiteralStreaming(data[pos:])
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
			off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
			// The second '<' may be in the next read: a merge key split across a
			// read boundary is not an unterminated one.
			if t.reader != nil && !t.drained {
				return nil, 0, io.EOF
			}
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
			lit, err := getSingleLiteralStreaming(data[pos:])
			if err == io.EOF {
				return nil, 0, io.EOF // literal runs to the buffer end; need more data
			}
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
			lit, err := getSingleLiteralStreaming(data[pos:])
			if err == io.EOF {
				return nil, 0, io.EOF // literal runs to the buffer end; need more data
			}
			if err != nil {
				return nil, 0, NewTokenizeErr(ErrLiteral, t.posDoc.Pos(int(absOffset)))
			}
			return nil, 0, UnexpectedErr(string(lit), t.posDoc.Pos(int(absOffset)))
		case format.YAMLFormat:
			off, err := t.yamlPlainAt(data[pos:], int(absOffset))
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
