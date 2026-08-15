package parse

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

func Parse(d []byte, opts ...ParseOption) (*ir.Node, error) {
	pOpts := &parseOpts{format: format.TonyFormat}
	for _, f := range opts {
		f(pOpts)
	}
	toks, err := token.Tokenize(nil, d, pOpts.TokenizeOpts()...)
	if err != nil {
		return nil, err
	}
	// Skip leading document separator (---) - common in YAML files from tools like controller-gen
	for len(toks) > 0 && toks[0].Type == token.TDocSep {
		toks = toks[1:]
	}
	// A whitespace-only body is an empty document. Every kind of blank input
	// tokenizes to nothing but TIndent tokens; without this, a body of spaces
	// ("   ") reaches Balance as a lone non-empty indent and fails with a confusing
	// "extraneous indent", while "\n\t\n" or "" quietly return (nil, nil). Treat
	// them all alike.
	if allBlank(toks) {
		return nil, nil
	}
	bal, err := token.Balance(toks, pOpts.format)
	if err != nil {
		return nil, err
	}
	off := 0
	res, err := parseBalanced(bal, nil, "", &off, pOpts)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	if pOpts.comments {
		res = associateComments(res)
	}
	return res, nil
}

// allBlank reports whether toks contains only indent tokens, i.e. the input is
// whitespace-only (a blank document).
func allBlank(toks []token.Token) bool {
	if len(toks) == 0 {
		return false // empty stream is handled by the normal path (returns nil, nil)
	}
	for i := range toks {
		if toks[i].Type != token.TIndent {
			return false
		}
	}
	return true
}

// ParseMulti parses multiple Tony documents separated by '---' from a single byte slice.
// It preserves global positions for all documents.
func ParseMulti(d []byte, opts ...ParseOption) ([]*ir.Node, error) {
	pOpts := &parseOpts{format: format.TonyFormat}
	for _, f := range opts {
		f(pOpts)
	}
	toks, err := token.Tokenize(nil, d, pOpts.TokenizeOpts()...)
	if err != nil {
		return nil, err
	}

	var nodes []*ir.Node
	var currentToks []token.Token

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.Type != token.TDocSep {
			currentToks = append(currentToks, t)
			continue
		}
		if len(currentToks) == 0 {
			continue
		}
		node, err := parseTokens(currentToks, pOpts)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
		currentToks = nil
	}

	// Parse the last document if any tokens remain
	if len(currentToks) > 0 {
		node, err := parseTokens(currentToks, pOpts)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}

	return nodes, nil
}

func parseTokens(toks []token.Token, pOpts *parseOpts) (*ir.Node, error) {
	bal, err := token.Balance(toks, pOpts.format)
	if err != nil {
		return nil, err
	}
	off := 0
	res, err := parseBalanced(bal, nil, "", &off, pOpts)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	if pOpts.comments {
		res = associateComments(res)
	}
	return res, nil
}

