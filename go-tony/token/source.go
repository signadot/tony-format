package token

import (
	"bytes"
	"io"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/format"
)

// TokenSource provides streaming tokenization from an io.Reader.
// It maintains internal state (tkState, PosDoc) and buffers data as needed.
type TokenSource struct {
	reader io.Reader

	// Internal buffer management
	buf      []byte
	bufStart int // Absolute offset where buf starts in stream
	bufPos   int // Current position within buf

	// Tokenization state (persisted across reads)
	ts     *tkState
	posDoc *PosDoc

	// Options
	opt *tokenOpts

	// Sliding windows for context-dependent functions
	recentTokens []Token // Last ~50 tokens for mLitIndent
	recentBuf    []byte  // Last ~200 bytes for commentPrefix

	// Last token emitted (for context)
	lastToken *Token

	// EOF handling
	eof             bool
	trailingNL      bool // Whether we've added trailing newline
	bufferSize      int  // Size of read buffer
	maxRecentTokens int  // Max tokens to keep in recentTokens
	maxRecentBuf    int  // Max bytes to keep in recentBuf

	// Initial state
	initialized bool // Whether we've processed initial indent

	// Path tracking (only for bracketed structures, using kinded path syntax)
	depth        int         // Current bracket nesting depth
	currentPath  string      // Current kinded path from root (e.g., "", "key", "key[0]", "a.b")
	pathStack    []string    // Stack of path components for nested structures
	bracketStack []TokenType // Stack of bracket types ('[' or '{') for each level
	arrayIndex   int         // Current array index (incremented in bracketed arrays)
	pendingKey   string      // Key name seen before TColon (TLiteral)
	pendingInt   string      // Integer key seen before TColon (for sparse arrays)
	pendingValue bool        // True if we've seen a value token that needs path update
}

const (
	defaultBufferSize      = 4096
	defaultMaxRecentTokens = 50
	defaultMaxRecentBuf    = 200
)

// NewTokenSource creates a new TokenSource reading from r.
func NewTokenSource(r io.Reader, opts ...TokenOpt) *TokenSource {
	opt := &tokenOpts{format: format.TonyFormat}
	for _, o := range opts {
		o(opt)
	}

	return &TokenSource{
		reader:          r,
		ts:              &tkState{},
		posDoc:          &PosDoc{}, // Empty PosDoc for streaming
		opt:             opt,
		bufferSize:      defaultBufferSize,
		maxRecentTokens: defaultMaxRecentTokens,
		maxRecentBuf:    defaultMaxRecentBuf,
		currentPath:     "", // Root path is empty string in kinded path syntax
	}
}

