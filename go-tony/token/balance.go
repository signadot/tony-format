package token

import (
	"errors"
	"fmt"

	"github.com/signadot/tony-format/go-tony/format"
)

func Balance(toks []Token, f format.Format) ([]Token, error) {
	y := 0
	dst, n, err := balanceOne(nil, toks, 0, &y, false, f)
	if err != nil {
		return nil, err
	}
	for n < len(toks) {
		tok := &toks[n]
		n++
		if tok.Type.IsComment() {
			dst = append(dst, *tok)
			continue
		}
		if tok.Type != TIndent {
			return nil, fmt.Errorf("%w: trailing material %s %q %s",
				ErrDocBalance, tok.Type, string(tok.Bytes), tok.Pos)
		}
		if len(tok.Bytes) != 0 {
			return nil, fmt.Errorf("%w: trailing material %q",
				ErrDocBalance, string(tok.Bytes))
		}
	}
	for i := range dst {
		t := &dst[i]
		if t.Type != TIndent {
			continue
		}
		for j := i + 1; j < len(dst); j++ {
			n := &dst[j]
			if n.Type.IsComment() {
				dst[j-1], dst[j] = dst[j], dst[j-1]
				continue
			}
			break
		}
	}
	return dst, err
}

func balanceOne(dst []Token, toks []Token, d int, y *int, underField bool, f format.Format) ([]Token, int, error) {
	var tok *Token
	// fmt.Printf("enter balanceOne d=%d y=%d\n", d, *y)
	// PrintTokens(toks, "trace b1")
	// defer func() {
	// 	fmt.Printf("done balanceOne d=%d y=%d\n", d, *y)
	// }()

	if len(toks) == 0 {
		return nil, 0, fmt.Errorf("%w: %w", ErrDocBalance, ErrEmptyDoc)
	}
	tok = &toks[0]
	switch tok.Type {
	case TLCurl:
		return balanceBrObj(dst, toks, f)
	case TLSquare:
		return balanceBrArr(dst, toks, f)
	case TRCurl, TRSquare:
		return nil, 0, fmt.Errorf("%w: unopened %c %s", ErrDocBalance, tok.Bytes[0], tok.Pos)
	case TComma:
		return nil, 0, fmt.Errorf("%w: non separating ',' %s", ErrDocBalance, tok.Pos)
	case TString, TMString, TInteger, TLiteral, TMergeKey:
		if len(toks) == 1 {
			dst = append(dst, *tok)
			return dst, 1, nil
		}
		z := 1
		var nxt *Token
	Zoom:
		nxt = &toks[z]
		if nxt.Type == TIndent || nxt.Type.IsComment() {
			z++
			if z >= len(toks) {
				dst = append(dst, *tok)
				return dst, 1, nil
			}
			goto Zoom
		}
		if nxt.Type == TColon {
			if d == -2 {
				return nil, 0, fmt.Errorf("%w: key : in bracketed array %s",
					ErrDocBalance, tok.Pos)
			}
			if d == -1 {
				return nil, 0, fmt.Errorf("%w: unbracketed object in bracketed object %s",
					ErrDocBalance, tok.Pos)
			}
			if tok.Type == TMString {
				return nil, 0, fmt.Errorf("%w: multiline string cannot be key %s",
					ErrDocBalance, tok.Pos)
			}
			return balanceObj(dst, toks, d, y, f)
		}
		dst = append(dst, *tok)
		return dst, 1, nil

	case TArrayElt:
		if d < 0 {
			return nil, 0, fmt.Errorf("%w: array item '-' under brackets %s",
				ErrDocBalance, tok.Pos)
		}
		if underField {
			d--
		}
		return balanceArr(dst, toks, d, y, f)
	case TNull, TMLit, TFalse, TTrue, TFloat:
		// non mapkey tokens
		dst = append(dst, *tok)
		return dst, 1, nil
	case TIndent:
		if d < 0 {
			dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
			if err != nil {
				return nil, 0, err
			}
			return dst, 1 + off, nil
		}
		// checking b/c no brackets
		if len(toks) == 1 {
			if len(tok.Bytes) != 0 {
				return nil, 0, fmt.Errorf("%w: extraneous indent (end) %q %s",
					ErrDocBalance, string(tok.Bytes), tok.Pos)
			}
			// empty document should not produce error
			return dst, 0, nil
		}
		// non empty document, no brackets
		n := len(toks[0].Bytes)
		switch f {
		case format.TonyFormat:
			if underField && toks[1].Type == TArrayElt {
				if n != (d-1)*2 {
					return nil, 0, fmt.Errorf("%w: extraneous '- ' indent %s",
						ErrDocBalance, tok.Pos)
				}
			} else if n != d*2 && !toks[1].Type.IsComment() {
				// Allow transition to bracketed mode with TLCurl or TLSquare
				if toks[1].Type != TLCurl && toks[1].Type != TLSquare {
					return nil, 0, fmt.Errorf("%w: extraneous indent (no comment) %s %q %s",
						ErrDocBalance, toks[1].Type, string(toks[1].Bytes), toks[1].Pos)
				}
			}
			dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
			if err != nil {
				return nil, 0, err
			}
			return dst, 1 + off, nil
		case format.YAMLFormat:
			if toks[1].Type.IsComment() {
				dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
				if err != nil {
					return nil, 0, err
				}
				return dst, 1 + off, nil

			} else if underField {
				if n < *y {
					if toks[1].Type != TArrayElt {
						return nil, 0, fmt.Errorf("%w: expected more indentation under field: got %d/%d %s",
							ErrDocBalance, n, *y, tok.Pos)
					}
				}
				if n == *y {
					if toks[1].Type != TArrayElt {
						return nil, 0, fmt.Errorf("%w: only '- ' can serve as whitespace under a field, indent %s",
							ErrDocBalance, tok.Pos)
					}
				}
			} else if n < *y {
				*y = n
				return dst, 0, nil
			}
			//fmt.Printf("balanceOne->balanceOne indent n=%d y=%d\n", n, *y)
			*y = n
			dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
			if err != nil {
				return nil, 0, err
			}
			return dst, 1 + off, nil
		default:
			// should never happen, only Tony/YAML have non-brackets
			return dst, 1, nil
		}
	case TTag:
		dst = append(dst, *tok)
		dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
		if err != nil {
			return nil, 0, err
		}
		return dst, 1 + off, nil
	default:
		if tok.Type.IsComment() {
			dst = append(dst, *tok)
			dst, off, err := balanceOne(dst, toks[1:], d, y, underField, f)
			if err != nil {
				return nil, 0, err
			}
			return dst, 1 + off, nil
		}
		return nil, 0, fmt.Errorf("`%s` cannot start a value %s", tok.Bytes, tok.Pos)
	}
}

