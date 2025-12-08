package parse

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// NodeParser provides incremental parsing of ir.Node from a token source.
// It uses the existing TokenSource (from token package) for streaming tokenization.
//
// Note: Currently only supports bracketed structures ({...} or [...]).
// Non-bracketed structures will return an error.
//
// Example usage:
//
//	reader := bytes.NewReader([]byte("{key: value}{other: node}"))
//	source := token.NewTokenSource(reader)
//	parser := NewNodeParser(source)
//
//	for {
//	    node, err := parser.ParseNext()
//	    if err == io.EOF {
//	        break
//	    }
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    // Process node incrementally
//	    processNode(node)
//	}
type NodeParser struct {
	source      *token.TokenSource
	buffer      []token.Token
	bracketDepth int
	opts        *parseOpts
	// unreadTokens holds tokens that were read but not consumed (e.g., when checking for trailing comments)
	unreadTokens []token.Token
}

// NewNodeParser creates a new NodeParser reading from the given TokenSource.
func NewNodeParser(source *token.TokenSource, opts ...ParseOption) *NodeParser {
	pOpts := &parseOpts{format: format.TonyFormat}
	for _, f := range opts {
		f(pOpts)
	}

	return &NodeParser{
		source: source,
		opts:   pOpts,
	}
}

// ParseNext parses the next complete ir.Node from the token source.
// Returns:
//   - node: The parsed node, or nil if no more nodes available
//   - err: Error if parsing fails, or io.EOF if source is exhausted
//
// Supports both bracketed structures ({...} or [...]) and simple values
// (strings, numbers, booleans, null).
func (p *NodeParser) ParseNext() (*ir.Node, error) {
	// Reset state for new node
	p.buffer = p.buffer[:0]
	p.bracketDepth = 0

	// Read leading comments (similar to comments() in parse.go)
	// These will be attached to the node we're about to parse
	var leadingComments []token.Token
	var firstNodeToken *token.Token
	var firstNodeTokenBatch []token.Token

	// Read tokens to find leading comments and the first node token
	// First check unreadTokens buffer
	if len(p.unreadTokens) > 0 {
		tokens := p.unreadTokens
		p.unreadTokens = nil
		for i, tok := range tokens {
			if tok.Type == token.TComment {
				leadingComments = append(leadingComments, tok)
				continue
			}
			if tok.Type == token.TIndent {
				continue // Skip indents
			}
			// Non-comment, non-indent token - this is the start of the node
			firstNodeToken = &tok
			firstNodeTokenBatch = tokens[i:] // Save remaining tokens from this batch
			goto foundNodeStart
		}
	}

	for {
		tokens, err := p.source.Read()
		if err == io.EOF {
			if len(leadingComments) == 0 {
				return nil, io.EOF
			}
			// Only comments, no actual node
			return nil, io.EOF
		}
		if err != nil {
			return nil, err
		}
		if len(tokens) == 0 {
			continue
		}

		// Process tokens in this batch
		for i, tok := range tokens {
			if tok.Type == token.TComment {
				leadingComments = append(leadingComments, tok)
				continue
			}
			if tok.Type == token.TIndent {
				continue // Skip indents
			}
			// Non-comment, non-indent token - this is the start of the node
			firstNodeToken = &tok
			firstNodeTokenBatch = tokens[i:] // Save remaining tokens from this batch
			goto foundNodeStart
		}
	}

foundNodeStart:
	if firstNodeToken == nil {
		return nil, io.EOF
	}

	// Now process the node starting from firstNodeToken
	var firstTokenFound bool

	// Process the first token and any remaining tokens from that batch
	tokensToProcess := firstNodeTokenBatch
	firstNodeTokenBatch = nil // Clear to avoid re-processing

	for {
		// Process tokens from current batch
		for _, tok := range tokensToProcess {
			// Find the first meaningful token
			if !firstTokenFound {
				switch tok.Type {
				case token.TLCurl, token.TLSquare:
					// Bracketed structure - track it
					firstTokenFound = true
					p.buffer = append(p.buffer, tok)
					p.bracketDepth++
					continue
				case token.TString, token.TMString, token.TLiteral, token.TMLit,
					token.TInteger, token.TFloat, token.TTrue, token.TFalse, token.TNull:
					// Simple value - parse it directly (with leading comments)
					return p.parseSimpleValue(&tok, leadingComments)
				default:
					// Unexpected token
					return nil, fmt.Errorf("unexpected token %s at %s (expected bracketed structure or simple value)",
						tok.Type, tok.Pos)
				}
			}

			// We've found the first token - continue processing
			p.buffer = append(p.buffer, tok)

			// Update bracket depth for bracketed structures
			switch tok.Type {
			case token.TLCurl, token.TLSquare:
				p.bracketDepth++
			case token.TRCurl, token.TRSquare:
				p.bracketDepth--
				if p.bracketDepth < 0 {
					return nil, fmt.Errorf("unmatched closing bracket at %s", tok.Pos)
				}
			}

			// Check if we have a complete bracketed structure (depth returned to 0)
			if p.bracketDepth == 0 && firstTokenFound {
				// We have a complete bracketed structure
				goto parse
			}
		}

		// Read next batch of tokens
		var err error
		tokensToProcess, err = p.source.Read()
		if err == io.EOF {
			if len(p.buffer) == 0 {
				return nil, io.EOF
			}
			// EOF reached but we have tokens - check if complete
			if p.bracketDepth != 0 {
				return nil, fmt.Errorf("incomplete bracketed structure: unmatched brackets (depth=%d)", p.bracketDepth)
			}
			if !firstTokenFound {
				return nil, io.EOF
			}
			break
		}
		if err != nil {
			return nil, err
		}
	}

parse:
	// Use existing parseTokens function - it handles balancing and parsing
	// We need to prepend leading comments to the buffer for parseTokens to handle
	allToks := append(leadingComments, p.buffer...)
	node, err := parseTokens(allToks, p.opts)
	if err != nil {
		return nil, err
	}

	// Clear buffer for next node
	p.buffer = p.buffer[:0]

	return node, nil
}