// Read reads tokens from the stream. It returns tokens until:
//   - A complete token (or tokens) is found
//   - EOF is reached
//   - An error occurs
//
// When EOF is reached, Read will return any remaining tokens and then
// return (nil, io.EOF) on subsequent calls.
//
// Some constructs, such as multiline strings, are encoded as
// sequences of tokens.
func (ts *TokenSource) Read() ([]Token, error) {
	if ts.eof && ts.bufPos >= len(ts.buf) {
		return nil, io.EOF
	}

	// Ensure we have data in buffer
	if err := ts.ensureBuffer(); err != nil {
		if err == io.EOF {
			ts.eof = true
		} else {
			return nil, err
		}
	}

	// Handle initial indent (only on first read)
	if !ts.initialized && len(ts.buf) > ts.bufPos {
		indent := readIndent(ts.buf[ts.bufPos:])
		if indent > 0 {
			tok := Token{
				Type:  TIndent,
				Bytes: bytes.Repeat([]byte{' '}, indent),
				Pos:   ts.posDoc.Pos(ts.bufStart + ts.bufPos),
			}
			ts.ts.lnIndent = indent
			ts.ts.kvSep = false
			ts.ts.bElt = 0
			ts.recentTokens = append(ts.recentTokens, tok)
			ts.lastToken = &tok
			// Advance past the indent (indent is number of spaces, so consume that many bytes)
			ts.bufPos += indent
			ts.recentBuf = append(ts.recentBuf, bytes.Repeat([]byte{' '}, indent)...)
			if len(ts.recentBuf) > ts.maxRecentBuf {
				excess := len(ts.recentBuf) - ts.maxRecentBuf
				ts.recentBuf = ts.recentBuf[excess:]
			}
			ts.initialized = true
			return []Token{tok}, nil
		}
		ts.initialized = true
	}

	// Main tokenization loop
	for {
		// Ensure we have data in buffer
		needsMoreData, err := ts.ensureBufferData()
		if err != nil {
			return nil, err
		}
		if needsMoreData {
			continue // Retry after reading more data
		}

		// Call tokenizeOne with streaming parameters
		tokens, consumed, err := tokenizeOne(
			ts.buf,          // buffer
			ts.bufPos,       // current position in buffer
			ts.bufStart,     // absolute offset where buffer starts
			ts.ts,           // state (modified)
			ts.posDoc,       // posDoc (modified)
			ts.opt,          // options
			ts.lastToken,    // last token
			ts.recentBuf,    // recent buffer (for commentPrefix)
			nil,             // allTokens (deprecated/unused)
			nil,             // docPrefix (nil for streaming)
		)

		if err == io.EOF {
			// tokenizeOne needs more buffer
			needsMoreData, err := ts.handleTokenizeEOF()
			if err != nil {
				return nil, err
			}
			if needsMoreData {
				continue // Retry after handling EOF
			}
			// No more data available, return EOF
			return nil, io.EOF
		}

		if err != nil {
			return nil, err
		}

		// Update position
		ts.bufPos += consumed

		// Update sliding windows and path tracking
		if len(tokens) > 0 {
			for _, tok := range tokens {
				ts.recentTokens = append(ts.recentTokens, tok)
				if len(ts.recentTokens) > ts.maxRecentTokens {
					ts.recentTokens = ts.recentTokens[1:]
				}
				ts.lastToken = &tok
				// Update path tracking (only for bracketed structures)
				ts.updateDepth(tok)
				ts.updatePath(tok)
			}

			// Update recentBuf with consumed bytes (before updating bufPos)
			if consumed > 0 {
				start := ts.bufPos - consumed
				if start < 0 {
					start = 0
				}
				consumedBytes := ts.buf[start:ts.bufPos]
				ts.recentBuf = append(ts.recentBuf, consumedBytes...)
				if len(ts.recentBuf) > ts.maxRecentBuf {
					// Keep only the last maxRecentBuf bytes
					excess := len(ts.recentBuf) - ts.maxRecentBuf
					ts.recentBuf = ts.recentBuf[excess:]
				}
			}

			// Return tokens if we got any
			return tokens, nil
		}

		// If we consumed bytes but got no tokens (whitespace), continue
		if consumed > 0 {
			continue
		}

		// Should not reach here, but handle gracefully
		return nil, io.EOF
	}
}

// ensureBuffer ensures we have data in the buffer, reading if necessary.
func (ts *TokenSource) ensureBuffer() error {
	if len(ts.buf) == 0 && !ts.eof {
		return ts.fillBuffer()
	}
	return nil
}

// ensureBufferData ensures we have data available at the current buffer position.
// Returns (needsMoreData, error) where needsMoreData is true if we need to retry
// after reading more data or handling EOF.
func (ts *TokenSource) ensureBufferData() (bool, error) {
	if ts.bufPos < len(ts.buf) {
		// We have data available
		return false, nil
	}

	// We've consumed all available data
	if ts.eof {
		// EOF reached - check if we need trailing newline
		return ts.ensureTrailingNewline()
	}

	// Try to read more data
	if err := ts.fillBuffer(); err != nil {
		if err == io.EOF {
			ts.eof = true
			// Retry with EOF handling
			return ts.ensureTrailingNewline()
		}
		return false, err
	}

	// Got more data, retry tokenization
	return true, nil
}

