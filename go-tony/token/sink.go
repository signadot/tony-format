package token

import (
	"io"
	"strconv"
	"strings"
)

// NodeOffsetCallback is called when a node starts in the output stream.
// The offset is the absolute byte position where the node begins.
// The path is the kinded path from document root (e.g., "", "key", "key[0]", "a.b.c", "a{0}").
// The token is the token that triggered the node start detection.
type NodeOffsetCallback func(offset int, path string, token Token)

// TokenSink provides streaming token encoding to an io.Writer.
// It tracks absolute byte offsets and calls a callback when nodes start.
type TokenSink struct {
	writer io.Writer

	// Offset tracking
	offset int // Current absolute byte offset in output stream

	// Node start detection
	onNodeStart NodeOffsetCallback // Callback when a node starts (can be nil)

	// Encoding state
	depth    int       // Current nesting depth (for indentation tracking)
	lastType TokenType // Last token type written (for spacing)
	
	// Node start detection state
	nextIsNodeStart bool // True if next token is a node start (after TArrayElt or TColon)
	keyProcessed bool   // True if we just processed a key (for triggering node start)
	
	// Path tracking (using kinded path syntax: "", "key", "key[0]", "key{0}", "a.b.c")
	currentPath string   // Current kinded path from root (e.g., "", "key", "key[0]", "a.b")
	pathStack   []string // Stack of path components for nested structures
	bracketStack []TokenType // Stack of bracket types ('[' or '{') for each level
	arrayIndex  int      // Current array index (incremented in bracketed arrays)
	pendingKey  string   // Key name seen before TColon (TLiteral)
	pendingInt  string   // Integer key seen before TColon (for sparse arrays)
	pendingValue bool    // True if we've seen a value token that needs path update
}

// NewTokenSink creates a new TokenSink writing to w.
// If onNodeStart is provided, it will be called whenever a node starts.
func NewTokenSink(w io.Writer, onNodeStart NodeOffsetCallback) *TokenSink {
	return &TokenSink{
		writer:      w,
		onNodeStart: onNodeStart,
		currentPath: "", // Root path is empty string in kinded path syntax
	}
}

// Write writes tokens to the underlying io.Writer.
// It tracks absolute byte offsets and detects node starts.
func (ts *TokenSink) Write(tokens []Token) error {
	for _, tok := range tokens {
		// Update path before detecting node start
		ts.updatePath(tok)
		
		// Detect node start before writing
		// TArrayElt and TColon indicate the NEXT token is a node start
		
		// Handle keyProcessed flag - must be cleared even if callback is nil
		if ts.keyProcessed {
			// We just processed a key (without a value) - trigger node start
			// Use the token that triggered this (could be TComma, TLiteral, TInteger, TRCurl, etc.)
			if ts.onNodeStart != nil {
				ts.onNodeStart(ts.offset, ts.currentPath, tok)
			}
			ts.keyProcessed = false // Always clear, even if callback is nil
			// If this was triggered by TRCurl, now pop the stack
			if tok.Type == TRCurl {
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
					ts.pathStack = ts.pathStack[:len(ts.pathStack)-1]
				}
				if len(ts.bracketStack) > 0 {
					ts.bracketStack = ts.bracketStack[:len(ts.bracketStack)-1]
				}
				ts.pendingKey = ""
				ts.pendingInt = ""
			}
		} else if ts.onNodeStart != nil {
			// Only check other node start conditions if callback is not nil
			if ts.nextIsNodeStart {
				ts.onNodeStart(ts.offset, ts.currentPath, tok)
				ts.nextIsNodeStart = false
			} else if ts.isNodeStart(tok) {
				ts.onNodeStart(ts.offset, ts.currentPath, tok)
			}
		}

		// Mark that next token is a node start if this is TArrayElt or TColon
		if tok.Type == TArrayElt || tok.Type == TColon {
			ts.nextIsNodeStart = true
		}
		
		// Reset nextIsNodeStart when structural tokens appear (they consume the expectation)
		// or when value tokens are processed (they consume it in updatePath)
		if tok.Type == TLCurl || tok.Type == TLSquare {
			// Structural tokens are node starts themselves, so they consume nextIsNodeStart
			ts.nextIsNodeStart = false
		}

		// Format and write token
		var toWrite []byte

		// Handle TIndent - write newline + spaces
		// But skip if last token was TDocSep (which already includes newline)
		if tok.Type == TIndent {
			if ts.lastType != TDocSep {
				toWrite = append(toWrite, '\n')
			}
			toWrite = append(toWrite, tok.Bytes...)
		} else {
			// Add newline before document separator if not at start
			if tok.Type == TDocSep && ts.lastType != TIndent && ts.lastType != TDocSep {
				toWrite = append(toWrite, '\n')
			}
			// Add space after colon, before value
			if ts.lastType == TColon && tok.Type != TColon && tok.Type != TIndent && tok.Type != TDocSep {
				toWrite = append(toWrite, ' ')
			}
			// Add space after tag, before value
			if ts.lastType == TTag && tok.Type != TColon && tok.Type != TIndent && tok.Type != TDocSep {
				toWrite = append(toWrite, ' ')
			}
			// Add space before comment if not at start of line
			if tok.Type == TComment && ts.lastType != TIndent && ts.lastType != TDocSep {
				toWrite = append(toWrite, ' ')
			}
			toWrite = append(toWrite, tok.Bytes...)
		}

		// Write formatted bytes
		n, err := ts.writer.Write(toWrite)
		if err != nil {
			return err
		}

		// Update offset
		ts.offset += n

		// Remember last token type for spacing
		ts.lastType = tok.Type

		// Update depth for structural tokens
		ts.updateDepth(tok)
	}

	return nil
}