// parseSimpleValue parses a simple value token with leading and trailing comments.
// This handles the same cases as noComments() in parse.go for simple values.
// Leading comments are attached as a CommentType wrapper node (like parseBalanced does).
// Trailing comments are only read if they're on the same line as the value.
func (p *NodeParser) parseSimpleValue(tok *token.Token, leadingComments []token.Token) (*ir.Node, error) {
	var node *ir.Node
	var pos *token.Pos

	switch tok.Type {
	case token.TLiteral, token.TString:
		node = ir.FromString(tok.String())
		pos = tok.Pos
	case token.TMString:
		node = ir.FromString(tok.String())
		pos = tok.Pos
		// TMString may have multiple lines
		parts := bytes.Split(tok.Bytes, []byte{'\n'})
		for _, part := range parts {
			node.Lines = append(node.Lines, token.QuotedToString(part))
		}
		// For TMString, check for N comment tokens (one per line)
		// We need to read more tokens to check for comments
		return p.parseSimpleValueWithComments(node, pos, true)
	case token.TMLit:
		node = ir.FromString(tok.String())
		pos = tok.Pos
	case token.TInteger:
		pos = tok.Pos
		i, err := strconv.ParseInt(string(tok.Bytes), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %w: %s", err, tok.Pos)
		}
		node = ir.FromInt(i)
	case token.TFloat:
		pos = tok.Pos
		f, err := strconv.ParseFloat(string(tok.Bytes), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %w: %s", err, tok.Pos)
		}
		node = ir.FromFloat(f)
	case token.TTrue:
		pos = tok.Pos
		node = ir.FromBool(true)
	case token.TFalse:
		pos = tok.Pos
		node = ir.FromBool(false)
	case token.TNull:
		pos = tok.Pos
		node = ir.Null()
	default:
		return nil, fmt.Errorf("unexpected token type for simple value: %s", tok.Type)
	}

	// Track position if needed
	trackPos(node, pos, p.opts)

	// Handle leading comments (wrap node in CommentType if we have leading comments)
	var result *ir.Node = node
	if len(leadingComments) > 0 && p.opts.comments {
		commentLines := make([]string, len(leadingComments))
		for i, c := range leadingComments {
			commentLines[i] = strings.TrimSpace(string(c.Bytes))
		}
		result = &ir.Node{
			Type:   ir.CommentType,
			Lines:  commentLines,
			Values: []*ir.Node{node},
		}
		node.Parent = result
		node.ParentIndex = 0
	}

	// Check for trailing comments (only on same line)
	return p.parseSimpleValueWithComments(result, pos, false)
}