// ensureTrailingNewline ensures the buffer ends with a newline if needed.
// This is required because the tokenizer expects input to end with a newline.
// Returns (needsMoreData, error) where needsMoreData is true if we added a newline
// and should retry tokenization.
func (ts *TokenSource) ensureTrailingNewline() (bool, error) {
	if ts.trailingNL {
		// Already added trailing newline, we're done
		return false, io.EOF
	}

	ts.trailingNL = true

	// Check if buffer already ends with newline
	if len(ts.buf) > 0 && ts.buf[len(ts.buf)-1] == '\n' {
		// Already ends with newline, we're done
		return false, io.EOF
	}

	// Append virtual newline to buffer for tokenization
	ts.buf = append(ts.buf, '\n')
	// Retry tokenization with the newline
	return true, nil
}

// handleTokenizeEOF handles the case where tokenizeOne returns io.EOF
// (meaning it needs more buffer to complete tokenization).
// Returns (needsMoreData, error) where needsMoreData is true if we should retry.
func (ts *TokenSource) handleTokenizeEOF() (bool, error) {
	if ts.eof {
		// EOF already reached, but tokenizer still needs buffer
		// This means we need a trailing newline
		return ts.ensureTrailingNewline()
	}

	// Try to read more data
	if err := ts.fillBuffer(); err != nil {
		if err == io.EOF {
			ts.eof = true
			// Retry with EOF handling
			return ts.ensureTrailingNewline()
		}
		return false, err
	}

	// Got more data, retry tokenization
	return true, nil
}

// fillBuffer reads more data into the buffer.
func (ts *TokenSource) fillBuffer() error {
	// If buffer is getting large and we've consumed a lot, compact it
	if ts.bufPos > ts.bufferSize && len(ts.buf) > ts.bufferSize*2 {
		// Move remaining data to front
		remaining := ts.buf[ts.bufPos:]
		copy(ts.buf, remaining)
		ts.buf = ts.buf[:len(remaining)]
		ts.bufStart += ts.bufPos
		ts.bufPos = 0
	}

	// Read into buffer
	readBuf := make([]byte, ts.bufferSize)
	n, err := ts.reader.Read(readBuf)
	if n > 0 {
		ts.buf = append(ts.buf, readBuf[:n]...)
	}
	return err
}

// Depth returns the current bracket nesting depth.
func (ts *TokenSource) Depth() int {
	return ts.depth
}

// CurrentPath returns the current kinded path from root (e.g., "", "key", "key[0]", "a.b").
// Only tracks bracketed structures (objects with {}/arrays with []).
// Block-style arrays and objects are not tracked.
func (ts *TokenSource) CurrentPath() string {
	return ts.currentPath
}

// updateDepth updates the nesting depth based on structural tokens.
func (ts *TokenSource) updateDepth(tok Token) {
	switch tok.Type {
	case TLCurl, TLSquare:
		// Opening bracket increases depth
		ts.depth++
	case TRCurl, TRSquare:
		// Closing bracket decreases depth
		if ts.depth > 0 {
			ts.depth--
		}
	}
}