func balanceArr(dst, toks []Token, d int, y *int, f format.Format) ([]Token, int, error) {
	// fmt.Printf("enter balanceArr d=%d y=%d\n", d, *y)
	// PrintTokens(toks, "trace ba")
	// defer func() {
	// 	fmt.Printf("done balanceArr d=%d y=%d\n", d, *y)
	// }()
	dst = append(dst, Token{Type: TLSquare, Bytes: toks[0].Bytes, Pos: toks[0].Pos})
	N := d * 2
	if f == format.YAMLFormat {
		N = *y
	}
	nElts := 0
	i := 0
	off := 0
	eltDelta := 0
	orgY := *y
	var err error
	for i < len(toks) {
		tok := &toks[i]
		if tok.Type != TArrayElt {
			break
		}
		if nElts == 0 {
			doneY := *y
			defer func() { *y = min(*y, doneY) }()
			if f == format.YAMLFormat {
				*y += len(tok.Bytes)
				eltDelta = len(tok.Bytes)
				N = *y
			}
			orgY = *y
		}
		i++
		*y = orgY
		dst, off, err = balanceOne(dst, toks[i:], d+1, y, false, f)
		if err != nil {
			//fmt.Printf("exit balanceArr %v\n", err)
			return nil, 0, err
		}
		nElts++
		i += off
		if i == len(toks) {
			// fmt.Printf("break balanceArr len\n")
			break
		}
		nxtNC := nextNonComment(toks[i:])
		if nxtNC+i == len(toks) {
			dst = append(dst, toks[i:i+nxtNC]...)
			i += nxtNC
			// fmt.Printf("break balanceArr len nc\n")
			break
		}
		nxt := &toks[i+nxtNC]
		if nxt.Type != TIndent {
			// fmt.Printf("return balanceArr %v\n", err)
			return nil, 0, fmt.Errorf("%w: unseparated array elements %s %q %s",
				ErrDocBalance, nxt.Type, string(nxt.Bytes), nxt.Pos)
		}
		after := i + nxtNC + 1
		if after == len(toks) {
			dst = append(dst, toks[i:i+nxtNC]...)
			i += nxtNC
			// fmt.Printf("break balanceArr len after-nc\n")
			break
		}
		look := &toks[after]
		if look.Type != TArrayElt {
			dst = append(dst, toks[i:i+nxtNC]...)
			i += nxtNC
			// fmt.Printf("break balanceArr look type %s\n", look.Type)
			break
		}
		n := len(nxt.Bytes) + eltDelta
		if n < N {
			if (N-n)%2 != 0 && f != format.YAMLFormat {
				return nil, 0, fmt.Errorf("%w: invalid array indent %d %s",
					ErrDocBalance, n, nxt.Pos)
			}
			dst = append(dst, toks[i:i+nxtNC]...)
			i += nxtNC
			*y = n
			// fmt.Printf("break balanceArr %d<%d\n", n, N)
			break
		}
		if n > N {
			return nil, 0, fmt.Errorf("%w: invalid array indent %s (%d/%d)",
				ErrDocBalance, nxt.Pos, n, N)
		}
		dst = append(dst, toks[i:i+nxtNC]...)
		i += nxtNC + 1
	}
	dst = append(dst, Token{Type: TRSquare, Pos: toks[0].Pos.D.end()})
	return dst, i, nil
}

