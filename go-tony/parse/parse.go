// Package parse provides tony parsing support.
package parse

import (
	"bytes"
	"fmt"
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

func trackPos(node *ir.Node, pos *token.Pos, opts *parseOpts) {
	if opts.positions != nil && pos != nil {
		opts.positions[node] = pos
	}
}

func parseBalanced(toks []token.Token, p *ir.Node, tag string, pi *int, opts *parseOpts) (*ir.Node, error) {
	// Skip leading comments if next token is TMString (multiline string comments are handled by checkMultilineStringComments)
	var yComments *ir.Node
	if *pi < len(toks) && toks[*pi].Type != token.TMString {
		yComments = comments(toks, pi, opts)
	}
	child, err := noComments(toks, p, tag, pi, opts)
	if err != nil {
		return nil, err
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
			child.Comment.Lines = append(child.Comment.Lines, trComments.Lines...)
		}
	}
	if !opts.comments || yComments == nil {
		return child, nil
	}
	if child == nil {
		return yComments, nil
	}
	yComments.Values = []*ir.Node{child}
	child.Parent = yComments
	child.ParentIndex = 0
	return yComments, nil
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
			tag = ir.TagCompose("!bracket", nil, tag)
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
			tag = ir.TagCompose("!bracket", nil, tag)
		}
		trackPos(arrY, pos, opts)
		return parseArr(toks, arrY, tag, pi, opts)
	case token.TLiteral, token.TString, token.TMString:
		*pi++
		sy := ir.FromString(t.String())
		sy.Parent = p
		sy.Tag = tag
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

	case token.TInteger:
		pos := t.Pos
		i, err := strconv.ParseInt(string(t.Bytes), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %w: %s", err, t.Pos)
		}
		*pi++
		iy := ir.FromInt(i)
		iy.Tag = tag
		iy.Parent = p
		trackPos(iy, pos, opts)
		if len(toks) == *pi {
			return iy, nil
		}
		return checkLineComment(iy, toks, pi, t.Pos, opts), nil

	case token.TFloat:
		pos := t.Pos
		*pi++
		f, err := strconv.ParseFloat(string(t.Bytes), 64)
		if err != nil {
			return nil, fmt.Errorf("invalid err %w: %s", err, t.Pos)
		}
		fy := ir.FromFloat(f)
		fy.Tag = tag
		fy.Parent = p
		trackPos(fy, pos, opts)
		return checkLineComment(fy, toks, pi, t.Pos, opts), nil
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
		if tok.Type != token.TComment {
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

	// Collect consecutive TComment tokens (skip TIndent tokens that might be between them)
	// Collect exactly numLines comments, or all consecutive comments if fewer
	for i < len(toks) && len(commentLines) < numLines {
		tok := &toks[i]
		if tok.Type == token.TComment {
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
		if len(kvs) > 0 {
			yc := comments(toks, pi, opts)
			if yc != nil {
				ycMap[len(kvs)] = yc
				continue
			}
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
			kvs = append(kvs, ir.KeyVal{Key: key, Val: val})
		case token.TTag:
			return nil, fmt.Errorf("%w %s", ErrKeyTag, tok.Pos)
		default:
			if len(kvs) == 0 {
				return nil, fmt.Errorf("%w: parseObj: unexpected token %q %s",
					ErrParse, string(tok.Bytes), tok.Pos)
			}
		}
	}
	return objFromKVs(p, kvs, tag, keyToks, ycMap)
}

func objFromKVs(at *ir.Node, kvs []ir.KeyVal, tag string, keyToks []token.Token, ycMap map[int]*ir.Node) (*ir.Node, error) {
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
		result := ir.FromKeyValsAt(at, kvs).WithTag(tag)
		if result != nil && len(result.Values) > 0 && result.Values[0].Comment != nil {
		}
		return result, nil
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
		result := ir.FromKeyValsAt(at, kvs).WithTag(tag)
		if result != nil && len(result.Values) > 0 && result.Values[0].Comment != nil {
		}
		return result, nil
	}
	d := make(map[uint32]*ir.Node, len(kvs))
	for i := range kvs {
		if kvs[i].Key.Type == ir.NumberType {
			d[uint32(*kvs[i].Key.Int64)] = kvs[i].Val
			continue
		}
		return nil, fmt.Errorf("%w: mixed key types in map %s", ErrParse, keyToks[i].Pos)
	}
	at.Tag = tag
	return ir.FromIntKeysMapAt(at, d).WithTag(tag), nil
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
