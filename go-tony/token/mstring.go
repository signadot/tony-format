package token

import (
	"fmt"
)

func mString(d []byte, start, indent int, posDoc *PosDoc) ([]Token, int, error) {
	i := 0
	n := len(d)
	res := []Token{}
	for i < n {
		toks, off, err := mStringOne(d[i:], start+i, indent, posDoc)
		if err != nil {
			return nil, 0, NewTokenizeErr(ErrMultilineString, posDoc.Pos(i))
		}
		res = append(res, toks...)
		i += off
		// Check for comment on the same line as the string we just processed
		commentOnSameLine, commentOff := checkCommentOnSameLine(d[i:], start+i, posDoc)
		res = append(res, *commentOnSameLine)
		i += commentOff
		// Check for next string
		x, comments := nextMString(d[i:], indent, start+i, posDoc)
		res = append(res, comments...)
		if x == -1 {
			break
		}
		i += x
	}
	return msMergeToks(res), i, nil
}

// checkCommentOnSameLine checks if there's a comment immediately after the current position
// (on the same line as the string we just processed). Returns the comment token and offset if found.
func checkCommentOnSameLine(d []byte, start int, posDoc *PosDoc) (*Token, int) {
	whitespaceStart := -1
	commentStart := -1
	inComment := false

	for i, c := range d {
		switch c {
		case '\n':
			// End of line - if we were in a comment, return it
			if inComment && commentStart != -1 {
				commentBytes := d[commentStart:i] // Exclude newline
				posDoc.nl(start + i)
				return &Token{
					Type:  TComment,
					Pos:   posDoc.Pos(start + commentStart),
					Bytes: commentBytes,
				}, i + 1 // Include newline in offset
			}
			// No comment found on this line
			return &Token{Type: TComment, Pos: posDoc.Pos(start)}, 0
		case '#':
			if !inComment {
				if whitespaceStart == -1 {
					whitespaceStart = i
				}
				commentStart = whitespaceStart
				inComment = true
			}
		case ' ':
			if !inComment && commentStart == -1 {
				if whitespaceStart == -1 {
					whitespaceStart = i
				}
			}
		default:
			if inComment {
				// Continue reading comment until newline
				continue
			}
			// Non-whitespace, non-comment character means no comment on this line
			return &Token{
				Type: TComment,
				Pos:  posDoc.Pos(start),
			}, 0
		}
	}
	// End of input, check if we found a comment
	if inComment && commentStart != -1 {
		commentBytes := d[commentStart:]
		return &Token{
			Type:  TComment,
			Pos:   posDoc.Pos(start + commentStart),
			Bytes: commentBytes,
		}, len(d)
	}
	return &Token{Type: TComment, Pos: posDoc.Pos(start)}, 0
}

func nextMString(d []byte, desOff, start int, posDoc *PosDoc) (int, []Token) {
	indents := 0
	inComment := false

	for i, c := range d {
		switch c {
		case '\n':
			// nl() now handles bounds checking internally, safe to call in both contexts
			posDoc.nl(start + i)
			indents = 0
			inComment = false
		case '#':
			// Start of comment - skip until newline
			inComment = true
		case ' ':
			if !inComment {
				indents++
			}
		case '"', '\'':
			if !inComment && indents == desOff {
				// Found next string - comments are handled by checkCommentOnSameLine
				return i, []Token{}
			}
		default:
			if inComment {
				// Continue skipping comment until newline
				continue
			}
			// Non-whitespace, non-quote, non-comment character before finding next string at correct indent
			return -1, nil
		}
	}
	return -1, nil
}

func msMergeToks(toks []Token) []Token {
	comments := []Token{}
	strTok := &Token{Type: TString, Pos: toks[0].Pos}
	stringCount := 0
	for i := range toks {
		tok := &toks[i]
		switch tok.Type {
		case TString:
			stringCount++
			if len(strTok.Bytes) != 0 {
				strTok.Bytes = append(strTok.Bytes, '\n')
				strTok.Type = TMString
			}
			strTok.Bytes = append(strTok.Bytes, tok.Bytes...)
		case TComment:
			comments = append(comments, *tok)
		}
	}
	// Ensure we have exactly N TComment tokens (one per string line)
	// If we have fewer, pad with empty comment tokens
	// Use the last comment's position or the string token's position as fallback
	var lastPos *Pos
	if len(comments) > 0 {
		lastPos = comments[len(comments)-1].Pos
	} else if strTok.Pos != nil {
		lastPos = strTok.Pos
	}
	for len(comments) < stringCount {
		comments = append(comments, Token{
			Type:  TComment,
			Pos:   lastPos,
			Bytes: []byte{},
		})
	}
	// If we have more comments than strings, something went wrong, but keep them
	return append([]Token{*strTok}, comments...)
}

func mStringOne(d []byte, start, indent int, posDoc *PosDoc) ([]Token, int, error) {
	if len(d) < 2 {
		return nil, 0, NewTokenizeErr(
			fmt.Errorf("%w quoted string", ErrUnterminated), posDoc.Pos(start))
	}
	off, err := bsEscQuoted(d)
	if err != nil {
		// Return error as-is - let caller handle streaming mode conversion
		return nil, 0, err
	}
	toks := []Token{
		Token{Type: TString, Bytes: d[:off], Pos: posDoc.Pos(start)}}
	return toks, off, nil
}