// ParseNodeFromSource parses the next complete ir.Node from a TokenSource.
// It reads tokens until it finds a complete bracketed structure or simple value,
// then parses and returns it. Returns io.EOF when the source is exhausted.
//
// This is a simplified replacement for NodeParser.ParseNext() that handles
// the core incremental parsing use case.
func ParseNodeFromSource(source *token.TokenSource, opts ...ParseOption) (*ir.Node, error) {
	pOpts := &parseOpts{format: format.TonyFormat}
	for _, f := range opts {
		f(pOpts)
	}

	var buffer []token.Token
	var leadingComments []token.Token
	var bracketDepth int
	var firstTokenFound bool

	// Read tokens to find leading comments and the first node token
	for {
		tokens, err := source.Read()
		if err == io.EOF {
			if len(buffer) == 0 {
				return nil, io.EOF
			}
			// EOF reached but we have tokens - check if complete
			if bracketDepth != 0 {
				return nil, fmt.Errorf("incomplete bracketed structure: unmatched brackets (depth=%d)", bracketDepth)
			}
			if !firstTokenFound {
				return nil, io.EOF
			}
			break
		}
		if err != nil {
			return nil, err
		}
		if len(tokens) == 0 {
			continue
		}

		// Process tokens in this batch
		for _, tok := range tokens {
			// Skip leading comments and indents
			if tok.Type == token.TComment {
				leadingComments = append(leadingComments, tok)
				continue
			}
			if tok.Type == token.TIndent {
				continue
			}

			// Find the first meaningful token
			if !firstTokenFound {
				switch tok.Type {
				case token.TLCurl, token.TLSquare:
					// Bracketed structure - track it
					firstTokenFound = true
					buffer = append(buffer, tok)
					bracketDepth++
					continue
				case token.TString, token.TMString, token.TLiteral, token.TMLit,
					token.TInteger, token.TFloat, token.TTrue, token.TFalse, token.TNull:
					// Simple value - parse it directly (simplified, without trailing comments)
					return parseSimpleValueFromToken(&tok, leadingComments, pOpts)
				default:
					return nil, fmt.Errorf("unexpected token %s at %s (expected bracketed structure or simple value)",
						tok.Type, tok.Pos)
				}
			}

			// We've found the first token - continue processing
			buffer = append(buffer, tok)

			// Update bracket depth for bracketed structures
			switch tok.Type {
			case token.TLCurl, token.TLSquare:
				bracketDepth++
			case token.TRCurl, token.TRSquare:
				bracketDepth--
				if bracketDepth < 0 {
					return nil, fmt.Errorf("unmatched closing bracket at %s", tok.Pos)
				}
			}

			// Check if we have a complete bracketed structure (depth returned to 0)
			if bracketDepth == 0 && firstTokenFound {
				// We have a complete bracketed structure
				goto parse
			}
		}
	}

parse:
	// Use existing parseTokens function - it handles balancing and parsing
	// We need to prepend leading comments to the buffer for parseTokens to handle
	allToks := append(leadingComments, buffer...)
	return parseTokens(allToks, pOpts)
}