// updatePath updates the current path based on the token.
// Only tracks bracketed structures - block-style arrays (TArrayElt) are skipped.
func (ts *TokenSource) updatePath(tok Token) {
	switch tok.Type {
	case TDocSep:
		// Document separator - reset to root
		ts.currentPath = "" // Root path is empty string in kinded path syntax
		ts.pathStack = nil
		ts.bracketStack = nil
		ts.arrayIndex = 0
		ts.pendingKey = ""
		ts.pendingInt = ""
		ts.pendingValue = false

	case TInteger:
		// If we're in an array context and no pending key, this is an array element value
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare && ts.pendingKey == "" {
			// Array element value - append array index to path
			ts.appendArrayIndexToPath()
			ts.pendingValue = true
		} else {
			// Might be a sparse array index - store it and wait for TColon
			ts.pendingInt = string(tok.Bytes)
			ts.pendingKey = ""
		}

	case TLiteral:
		// If we're in an array context (top of bracketStack is TLSquare),
		// and we don't have a pending key, this is an array element value
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare && ts.pendingKey == "" {
			// Array element value - append array index to path
			ts.appendArrayIndexToPath()
			ts.pendingValue = true
		} else if ts.pendingValue {
			// We're expecting a value (after TColon), so this is a value, not a key
			// Path was already set by the key, so no change needed
			ts.pendingValue = false
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// We're in an object context
			if ts.pendingKey != "" {
				// We have a pending key from before - process it as a key with implicit value
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
			}
			// Store the new literal as pending key (waiting for TColon or next key)
			ts.pendingKey = string(tok.Bytes)
		} else {
			// Might be a key - store it and wait for TColon
			ts.pendingKey = string(tok.Bytes)
		}

	case TString:
		// TString can be either a key or a value
		// First check if we're in an array context (even if pendingValue is true,
		// we might be processing an array element value)
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare && ts.pendingKey == "" {
			// If we're in an array context and no pending key, this is an array element value
			// Array element value - append array index to path
			ts.appendArrayIndexToPath()
			ts.pendingValue = true
		} else if ts.pendingValue {
			// We're expecting a value (after TColon), so this is a value, not a key
			// Path was already set by the key, so no change needed
			ts.pendingValue = false
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// We're in an object context - this could be a key
			if ts.pendingKey != "" {
				// We have a pending key from before - process it as a key with implicit value
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
			}
			// Store the string as pending key (waiting for TColon or next key)
			// Use String() method to get the unquoted value
			ts.pendingKey = tok.String()
		} else {
			// Might be a key - store it and wait for TColon
			ts.pendingKey = tok.String()
		}

	case TColon:
		// Check if previous token was an integer (sparse array), literal (key), or string (key)
		if ts.pendingInt != "" {
			// Sparse array index - append {index} to path (kinded path syntax)
			// If path already ends with an array/sparse index, reset to parent path
			lastBrace := strings.LastIndex(ts.currentPath, "{")
			lastBracket := strings.LastIndex(ts.currentPath, "[")
			lastIndex := lastBrace
			if lastBracket > lastIndex {
				lastIndex = lastBracket
			}
			if lastIndex > 0 {
				// Path ends with index - reset to parent (before the last "[" or "{")
				ts.currentPath = ts.currentPath[:lastIndex]
			} else if ts.depth == 0 && (strings.HasPrefix(ts.currentPath, "[") || strings.HasPrefix(ts.currentPath, "{")) {
				// Root level sparse array - reset to ""
				ts.currentPath = ""
			}
			ts.currentPath += "{" + ts.pendingInt + "}" // Use {index} for sparse arrays
			ts.pendingInt = ""
			// Mark that next token will be a value (so TInteger won't treat it as a key)
			ts.pendingValue = true
		} else if ts.pendingKey != "" {
			// The previous TLiteral or TString was a key
			// If we're in an object context, reset to object base before appending key
			// This handles optional commas between key-value pairs
			if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
				// Reset to object base (from pathStack) before appending new key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
			}
			ts.appendKeyToPath(ts.pendingKey)
			ts.pendingKey = ""
			// Mark that next token will be a value
			ts.pendingValue = true
		}

	case TArrayElt:
		// Block-style array element - skip path tracking
		// Path tracking only works for bracketed structures (TLSquare/TRSquare)
		// Block-style arrays don't have explicit boundaries, so we can't track them accurately

	case TLCurl:
		// Opening object - push current path and bracket type to stack
		ts.pathStack = append(ts.pathStack, ts.currentPath)
		ts.bracketStack = append(ts.bracketStack, TLCurl)
		// Path doesn't change until we see a key
		// Reset pendingValue - new structure starts, next token could be a key
		ts.pendingValue = false

	case TLSquare:
		// Opening array - push current path and bracket type to stack, reset index
		ts.pathStack = append(ts.pathStack, ts.currentPath)
		ts.bracketStack = append(ts.bracketStack, TLSquare)
		ts.arrayIndex = 0
		// Path doesn't change until we see an element
		// Reset pendingValue - new structure starts
		ts.pendingValue = false

	case TRCurl, TRSquare:
		// Closing bracket - pop path stack and bracket stack
		// If we're closing an object and have a pending key, process it first
		if tok.Type == TRCurl && ts.pendingKey != "" {
			// Process the pending key as a key with implicit value
			// Reset to object base before appending key
			if len(ts.pathStack) > 0 {
				ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
			}
			ts.appendKeyToPath(ts.pendingKey)
			ts.pendingKey = ""
		}
		if len(ts.pathStack) > 0 {
			ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
			ts.pathStack = ts.pathStack[:len(ts.pathStack)-1]
		}
		if len(ts.bracketStack) > 0 {
			ts.bracketStack = ts.bracketStack[:len(ts.bracketStack)-1]
		}
		ts.arrayIndex = 0
		ts.pendingKey = ""
		ts.pendingInt = ""
		ts.pendingValue = false

	case TComma:
		// In bracketed arrays, comma separates elements
		// Reset path to array base (removing current array index) for next element
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare {
			// We're in an array - reset to base path (before array index)
			lastBracket := strings.LastIndex(ts.currentPath, "[")
			if lastBracket > 0 {
				ts.currentPath = ts.currentPath[:lastBracket]
			} else if strings.HasPrefix(ts.currentPath, "[") {
				ts.currentPath = "" // Root level array - reset to empty string
			}
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// In objects, comma separates key-value pairs
			// If we have a pending key, process it as a key with implicit value
			if ts.pendingKey != "" {
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
			} else {
				// Reset to object base for next key-value pair
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
			}
		}
		ts.pendingValue = false

	case TFloat, TTrue, TFalse, TNull, TMString, TMLit:
		// Value tokens - if we're in an array context, treat as array element
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare && ts.pendingKey == "" {
			// Array element value - append array index to path
			ts.appendArrayIndexToPath()
			ts.pendingValue = true
		}
	}
}