func nextNonComment(toks []Token) int {
	lastIndent := -1
	i := 0
	N := len(toks)
scan:
	for i < N {
		if toks[i].Type == TIndent {
			if lastIndent == -1 {
				lastIndent = i
			}
			i++
			goto scan
		}
		if toks[i].Type.IsComment() {
			lastIndent = -1
			i++
			goto scan
		}
		if lastIndent != -1 {
			return lastIndent
		}
		return i
	}
	if lastIndent != -1 {
		return lastIndent
	}
	return i
}

func balanceBrArr(dst, toks []Token, f format.Format) ([]Token, int, error) {
	dst = append(dst, Token{Type: TLSquare, Bytes: toks[0].Bytes, Pos: toks[0].Pos})
	var (
		i, off int
		err    error
		found  bool
		start  = &toks[0]
		tok    *Token
	)
	i = 1
Elts:
	for i < len(toks) && !found {
		tok = &toks[i]
		switch tok.Type {
		case TRSquare:
			i++
			found = true
			goto done
		case TIndent:
			i++
			continue
		default:
			if tok.Type.IsComment() {
				dst = append(dst, *tok)
				i++
				continue
			}
			dst, off, err = balanceOne(dst, toks[i:], -2, nil, false, f)
			if errors.Is(err, ErrEmptyDoc) || i+off == len(toks) {
				return nil, 0, fmt.Errorf("%w: '[' not closed %s",
					ErrDocBalance, start.Pos)
			}
			if err != nil {
				return nil, 0, err
			}
		}
	Comma:
		if i+off == len(toks) {
			return nil, 0, fmt.Errorf("%w: '[' not closed %s",
				ErrDocBalance, start.Pos)
		}
		for {
			nxt := &toks[i+off]
			switch nxt.Type {
			case TComma:
				i += off + 1
				continue Elts
			case TRSquare:
				i += off + 1
				found = true
				goto done
			case TTag:
				dst = append(dst, *nxt)
				off++
				goto Comma
			case TIndent:
				off++
				goto Comma
			default:
				if nxt.Type.IsComment() {
					dst = append(dst, *nxt)
					off++
					goto Comma
				}
				i += off
				continue Elts
			}
		}
	}
done:
	if !found {
		return nil, 0, fmt.Errorf("%w: '[' not closed %s",
			ErrDocBalance, start.Pos)
	}
	dst = append(dst, Token{Type: TRSquare, Pos: toks[0].Pos.D.end()})
	return dst, i, nil
}