// Offset returns the current absolute byte offset in the output stream.
func (ts *TokenSink) Offset() int {
	return ts.offset
}

// isNodeStart determines if a token indicates the start of a new node.
// According to Tony format, nodes start at:
// - Document separator (TDocSep) - new document
// - Indent at depth 0 - new top-level value
// - Opening brackets (TLCurl, TLSquare) - new collection
// - Value tokens in array context (when pendingValue is true after updatePath)
// Note: TArrayElt and TColon indicate the NEXT token is a node start, not this one.
func (ts *TokenSink) isNodeStart(tok Token) bool {
	switch tok.Type {
	case TDocSep:
		// Document separator - definitely a new node
		return true
	case TIndent:
		// Indent at depth 0 indicates a new top-level value
		// (depth is tracked separately, but TIndent at start of line usually means new value)
		return ts.depth == 0
	case TLCurl, TLSquare:
		// Opening bracket - new collection node
		return true
	case TLiteral, TString, TInteger, TFloat, TTrue, TFalse, TNull, TMString, TMLit:
		// Value tokens - if we just updated path for an array element, this is a node start
		return ts.pendingValue
	default:
		return false
	}
}

// updateDepth updates the nesting depth based on structural tokens.
func (ts *TokenSink) updateDepth(tok Token) {
	switch tok.Type {
	case TLCurl, TLSquare:
		// Opening bracket increases depth
		ts.depth++
	case TRCurl, TRSquare:
		// Closing bracket decreases depth
		if ts.depth > 0 {
			ts.depth--
		}
	// Note: TIndent doesn't change depth - depth is about bracket nesting,
	// not indentation level
	}
}