// parseSimpleValueWithComments reads trailing comments and attaches them to the node.
// For TMString, multiline comments are handled separately.
func (p *NodeParser) parseSimpleValueWithComments(node *ir.Node, pos *token.Pos, isMultilineString bool) (*ir.Node, error) {
	if isMultilineString && p.opts.comments {
		// For TMString, check for N comment tokens (one per line)
		// Similar to checkMultilineStringComments in parse.go
		numLines := len(node.Lines)
		if numLines == 0 {
			return node, nil
		}

		commentLines := make([]string, 0, numLines)
		for len(commentLines) < numLines {
			tokens, err := p.source.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if len(tokens) == 0 {
				break
			}

			for _, tok := range tokens {
				if tok.Type == token.TComment {
					commentLines = append(commentLines, string(tok.Bytes))
					if len(commentLines) >= numLines {
						break
					}
				} else if tok.Type == token.TIndent {
					// Skip TIndent tokens between comments
					continue
				} else {
					// Non-comment, non-indent token - stop collecting
					// We've read too far, but that's okay - we'll just use what we have
					goto doneMultiline
				}
			}
		}

	doneMultiline:
		// Pad with empty strings if we have fewer comments than lines
		for len(commentLines) < numLines {
			commentLines = append(commentLines, "")
		}

		// Create Comment node with all comment lines
		node.Comment = &ir.Node{
			Parent: node,
			Type:   ir.CommentType,
			Lines:  commentLines,
		}
		return node, nil
	}

	// For regular simple values, check for trailing comments on the same line
	// Comments on subsequent lines are NOT associated here - they will be
	// re-associated later by associateComments() (similar to parse.go behavior)
	//
	// This matches checkLineComment behavior: read consecutive comments, but
	// only those that are on the same line as the value token.
	if node.Comment != nil {
		return node, nil
	}

	// Read trailing comments, but only if they're on the same line as the value
	// Process all tokens in the batch, collecting comments and stopping on first non-comment
	for {
		var tokens []token.Token
		var err error

		// Check unreadTokens buffer first
		if len(p.unreadTokens) > 0 {
			tokens = p.unreadTokens
			p.unreadTokens = nil
		} else {
			tokens, err = p.source.Read()
			if err == io.EOF {
				// End of document - stop reading
				return node, nil
			}
			if err != nil {
				return nil, err
			}
			if len(tokens) == 0 {
				return node, nil
			}
		}

		// Process all tokens in the batch
		var remainingTokens []token.Token
		for i, tok := range tokens {
			// Only read comments on the same line as the value token
			// Comments on subsequent lines belong to the next node
			if tok.Type != token.TComment {
				// Found non-comment token - save remaining tokens for next ParseNext()
				remainingTokens = tokens[i:]
				break
			}

			// Stop reading if comment is on a different line
			if tok.Pos.Line() > pos.Line() {
				// Comment on different line - save this and remaining tokens for next ParseNext()
				remainingTokens = tokens[i:]
				break
			}

			// Found a comment on the same line - attach it to the node
			if p.opts.comments {
				if node.Comment == nil {
					node.Comment = &ir.Node{Parent: node, Type: ir.CommentType}
				}
				node.Comment.Lines = append(node.Comment.Lines, string(tok.Bytes))
			}
		}

		// If we found non-comment tokens, save them for next ParseNext()
		if len(remainingTokens) > 0 {
			p.unreadTokens = remainingTokens
			return node, nil
		}
	}
}