// appendKeyToPath appends a key to the current path using kinded path syntax.
// Handles special characters by quoting the key.
func (ts *TokenSource) appendKeyToPath(key string) {
	// Use Quote to properly quote the key if needed
	needsQuote := KPathQuoteField(key)
	if ts.currentPath == "" {
		// First field - no leading dot
		if needsQuote {
			ts.currentPath = Quote(key, true)
		} else {
			ts.currentPath = key
		}
	} else {
		// Subsequent field - add dot separator
		if needsQuote {
			ts.currentPath += "." + Quote(key, true)
		} else {
			ts.currentPath += "." + key
		}
	}
}

// appendArrayIndexToPath appends the current array index to the path using kinded path syntax.
// If the path already ends with an array index, it resets to the base first.
// This is called when processing elements in bracketed arrays.
func (ts *TokenSource) appendArrayIndexToPath() {
	// Reset to array base if path already ends with an array index
	// This handles cases where we see consecutive array elements without commas
	lastBracket := strings.LastIndex(ts.currentPath, "[")
	if lastBracket > 0 {
		// Path ends with array index - reset to parent (before the last "[")
		ts.currentPath = ts.currentPath[:lastBracket]
	} else if strings.HasPrefix(ts.currentPath, "[") {
		// Root level array - reset to ""
		ts.currentPath = ""
	}
	ts.currentPath += "[" + strconv.Itoa(ts.arrayIndex) + "]"
	ts.arrayIndex++
}
