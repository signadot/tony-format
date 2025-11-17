package token

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/signadot/tony-format/tony/format"
)

type tkState struct {
	cb, sb int
	// reset every '\n'
	lnIndent int
	kvSep    bool
	bElt     int
}

func Tokenize(dst []Token, src []byte, opts ...TokenOpt) ([]Token, error) {
	var (
		i, n int
		c    byte
		d    []byte
	)
	posDoc := &PosDoc{d: make([]byte, len(src), len(src)+1)}
	copy(posDoc.d, src)
	posDoc.d = append(posDoc.d, '\n')
	d = posDoc.d
	n = len(d)
	opt := &tokenOpts{format: format.TonyFormat}
	for _, o := range opts {
		o(opt)
	}
	ts := &tkState{}
	indent := readIndent(d)
	//fmt.Printf("at offset %d indent %d\n", i, indent)
	if indent != 0 {
		tok := &Token{
			Type:  TIndent,
			Bytes: bytes.Repeat([]byte{' '}, indent),
			Pos:   posDoc.Pos(i),
		}
		dst = append(dst, *tok)
		ts.lnIndent = indent
		ts.kvSep = false
		ts.bElt = 0
	}

	for i < n {
		c = d[i]
		// fmt.Printf("tokenize on %q at %d\n", string(c), i)
		//if len(dst) > 0 {
		//	fmt.Printf("\tlast: %s %s\n", dst[len(dst)-1].Info(), dst[len(dst)-1].String())
		//}
		if c == '\n' {
			posDoc.nl(i)
			i++
			if i < n && d[i] == '-' && i < n-1 && d[i+1] == '-' && i < n-2 && d[i+2] == '-' {
				dst = append(dst, Token{
					Type:  TDocSep,
					Pos:   posDoc.Pos(i),
					Bytes: d[i : i+4],
				})
				i += 4
				continue
			}
			if i < n && d[i] == '\n' {
				continue
			}
			indent := readIndent(d[i:])
			//fmt.Printf("at offset %d indent %d\n", i, indent)
			tok := &Token{
				Type:  TIndent,
				Bytes: bytes.Repeat([]byte{' '}, indent),
				Pos:   posDoc.Pos(i),
			}
			dst = append(dst, *tok)
			ts.lnIndent = indent
			ts.kvSep = false
			ts.bElt = 0
			continue
		}

		switch c {
		case ':':
			if opt.format == format.YAMLFormat &&
				i+1 < len(d) &&
				d[i+1] != ' ' &&
				d[i+1] != '\t' &&
				d[i+1] != '\r' &&
				d[i+1] != '\n' {
				off, err := yamlPlain(d[i+1:], ts, i+1, posDoc)
				if err != nil {
					return nil, err
				}
				dst = append(dst, *yamlPlainToken(d[i:i+off+1], posDoc.Pos(i)))
				i += off + 1
				continue
			}
			dst = append(dst, Token{
				Type:  TColon,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			ts.kvSep = true
			i++
		case '"', '\'':
			if opt.format == format.YAMLFormat {
				tok, off, err := YAMLQuotedString(d[i:], posDoc.Pos(i))
				if err != nil {
					return nil, NewTokenizeErr(err, posDoc.Pos(i))
				}
				tok.Pos = posDoc.Pos(i)
				i += off
				dst = append(dst, *tok)
				continue
			}
			if opt.format == format.TonyFormat {
				n := -1
				if len(dst) == 0 {
					n = 0
				} else {
					tok := dst[len(dst)-1]
					switch tok.Type {
					case TIndent:
						n = len(tok.Bytes)
					}
				}
				if n != -1 {
					// multiline enabled string
					toks, off, err := mString(d[i:], i, n, posDoc)
					if err != nil {
						return nil, err
					}
					i += off
					dst = append(dst, toks...)
					continue
				}
			}

			j, err := bsEscQuoted(d[i:])
			if err != nil {
				return nil, NewTokenizeErr(err, posDoc.Pos(i))
			}
			dst = append(dst, Token{
				Type:  TString,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+j],
			})
			i += j
		case '!':
			if opt.format == format.JSONFormat {
				return nil, UnexpectedErr("!", posDoc.Pos(i))
			}
			start := i + 1
			for start < n {
				r, sz := utf8.DecodeRune(d[start:])
				if r == utf8.RuneError {
					return nil, UnexpectedErr("bad utf8", posDoc.Pos(start))
				}
				if unicode.IsSpace(r) {
					break
				}
				if unicode.Is(unicode.Other, r) {
					return nil, UnexpectedErr("unicode other", posDoc.Pos(start))
				}
				start += sz
			}

			if i+1 == start {
				return nil, UnexpectedErr("end", posDoc.Pos(start))
			}

			dst = append(dst, Token{
				Type:  TTag,
				Pos:   posDoc.Pos(i),
				Bytes: d[i:start],
			})
			i = start
		case '|':
			if opt.format == format.JSONFormat {
				return nil, UnexpectedErr("|", posDoc.Pos(i))
			}
			// TODO yaml leading whitespace on 1st line stuff
			mIndent, err := mLitIndent(dst, 0)
			if err != nil {
				return nil, NewTokenizeErr(err, posDoc.Pos(i))
			}
			sz, err := mLit(d[i:], mIndent, posDoc, i)
			if err != nil {
				return nil, err
			}
			idBytes := make([]byte, 0, sz+1)
			idBytes = binary.AppendUvarint(idBytes, uint64(mIndent))
			dst = append(dst, Token{
				Type:  TMLit,
				Bytes: append(idBytes, d[i:i+sz]...),
				Pos:   posDoc.Pos(i),
			})
			i += sz
			if sz > 0 {
				i--
			}
		case '>':
			if opt.format != format.YAMLFormat {
				return nil, UnexpectedErr(">", posDoc.Pos(i))
			}
			// TODO yaml folding block style?
			return nil, NewTokenizeErr(ErrUnsupported, posDoc.Pos(i))
		case '-':
			if i == n-1 {
				return nil, UnexpectedErr("end", posDoc.Pos(i))
			}
			if i == 0 && n >= 3 && d[1] == '-' && d[2] == '-' {
				if opt.format == format.JSONFormat {
					return nil, UnexpectedErr("-", posDoc.Pos(i))
				}
				dst = append(dst, Token{
					Type:  TDocSep,
					Pos:   posDoc.Pos(0),
					Bytes: d[0:3],
				})
				i += 3
				continue
			}

			next := d[i+1]
			// fmt.Printf("next is %q format %s\n", string(next), opt.format)
			switch next {
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
				if opt.format == format.YAMLFormat {
					off, err := yamlPlain(d[i+1:], ts, i+1, posDoc)
					if err != nil {
						return nil, err
					}
					n, isFloat, err := number(d[i+1 : i+1+off])
					if err == nil && n == off {
						tok := &Token{
							Type:  TInteger,
							Pos:   posDoc.Pos(i),
							Bytes: d[i : i+n+1],
						}
						if isFloat {
							tok.Type = TFloat
						}
						dst = append(dst, *tok)
					} else {
						dst = append(dst, *yamlPlainToken(d[i:i+off+1], posDoc.Pos(i)))
					}
					i += off + 1
					continue
				}
				n, isFloat, err := number(d[i+1:])
				if err != nil {
					return nil, NewTokenizeErr(err, posDoc.Pos(i))
				}
				tok := &Token{
					Type:  TInteger,
					Pos:   posDoc.Pos(i),
					Bytes: d[i : i+n+1],
				}
				if isFloat {
					tok.Type = TFloat
				}
				dst = append(dst, *tok)
				i += n + 1
			case ' ', '\n', '\t':
				if opt.format == format.JSONFormat {
					return nil, UnexpectedErr("- ", posDoc.Pos(i))
				}
				dst = append(dst, Token{
					Type:  TArrayElt,
					Bytes: d[i : i+2],
					Pos:   posDoc.Pos(i),
				})
				i++
				if next != '\n' {
					// trigger indent token
					i++
				}
				ts.bElt++
				if opt.format == format.YAMLFormat {
					j := i + 2
					for j < len(d) {
						if d[j] == ' ' {
							ts.lnIndent++
							j++
							continue
						}
						break
					}
				}
				continue
			default:
				switch opt.format {
				case format.JSONFormat:
					return nil, UnexpectedErr("n...", posDoc.Pos(i))
				case format.TonyFormat:
					lit, err := getSingleLiteral(d[i:])
					if err != nil {
						return nil, err
					}
					dst = append(dst, Token{
						Type:  TLiteral,
						Pos:   posDoc.Pos(i),
						Bytes: lit,
					})
					i += len(lit)
				case format.YAMLFormat:
					off, err := yamlPlain(d[i:], ts, i, posDoc)
					if err != nil {
						return nil, err
					}
					dst = append(dst, *yamlPlainToken(d[i:i+off], posDoc.Pos(i)))
					i += off
				}
				continue
			}
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			if opt.format == format.YAMLFormat {
				off, err := yamlPlain(d[i:], ts, i, posDoc)
				if err != nil {
					return nil, err
				}
				n, isFloat, err := number(d[i : i+off])
				if err == nil && n == off {
					tok := &Token{
						Type:  TInteger,
						Pos:   posDoc.Pos(i),
						Bytes: d[i : i+n],
					}
					if isFloat {
						tok.Type = TFloat
					}
					dst = append(dst, *tok)
				} else {
					dst = append(dst, *yamlPlainToken(d[i:i+off], posDoc.Pos(i)))
				}
				i += off
				continue
			}
			n, isFloat, err := number(d[i:])
			if err != nil {
				return nil, NewTokenizeErr(err, posDoc.Pos(i))
			}
			tok := &Token{
				Type:  TInteger,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+n],
			}
			if isFloat {
				tok.Type = TFloat
			}
			dst = append(dst, *tok)
			i += n

		case '#':
			if opt.format == format.JSONFormat {
				return nil, UnexpectedErr("#", posDoc.Pos(i))
			}
			// nb yaml preceding ' ' is handled in yamlPlain
			preLen := commentPrefix(d[:i], ts.lnIndent)
			end := i
			for end < n {
				r, sz := utf8.DecodeRune(d[end:])
				if r == utf8.RuneError {
					return nil, UnexpectedErr("bad utf8", posDoc.Pos(end))
				}
				if r != '\n' {
					end += sz
					continue
				}
				dst = append(dst, Token{
					Type:  TComment,
					Pos:   posDoc.Pos(end),
					Bytes: d[i-preLen : end],
				})
				break
			}
			i = end
		case ' ', '\t', '\r', '\v', '\f':
			i++
		case 'n':
			if isKeyWordPrefix(d[i:], []byte("null")) {
				dst = append(dst, Token{
					Type:  TNull,
					Bytes: d[i : i+4],
					Pos:   posDoc.Pos(i),
				})
				i += 4
				continue
			}
			switch opt.format {
			case format.JSONFormat:
				return nil, UnexpectedErr("n...", posDoc.Pos(i))
			case format.TonyFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, err
				}
				dst = append(dst, Token{
					Type:  TLiteral,
					Pos:   posDoc.Pos(i),
					Bytes: lit,
				})
				i += len(lit)
			case format.YAMLFormat:
				off, err := yamlPlain(d[i:], ts, i, posDoc)
				if err != nil {
					return nil, err
				}
				dst = append(dst, *yamlPlainToken(d[i:i+off], posDoc.Pos(i)))
				i += off
			}

		case 't':
			if isKeyWordPrefix(d[i:], []byte("true")) {
				dst = append(dst, Token{
					Type:  TTrue,
					Bytes: d[i : i+4],
					Pos:   posDoc.Pos(i),
				})
				i += 4
				continue
			}
			switch opt.format {
			case format.JSONFormat:
				return nil, UnexpectedErr("n...", posDoc.Pos(i))
			case format.TonyFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, err
				}
				dst = append(dst, Token{
					Type:  TLiteral,
					Pos:   posDoc.Pos(i),
					Bytes: lit,
				})
				i += len(lit)
			case format.YAMLFormat:
				off, err := yamlPlain(d[i:], ts, i, posDoc)
				if err != nil {
					return nil, err
				}
				dst = append(dst, *yamlPlainToken(d[i:i+off], posDoc.Pos(i)))
				i += off
			}
		case 'f':
			if isKeyWordPrefix(d[i:], []byte("false")) {
				dst = append(dst, Token{
					Type:  TFalse,
					Bytes: d[i : i+5],
					Pos:   posDoc.Pos(i),
				})
				i += 5
				continue
			}
			switch opt.format {
			case format.JSONFormat:
				return nil, UnexpectedErr("n...", posDoc.Pos(i))
			case format.TonyFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, err
				}
				dst = append(dst, Token{
					Type:  TLiteral,
					Pos:   posDoc.Pos(i),
					Bytes: lit,
				})
				i += len(lit)
			case format.YAMLFormat:
				off, err := yamlPlain(d[i:], ts, i, posDoc)
				if err != nil {
					return nil, err
				}
				dst = append(dst, *yamlPlainToken(d[i:i+off], posDoc.Pos(i)))
				i += off
			}
			if opt.format == format.JSONFormat {
				return nil, UnexpectedErr("f...", posDoc.Pos(i))
			}
		case '<':
			if opt.format == format.JSONFormat {
				return nil, UnexpectedErr("<", posDoc.Pos(i))
			}
			if i+1 == len(d) {
				return nil, NewTokenizeErr(ErrUnterminated, posDoc.Pos(i))
			}
			if d[i+1] != '<' {
				return nil, NewTokenizeErr(ErrUnterminated, posDoc.Pos(i))
			}
			dst = append(dst, Token{
				Type:  TMergeKey,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+2],
			})
			i += 2
		case '{':
			ts.cb++
			dst = append(dst, Token{
				Type:  TLCurl,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			i++
		case '}':
			ts.cb--
			dst = append(dst, Token{
				Type:  TRCurl,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			i++
		case '[':
			ts.sb++
			dst = append(dst, Token{
				Type:  TLSquare,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			i++
		case ']':
			ts.sb--
			dst = append(dst, Token{
				Type:  TRSquare,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			i++
		case ',':
			dst = append(dst, Token{
				Type:  TComma,
				Pos:   posDoc.Pos(i),
				Bytes: d[i : i+1],
			})
			i++
		default:
			switch opt.format {
			case format.TonyFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, NewTokenizeErr(ErrLiteral, posDoc.Pos(i))
				}
				dst = append(dst, Token{
					Type:  TLiteral,
					Pos:   posDoc.Pos(i),
					Bytes: lit,
				})
				i += len(lit)
			case format.JSONFormat:
				lit, err := getSingleLiteral(d[i:])
				if err != nil {
					return nil, NewTokenizeErr(ErrLiteral, posDoc.Pos(i))
				}
				return nil, UnexpectedErr(string(lit), posDoc.Pos(i))
			case format.YAMLFormat:
				off, err := yamlPlain(d[i:], ts, i, posDoc)
				if err != nil {
					return nil, err
				}
				ypt := yamlPlainToken(d[i:i+off], posDoc.Pos(i))
				dst = append(dst, *ypt)
				i += off
			default:
				return nil, NewTokenizeErr(fmt.Errorf("%w format %q", ErrUnsupported, opt.format.String()), posDoc.Pos(i))
			}
		}

	}
	return dst, nil
}
