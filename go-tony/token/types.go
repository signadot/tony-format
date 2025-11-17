package token

import (
	"bytes"
	"fmt"
	"strings"
)

type TokenType int

const (
	TIndent = iota
	TInteger
	TFloat
	TColon
	TArrayElt
	TDocSep
	TComment
	TNull
	TTrue
	TFalse
	TTag
	TString
	TMString
	TLiteral
	TMLit
	TMergeKey
	TLCurl
	TRCurl
	TLSquare
	TRSquare
	TComma
)

func (t TokenType) String() string {
	return map[TokenType]string{
		TInteger:  "TInteger",
		TFloat:    "TFloat",
		TColon:    "TColon",
		TArrayElt: "TArrayElt",
		TDocSep:   "TDocSep",
		TMLit:     "TMLit",
		TComment:  "TComment",
		TNull:     "TNull",
		TTrue:     "TTrue",
		TFalse:    "TFalse",
		TMergeKey: "TMergeKey",
		TTag:      "TTag",
		TString:   "TString",
		TMString:  "TMString",
		TLiteral:  "TLiteral",
		TIndent:   "TIndent",
		TLCurl:    "TLCurl",
		TRCurl:    "TRCurl",
		TLSquare:  "TLSquare",
		TRSquare:  "TRSquare",
		TComma:    "TComma",
	}[t]
}

type Token struct {
	Type  TokenType
	Pos   *Pos
	Bytes []byte
}

func (t *Token) Info() string {
	return fmt.Sprintf("%s %s", t.Type, t.Pos.String())
}

func (t *Token) String() string {
	switch t.Type {
	case TMLit:
		return mLitToString(t.Bytes)
	case TString:
		return QuotedToString(t.Bytes)
	case TMString:
		parts := bytes.Split(t.Bytes, []byte{'\n'})
		res := make([]string, len(parts))
		for i, part := range parts {
			s := QuotedToString(part)
			res[i] = s
		}
		return strings.Join(res, "")
	default:
		return string(t.Bytes)
	}
}

type TokenizeErr struct {
	Err error
	Pos Pos
}

func (t *TokenizeErr) Unwrap() error {
	return t.Err
}

func NewTokenizeErr(e error, p *Pos) *TokenizeErr {
	return &TokenizeErr{Err: e, Pos: *p}
}

func (e *TokenizeErr) Error() string {
	return fmt.Sprintf("%s at %s", e.Err.Error(), e.Pos.String())
}

func ExpectedErr(what string, p *Pos) error {
	return NewTokenizeErr(fmt.Errorf("expected %s", what), p)
}
func UnexpectedErr(what string, p *Pos) error {
	return NewTokenizeErr(fmt.Errorf("unexpected %s", what), p)
}