// updatePath updates the current path based on the token.
// Only tracks bracketed structures (objects with {}/arrays with []).
// Block-style arrays (TArrayElt) are not tracked as they lack explicit boundaries.
func (ts *TokenSink) updatePath(tok Token) {
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
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// We're in an object context
			// Check if nextIsNodeStart is set (meaning previous token was TColon)
			// If so, this integer is a VALUE, not a key
			if ts.nextIsNodeStart {
				// This is a value after TColon - don't process as key
				// Path was already set by the key, so no change needed
				ts.pendingValue = true
				// Consume the nextIsNodeStart flag
				ts.nextIsNodeStart = false
			} else if ts.pendingInt != "" {
				// We have a pending integer key from before - process it as a sparse array key with implicit value
				// Reset to object base before appending sparse index
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.currentPath += "{" + ts.pendingInt + "}" // Use {index} for sparse arrays
				ts.pendingInt = ""
				ts.keyProcessed = true // Mark that we processed a key
				// Store the new integer as pending (waiting for TColon or next integer)
				ts.pendingInt = string(tok.Bytes)
				ts.pendingKey = ""
			} else {
				// Store the new integer as pending (waiting for TColon or next integer)
				ts.pendingInt = string(tok.Bytes)
				ts.pendingKey = ""
			}
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
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// We're in an object context
			// Check if nextIsNodeStart is set (meaning previous token was TColon)
			// If so, this literal is a VALUE, not a key
			if ts.nextIsNodeStart {
				// This is a value after TColon - don't process as key
				// Path was already set by the key, so no change needed
				ts.pendingValue = true
				// Do NOT set pendingKey - this is a value, not a key
			} else if ts.pendingKey != "" {
				// We have a pending key from before - process it as a key with implicit value
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
				ts.keyProcessed = true // Mark that we processed a key
			}
			// Store the new literal as pending key (waiting for TColon or next key)
			// Only if it's NOT a value (nextIsNodeStart is false)
			if !ts.nextIsNodeStart {
				ts.pendingKey = string(tok.Bytes)
			} else {
				// This was a value - consume the nextIsNodeStart flag
				ts.nextIsNodeStart = false
			}
		} else {
			// Might be a key - store it and wait for TColon
			ts.pendingKey = string(tok.Bytes)
		}
		
	case TString:
		// TString can be either a key or a value
		// First check if we're in an array context (even if nextIsNodeStart is true,
		// we might be processing an array element value)
		if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLSquare && ts.pendingKey == "" {
			// Array element value - append array index to path
			ts.appendArrayIndexToPath()
			ts.pendingValue = true
		} else if len(ts.bracketStack) > 0 && ts.bracketStack[len(ts.bracketStack)-1] == TLCurl {
			// We're in an object context - this could be a key
			// Check if nextIsNodeStart is set (meaning previous token was TColon)
			// If so, this string is a VALUE, not a key
			if ts.nextIsNodeStart {
				// This is a value after TColon - don't process as key
				// Path was already set by the key, so no change needed
				ts.pendingValue = true
				// Do NOT set pendingKey - this is a value, not a key
			} else if ts.pendingKey != "" {
				// We have a pending key from before - process it as a key with implicit value
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
				ts.keyProcessed = true // Mark that we processed a key
			}
			// Store the string as pending key (waiting for TColon or next key)
			// Only if it's NOT a value (nextIsNodeStart is false)
			if !ts.nextIsNodeStart {
				// Use String() method to get the unquoted value
				ts.pendingKey = tok.String()
			} else {
				// This was a value - consume the nextIsNodeStart flag
				ts.nextIsNodeStart = false
			}
		} else if ts.pendingValue || ts.nextIsNodeStart {
			// We're expecting a value (after TColon), so this is a value, not a key
			ts.pendingValue = false
			// Consume the nextIsNodeStart flag if it was set
			if ts.nextIsNodeStart {
				ts.nextIsNodeStart = false
			}
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
		
	case TLSquare:
		// Opening array - push current path and bracket type to stack, reset index
		ts.pathStack = append(ts.pathStack, ts.currentPath)
		ts.bracketStack = append(ts.bracketStack, TLSquare)
		ts.arrayIndex = 0
		// Path doesn't change until we see an element
		
	case TRCurl, TRSquare:
		// Closing bracket - pop path stack and bracket stack
		// If we're closing an object and have a pending key or pending int, process it first
		// (but don't pop the stack yet - we need the path for the callback)
		if tok.Type == TRCurl {
			if ts.pendingKey != "" {
				// Process the pending key as a key with implicit value
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
				ts.keyProcessed = true // Mark that we processed a key
				// Don't pop stack yet - the callback will use currentPath
			} else if ts.pendingInt != "" {
				// Process the pending integer as a sparse array key with implicit value
				// Reset to object base before appending sparse index
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.currentPath += "{" + ts.pendingInt + "}" // Use {index} for sparse arrays
				ts.pendingInt = ""
				ts.keyProcessed = true // Mark that we processed a key
				// Don't pop stack yet - the callback will use currentPath
			} else {
				// No pending key - safe to pop stack immediately
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
					ts.pathStack = ts.pathStack[:len(ts.pathStack)-1]
				}
				if len(ts.bracketStack) > 0 {
					ts.bracketStack = ts.bracketStack[:len(ts.bracketStack)-1]
				}
			}
		} else {
			// TRSquare - no special handling needed
			if len(ts.pathStack) > 0 {
				ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				ts.pathStack = ts.pathStack[:len(ts.pathStack)-1]
			}
			if len(ts.bracketStack) > 0 {
				ts.bracketStack = ts.bracketStack[:len(ts.bracketStack)-1]
			}
		}
		ts.arrayIndex = 0
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
			// If we have a pending key or pending int, process it as a key with implicit value
			if ts.pendingKey != "" {
				// Reset to object base before appending key
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.appendKeyToPath(ts.pendingKey)
				ts.pendingKey = ""
				ts.keyProcessed = true // Mark that we processed a key
			} else if ts.pendingInt != "" {
				// Process the pending integer as a sparse array key with implicit value
				// Reset to object base before appending sparse index
				if len(ts.pathStack) > 0 {
					ts.currentPath = ts.pathStack[len(ts.pathStack)-1]
				}
				ts.currentPath += "{" + ts.pendingInt + "}" // Use {index} for sparse arrays
				ts.pendingInt = ""
				ts.keyProcessed = true // Mark that we processed a key
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
		// Note: In object context, these are values, so they don't change the path
		// (path was already set by the key)
		// Consume nextIsNodeStart if it was set (these tokens are values after TColon)
		if ts.nextIsNodeStart {
			ts.nextIsNodeStart = false
		}
	}
}

// appendKeyToPath appends a key to the current path using kinded path syntax.
// Handles special characters by quoting the key.
func (ts *TokenSink) appendKeyToPath(key string) {
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
func (ts *TokenSink) appendArrayIndexToPath() {
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