func balanceObj(dst, toks []Token, d int, y *int, f format.Format) ([]Token, int, error) {
	// fmt.Printf("enter balanceObj d=%d y=%d\n", d, *y)
	// PrintTokens(toks, "trace bo")
	// defer func() {
	// 	fmt.Printf("done balanceObj d=%d y=%d\n", d, *y)
	// }()
	dst = append(dst, Token{Type: TLCurl, Pos: toks[0].Pos})
	var (
		i, off int
		N      = d * 2
		err    error
	)
	if f == format.YAMLFormat {
		N = *y
	}
	i = 0
	keyI := -1
	colonI := -1
	found := false

KVLoop:
	for i < len(toks) && !found {
		tok := &toks[i]
		// fmt.Printf("\tbalanceObj on %s keyI=%d :-i = %d\n", tok.Type, keyI, colonI)
		if keyI == -1 {
			switch tok.Type {
			case TLiteral, TString, TInteger, TMergeKey:
				dst = append(dst, *tok)
				keyI = i
				i++
				continue KVLoop
			case TTag:
				dst = append(dst, *tok)
				i++
				continue KVLoop
			case TIndent:
				if i+1 != len(toks) {
					nxt := &toks[i+1]
					if nxt.Type.IsComment() || nxt.Type == TIndent {
						i++
						continue KVLoop
					}
				}
				n := len(tok.Bytes)
				if n == N {
					i++
					continue KVLoop
				}
				if n > N {
					return nil, 0, fmt.Errorf("%w: key indented too much (%d/%d) %s",
						ErrDocBalance, n, N, tok.Pos)
				}
				if n < N {
					if (N-n)%2 != 0 && f != format.YAMLFormat {
						return nil, 0, fmt.Errorf("%w: invalid field indent %d %s",
							ErrDocBalance, n, tok.Pos)
					}
					found = true
					*y = n
					//i++
					continue KVLoop
				}
			default:
				if tok.Type.IsComment() {
					dst = append(dst, *tok)
					i++
					continue KVLoop
				}
				return nil, 0, fmt.Errorf("%w: %q is not a key %s %s N=%d",
					ErrDocBalance, string(tok.Bytes), tok.Type, tok.Pos, N)
			}
		}
		// we have a key
		if colonI == -1 {
			if tok.Type == TColon {
				dst = append(dst, *tok)
				colonI = i
				i++
				continue KVLoop
			}
			if tok.Type.IsComment() {
				// can be TString with following comment
				// from mString tokenizer
				i++
				continue KVLoop
			}
			return nil, 0, fmt.Errorf("%w: key not followed by : got %s %q %s",
				ErrDocBalance, tok.Type, string(tok.Bytes), tok.Pos)
		}
		tmp := *y
		dst, off, err = balanceOne(dst, toks[i:], d+1, y, true, f)
		if err != nil {
			return nil, 0, err
		}
		i += off
		keyI = -1
		colonI = -1
		if *y < tmp {
			found = true
		}
	}
	if !found && i < len(toks) {
		return nil, 0, fmt.Errorf("%w: object not terminated %s",
			ErrDocBalance, toks[0].Pos)
	}
	dst = append(dst, Token{Type: TRCurl, Pos: toks[0].Pos})
	return dst, i, nil
}

func balanceBrObj(dst, toks []Token, f format.Format) ([]Token, int, error) {
	dst = append(dst, Token{Type: TLCurl, Bytes: toks[0].Bytes, Pos: toks[0].Pos})
	var (
		i, off int
		err    error
	)
	i = 1
	keyI := -1
	colonI := -1
	found := false
	nKVs := 0
KVLoop:
	for i < len(toks) && !found {
		tok := &toks[i]
		if keyI == -1 {
			switch tok.Type {
			case TLiteral, TString, TInteger, TMergeKey:
				dst = append(dst, *tok)
				keyI = i
				i++
				continue KVLoop
			case TTag:
				dst = append(dst, *tok)
				i++
				continue KVLoop
			case TRCurl:
				found = true
				i++
				continue KVLoop
			case TIndent:
				i++
				continue KVLoop
			case TComma:
				if nKVs == 0 {
					return nil, 0, fmt.Errorf("%w: %q is not a key %s %s",
						ErrDocBalance, string(tok.Bytes), tok.Type, tok.Pos)
				}
				i++
				continue KVLoop

			default:
				if tok.Type.IsComment() {
					dst = append(dst, *tok)
					i++
					continue KVLoop
				}
				return nil, 0, fmt.Errorf("%w: %q is not a key %s %s",
					ErrDocBalance, string(tok.Bytes), tok.Type, tok.Pos)
			}
		}
		// we have a key
		if colonI == -1 {
			if tok.Type.IsComment() {
				// can be TString with following comment
				// from mString tokenizer
				i++
				continue KVLoop
			}
			if tok.Type == TColon {
				dst = append(dst, *tok)
				colonI = i
				i++
				continue KVLoop
			}
			if f.IsTony() {
				// implicit : null
				if tok.Type == TTag {
					dst = append(dst, Token{Type: TColon, Pos: tok.Pos},
						*tok, Token{Type: TNull, Pos: tok.Pos})
					i++
				} else {
					dst = append(dst, Token{Type: TColon, Pos: tok.Pos},
						Token{Type: TNull, Pos: tok.Pos})
				}
				keyI = -1
				colonI = -1
				if tok.Type == TRCurl {
					found = true
					i++
				}
				continue KVLoop
			}
			return nil, 0, fmt.Errorf("%w: key not followed by : got %q %s",
				ErrDocBalance, string(tok.Bytes), tok.Pos)
		}
		dst, off, err = balanceOne(dst, toks[i:], -1, nil, false, f)
		if err != nil {
			return nil, 0, err
		}
		i += off
		keyI = -1
		colonI = -1
		nKVs++
	}
	if !found {
		return nil, 0, fmt.Errorf("%w: '{' not closed %s",
			ErrDocBalance, toks[0].Pos)
	}
	dst = append(dst, Token{Type: TRCurl, Pos: toks[0].Pos})
	return dst, i, nil
}