// parseSimpleValueFromToken parses a simple value token with leading comments.
// This is a simplified version that doesn't handle trailing comments.
func parseSimpleValueFromToken(tok *token.Token, leadingComments []token.Token, pOpts *parseOpts) (*ir.Node, error) {
	var node *ir.Node
	var pos *token.Pos

	switch tok.Type {
	case token.TLiteral, token.TString:
		node = ir.FromString(tok.String())
		pos = tok.Pos
	case token.TMString:
		node = ir.FromString(tok.String())
		pos = tok.Pos
		parts := bytes.Split(tok.Bytes, []byte{'\n'})
		for _, part := range parts {
			node.Lines = append(node.Lines, token.QuotedToString(part))
		}
	case token.TMLit:
		node = ir.FromString(tok.String())
		pos = tok.Pos
	case token.TInteger, token.TFloat:
		pos = tok.Pos
		n, notation, err := numberNode(tok)
		if err != nil {
			return nil, err
		}
		node = n
		node.Tag = composeNotation(notation, node.Tag)
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
	trackPos(node, pos, pOpts)

	// Handle leading comments (wrap node in CommentType if we have leading comments)
	var result *ir.Node = node
	if len(leadingComments) > 0 && pOpts.comments {
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

	return result, nil
}
func trackPos(node *ir.Node, pos *token.Pos, opts *parseOpts) {
	if opts.positions != nil && pos != nil {
		opts.positions[node] = pos
	}
}

func parseBalanced(toks []token.Token, p *ir.Node, tag string, pi *int, opts *parseOpts) (*ir.Node, error) {
	// Collect head comments (TComment) and line comments (TLineComment) before the value.
	// Skip TMString - multiline string comments are handled by checkMultilineStringComments.
	var yComments *ir.Node
	var leadingLineComments []string
	if opts.comments && *pi < len(toks) && toks[*pi].Type != token.TMString {
		// Both kinds can precede the value, in either order, and there can be more
		// of one after the other: "spec: # latch" puts a line comment on the value
		// and the block under it may then open with head comments of its own. Taken
		// once each, in a fixed order, the head comments AFTER a latch were left for
		// the skip loop below to discard -- so a comment written inside a block was
		// silently lost whenever the field line also carried one
		// (3cdjz00jh12krns4g1n0).
		for {
			before := *pi
			if yc := comments(toks, pi, opts); yc != nil {
				if yComments == nil {
					yComments = yc
				} else {
					// One value, one set of preceding comments (docs/ir.md).
					yComments.Lines = append(yComments.Lines, yc.Lines...)
				}
			}
			// TLineComment before value becomes line comment on that value
			// (e.g., "foo: # comment\n  bar: value" - comment goes on inner object)
			for *pi < len(toks) && toks[*pi].Type == token.TLineComment {
				leadingLineComments = append(leadingLineComments, string(toks[*pi].Bytes))
				*pi++
			}
			if *pi == before {
				break
			}
		}
	}
	// Skip comment and indent tokens (even when not collecting comments)
	for *pi < len(toks) {
		t := toks[*pi].Type
		if !t.IsComment() && t != token.TIndent {
			break
		}
		*pi++
	}
	// End of array/object after comments - return head comments only
	if *pi < len(toks) {
		t := toks[*pi].Type
		if t == token.TRSquare || t == token.TRCurl {
			return yComments, nil
		}
	}
	child, err := noComments(toks, p, tag, pi, opts)
	if err != nil {
		return nil, err
	}
	// Attach leading line comments to child's .comment field
	if len(leadingLineComments) > 0 && child != nil {
		if child.Comment == nil {
			child.Comment = &ir.Node{Parent: child, Type: ir.CommentType}
		}
		child.Comment.Lines = append(leadingLineComments, child.Comment.Lines...)
	}
	if p == nil && child != nil {
		// trailing comments appended as lines to child. this records
		// the comments but places them in incorrect association with
		// nodes, as most trailing comments are actually leading
		// comments for the next item, and Tony defines them to be so
		// associated.
		//
		// It is difficult in a recursive descent parser to associate
		// comments with nodes correctly.  So, they are associated in
		// in this incorrect way here and then corrected in a subsequent pass.
		// see associateComments
		trComments := comments(toks, pi, opts)
		if trComments != nil {
			if child.Comment == nil {
				child.Comment = &ir.Node{Parent: child, Type: ir.CommentType}
			}
			// Add dummy "" placeholder if no line comment exists - this distinguishes
			// "value # comment" (line comment) from "value\n# comment" (trailing comment)
			if len(child.Comment.Lines) == 0 {
				child.Comment.Lines = []string{""}
			}
			child.Comment.Lines = append(child.Comment.Lines, trComments.Lines...)
		}
	}
	if !opts.comments || yComments == nil {
		return child, nil
	}
	if child == nil {
		return yComments, nil
	}
	return headWrap(yComments, child), nil
}

func noComments(toks []token.Token, p *ir.Node, tag string, pi *int, opts *parseOpts) (*ir.Node, error) {
	if *pi >= len(toks) {
		if tag != "" {
			return ir.Null().WithTag(tag), nil
		}
		return p, nil
	}
	t := &toks[*pi]
	switch t.Type {
	case token.TLCurl:
		if *pi == len(toks)-1 {
			return nil, fmt.Errorf("%w: unbalanced %s", errInternal, toks[*pi].Pos)
		}
		pos := t.Pos
		*pi++
		objY := &ir.Node{Type: ir.ObjectType}
		if string(t.Bytes) == "{" && !opts.noBrackets {
			tag = ir.TagCompose(ir.BracketTag, nil, tag)
		}

		trackPos(objY, pos, opts)
		return parseObj(toks, objY, tag, pi, opts)
	case token.TLSquare:
		if *pi == len(toks)-1 {
			return nil, fmt.Errorf("%w: unbalanced %s", errInternal, toks[*pi].Pos)
		}
		pos := t.Pos
		*pi++
		arrY := &ir.Node{Type: ir.ArrayType}
		if string(t.Bytes) == "[" && !opts.noBrackets {
			tag = ir.TagCompose(ir.BracketTag, nil, tag)
		}
		trackPos(arrY, pos, opts)
		return parseArr(toks, arrY, tag, pi, opts)
	case token.TLiteral, token.TString, token.TMString:
		*pi++
		sy := ir.FromString(t.String())
		sy.Parent = p
		sy.Tag = tag
		trackPos(sy, t.Pos, opts)
		if t.Type == token.TMString {
			parts := bytes.Split(t.Bytes, []byte{'\n'})
			for _, part := range parts {
				sy.Lines = append(sy.Lines, token.QuotedToString(part))
			}
			// For TMString, check for N comment tokens (one per line)
			if len(toks) > *pi && opts.comments {
				return checkMultilineStringComments(sy, toks, pi, opts), nil
			}
			return sy, nil
		}
		// For regular TString, use single line comment check
		if len(toks) == *pi {
			return sy, nil
		}
		return checkLineComment(sy, toks, pi, t.Pos, opts), nil
	case token.TMLit:
		pos := t.Pos
		*pi++
		sy := ir.FromString(t.String())
		sy.Parent = p
		sy.Tag = tag
		trackPos(sy, pos, opts)
		if len(toks) == *pi {
			return sy, nil
		}
		return checkLineComment(sy, toks, pi, t.Pos, opts), nil

	case token.TInteger, token.TFloat:
		pos := t.Pos
		ny, notation, err := numberNode(t)
		if err != nil {
			return nil, err
		}
		*pi++
		ny.Tag = composeNotation(notation, tag)
		ny.Parent = p
		trackPos(ny, pos, opts)
		if len(toks) == *pi {
			return ny, nil
		}
		return checkLineComment(ny, toks, pi, t.Pos, opts), nil
	case token.TFalse:
		pos := t.Pos
		*pi++
		b := ir.FromBool(false)
		b.Tag = tag
		b.Parent = p
		trackPos(b, pos, opts)
		return checkLineComment(b, toks, pi, t.Pos, opts), nil
	case token.TTrue:
		pos := t.Pos
		*pi++
		b := ir.FromBool(true)
		b.Tag = tag
		b.Parent = p
		trackPos(b, pos, opts)
		return checkLineComment(b, toks, pi, t.Pos, opts), nil
	case token.TNull:
		pos := t.Pos
		*pi++
		res := ir.Null()
		res.Tag = tag
		res.Parent = p
		trackPos(res, pos, opts)
		if len(toks) == 1 {
			return res, nil
		}
		return checkLineComment(res, toks, pi, t.Pos, opts), nil

	case token.TTag:
		// A node takes ONE tag. Tags compose with '.' -- the grammar says
		// <tag-content> ::= <single-tag> [ '.' <single-tag> ]... -- and there is
		// no form in which two of them sit side by side.
		//
		// This used to recurse with the new tag and drop the one it was called
		// with, so `!raw !let {...}` parsed as a bare !let: the escape that says
		// "this subtree is data" vanished at parse time, and what got stored was
		// an instruction. Refusing beats dropping, since a tag that is silently
		// discarded takes the meaning of the document with it.
		if tag != "" {
			return nil, fmt.Errorf("%w: two tags on one node, %s and %s %s: tags compose with '.', as %s",
				ErrParse, tag, string(t.Bytes), t.Pos,
				ir.TagCompose(tag, nil, string(t.Bytes)))
		}
		*pi++
		return parseBalanced(toks, p, string(t.Bytes), pi, opts)

	case token.TIndent:
		return nil, fmt.Errorf("%w: indent in noComments %s", errInternal, toks[*pi].Pos)
	default:
		return nil, fmt.Errorf("%w: noComments: unexpected token %q %s (%s)", ErrParse, string(t.Bytes), t.Pos, t.Type)
	}
}

func checkLineComment(node *ir.Node, toks []token.Token, pi *int, pos *token.Pos, opts *parseOpts) *ir.Node {
	// If node already has a comment (e.g., from checkMultilineStringComments), don't overwrite it
	if node.Comment != nil {
		return node
	}
	for {
		i := *pi
		if len(toks) == i {
			return node
		}
		tok := &toks[i]
		// TLineComment is the token type for line comments (after colon or value on same line)
		if tok.Type != token.TLineComment {
			return node
		}
		*pi++
		if opts.comments {
			if node.Comment == nil {
				node.Comment = &ir.Node{Parent: node, Type: ir.CommentType}
			}
			if tok.Pos.Line() > pos.Line() && len(node.Comment.Lines) == 0 {
				node.Comment.Lines = []string{""}
			}
			node.Comment.Lines = append(node.Comment.Lines, string(tok.Bytes))
		}
	}
}

func checkMultilineStringComments(node *ir.Node, toks []token.Token, pi *int, opts *parseOpts) *ir.Node {
	if !opts.comments {
		return node
	}

	numLines := len(node.Lines)
	if numLines == 0 {
		// Not a multiline string, fall back to single comment check
		return node
	}

	// Collect N TComment tokens (one per line)
	commentLines := make([]string, 0, numLines)
	i := *pi

	// Collect consecutive TLineComment tokens (skip TIndent tokens that might be between them)
	// Collect exactly numLines comments, or all consecutive comments if fewer
	for i < len(toks) && len(commentLines) < numLines {
		tok := &toks[i]
		if tok.Type == token.TLineComment {
			// Extract comment text, preserving whitespace before '#'
			commentText := string(tok.Bytes)
			commentLines = append(commentLines, commentText)
			i++
		} else if tok.Type == token.TIndent {
			// Skip TIndent tokens between comments
			i++
		} else {
			// Non-comment, non-indent token - stop collecting
			// This should not happen if tokens are correctly sequenced
			break
		}
	}

	// Pad with empty strings if we have fewer comments than lines
	for len(commentLines) < numLines {
		commentLines = append(commentLines, "")
	}

	// Only consume the tokens we actually processed
	*pi = i

	// Create Comment node with all comment lines
	node.Comment = &ir.Node{
		Parent: node,
		Type:   ir.CommentType,
		Lines:  commentLines,
	}

	return node
}

func parseObj(toks []token.Token, p *ir.Node, tag string, pi *int, opts *parseOpts) (*ir.Node, error) {
	kvs := []ir.KeyVal{}
	keyToks := []token.Token{}
	ycMap := map[int]*ir.Node{}

	for *pi < len(toks) {
		// Collect head comments for the pair that follows, keyed by its index. This used
		// to run only once len(kvs) > 0, so a comment before the first key was never
		// consumed and fell through to the default arm below, which rejects it — a
		// comment as the first thing inside { } failed to parse while the same comment
		// one line lower was fine.
		yc := comments(toks, pi, opts)
		if yc != nil {
			ycMap[len(kvs)] = yc
			continue
		}
		// comments consumes indent tokens too, so it can reach the end of the input.
		if *pi >= len(toks) {
			break
		}
		tok := &toks[*pi]
		switch tok.Type {
		case token.TRCurl:
			*pi++
			return objFromKVs(p, kvs, tag, keyToks, ycMap)
		case token.TLiteral, token.TString, token.TInteger, token.TMergeKey:
			keyToks = append(keyToks, *tok)
			var key *ir.Node
			switch tok.Type {
			case token.TString, token.TLiteral:
				key = ir.FromString(tok.String())
				trackPos(key, tok.Pos, opts)
			case token.TInteger:
				// A key cannot carry a tag, so a key written in a non-decimal
				// notation has nowhere to record it and would come back
				// silently as decimal. Integer keys are base-10 by definition
				// (see docs/tony.md, Sparse Arrays), so this is refused rather
				// than quietly rewritten.
				if _, _, radix := token.RadixLiteral(string(tok.Bytes)); radix {
					return nil, fmt.Errorf("%w: int key must be base-10, got %q %s",
						ErrParse, tok.Bytes, tok.Pos)
				}
				u64, err := strconv.ParseUint(string(tok.Bytes), 10, 64)
				if err != nil {
					return nil, fmt.Errorf("%w: bad int key (%w) %s",
						ErrParse, err, tok.Pos)
				}
				key = ir.FromInt(int64(u64))
				trackPos(key, tok.Pos, opts)
			case token.TMergeKey:
				key = ir.Null()
				trackPos(key, tok.Pos, opts)
			default:
				panic("unreachable")
			}

			*pi++
			if *pi == len(toks) {
				return nil, fmt.Errorf("%w: premature end of object %s",
					ErrParse, tok.Pos)
			}
			colTok := toks[*pi]
			if colTok.Type != token.TColon {
				return nil, fmt.Errorf("%w: unexpected %s %q %s",
					ErrParse, tok.Type, string(tok.Bytes), tok.Pos)
			}
			*pi++
			if *pi == len(toks) {
				return nil, fmt.Errorf("%w: premature end of object %s",
					ErrParse, tok.Pos)
			}
			val, err := parseBalanced(toks, p, "", pi, opts)
			if err != nil {
				return nil, err
			}
			if val == nil {
				return nil, fmt.Errorf("%w: missing value after colon %s",
					ErrParse, colTok.Pos)
			}
			// A scalar value consumes its own line comment in noComments, but an
			// object or array value returns with the comment still on the input, as
			// parseObj/parseArr stop at the closing token. Take it here, the way
			// parseArr does for its elements: "a: {} # c" used to leave the
			// TLineComment for the loop below, where the default arm ignored it
			// without advancing pi, and the parser spun forever.
			kvs = append(kvs, ir.KeyVal{Key: key, Val: checkLineComment(val, toks, pi, tok.Pos, opts)})
		case token.TTag:
			return nil, fmt.Errorf("%w %s", ErrKeyTag, tok.Pos)
		default:
			if len(kvs) == 0 {
				return nil, fmt.Errorf("%w: parseObj: unexpected token %q %s",
					ErrParse, string(tok.Bytes), tok.Pos)
			}
			// Tolerated, but consumed: leaving pi where it is means this arm is
			// reached again with the same token, forever.
			*pi++
		}
	}
	return objFromKVs(p, kvs, tag, keyToks, ycMap)
}

// applyHeadComments attaches the comments parseObj collected between pairs to the pair
// each one precedes. ycMap is keyed by the index of the following pair, so ycMap[i] is the
// head comment of kvs[i].
//
// The representation is the one parseBalanced uses for a head comment on a value and that
// associateComments already walks: a CommentType node holding the value as its only child,
// standing in the value's place. Without this the map was collected and then dropped on the
// floor — objFromKVs took ycMap as a parameter and never read it — so every comment between
// object keys was silently lost on a round trip, in brace and indented objects alike.
//
// ycMap[len(kvs)] — comments after the last pair, with no pair to head — is not attached
// here; an object has no place to hang a value with no key. Those are still dropped.
func applyHeadComments(kvs []ir.KeyVal, ycMap map[int]*ir.Node) {
	if len(ycMap) == 0 {
		return
	}
	for i := range kvs {
		yc := ycMap[i]
		if yc == nil {
			continue
		}
		kvs[i].Val = headWrap(yc, kvs[i].Val)
	}
}

// headWrap gives node the preceding comments yc holds.
//
// A comment does not wrap a comment. A value can be preceded by comments written
// at two levels -- above the key and above the first line of the block that
// follows it -- and the format attributes both to the next value to BEGIN, which
// is the same value. They are therefore ONE set of preceding comments and compose
// as lines, in writing order. Wrapping twice made a shape the IR does not have,
// and every consumer that met one kept the inner comment and dropped the outer
// (3cdjz00jh12krns4g1n0).
func headWrap(yc, node *ir.Node) *ir.Node {
	if node.Type == ir.CommentType {
		node.Lines = append(slices.Clone(yc.Lines), node.Lines...)
		return node
	}
	yc.Values = []*ir.Node{node}
	node.Parent = yc
	node.ParentIndex = 0
	return yc
}

func objFromKVs(at *ir.Node, kvs []ir.KeyVal, tag string, keyToks []token.Token, ycMap map[int]*ir.Node) (*ir.Node, error) {
	applyHeadComments(kvs, ycMap)
	var keyType *ir.Type
	for i := range kvs {
		if kvs[i].Key.Type == ir.NullType {
			tmp := ir.StringType
			keyType = &tmp
			break
		}
		keyType = &kvs[i].Key.Type
		break
	}
	if keyType == nil {
		return ir.FromKeyValsAt(at, kvs).WithTag(tag), nil
	}
	if *keyType == ir.StringType {
		for i := range kvs {
			if kvs[i].Key.Type == ir.NullType {
				continue
			}
			if kvs[i].Key.Type == ir.StringType {
				continue
			}
			return nil, fmt.Errorf("%w: mixed key types in map %s", ErrParse, keyToks[i].Pos)
		}
		return ir.FromKeyValsAt(at, kvs).WithTag(tag), nil
	}
	d := make(map[uint32]*ir.Node, len(kvs))
	for i := range kvs {
		if kvs[i].Key.Type == ir.NumberType {
			d[uint32(*kvs[i].Key.Int64)] = kvs[i].Val
			continue
		}
		return nil, fmt.Errorf("%w: mixed key types in map %s", ErrParse, keyToks[i].Pos)
	}
	// FromIntKeysMapAt will set the !sparsearray tag, so we need to compose it with the existing tag
	// rather than overwriting it
	result := ir.FromIntKeysMapAt(at, d)
	if tag != "" {
		// Compose the existing tag (e.g., !bracket) with the !sparsearray tag that FromIntKeysMapAt set
		result.Tag = ir.TagCompose(tag, nil, result.Tag)
	}
	return result, nil
}

func parseArr(toks []token.Token, p *ir.Node, tag string, pi *int, opts *parseOpts) (*ir.Node, error) {
	for *pi < len(toks) {
		tok := &toks[*pi]
		switch tok.Type {
		case token.TRSquare:
			*pi++
			goto done
		case token.TIndent:
			*pi++

		default:
			elt, err := parseBalanced(toks, p, "", pi, opts)
			if err != nil {
				return nil, err
			}
			// parseBalanced may return nil if it only consumed trailing comments
			if elt == nil {
				continue
			}
			elt.Parent = p
			elt.ParentIndex = len(p.Values)
			p.Values = append(p.Values, checkLineComment(elt, toks, pi, tok.Pos, opts))
		}
	}
done:
	p.Tag = tag
	return p, nil
}

func comments(toks []token.Token, pi *int, opts *parseOpts) *ir.Node {
	lns := []string{}
	n := len(toks)
	start := *pi
	for start < n {
		t := &toks[start]
		// Only collect TComment (head comments) here, not TLineComment.
		// TLineComment after a colon becomes a line comment on the value, handled separately.
		if t.Type == token.TComment {
			start++
			lns = append(lns, strings.TrimSpace(string(t.Bytes)))
			continue
		}
		if t.Type == token.TIndent {
			start++
			continue
		}
		break
	}
	if start == *pi {
		return nil
	}
	*pi += start - *pi
	if !opts.comments {
		return nil
	}
	if len(lns) != 0 {
		return &ir.Node{
			Type:  ir.CommentType,
			Lines: lns,
		}
	}
	return nil
}
