package token

import (
	"errors"
	"fmt"
)

var (
	ErrBadUTF8           = errors.New("bad utf8")
	ErrUnterminated      = errors.New("unterminated")
	ErrNumberLeadingZero = errors.New("leading zero")
	ErrNoIndent          = errors.New("indentation needed")
	ErrDocBalance        = errors.New("imbalanced document")
	ErrLiteral           = errors.New("bad literal")
	ErrBadEscape         = errors.New("bad escape")
	ErrBadUnicode        = errors.New("bad unicode")
	ErrUnicodeControl    = errors.New("unicode control")
	ErrMalformedMLit     = errors.New("malformed multiline literal")
	ErrColonSpace        = errors.New("colon should be followed by space")
	ErrEmptyDoc          = errors.New("empty document")
	ErrMultilineString   = errors.New("multiline string")
	ErrYAMLDoubleQuote   = errors.New("yaml double quote")
	ErrMLitPlacement     = errors.New("bad placement of |")
	ErrYAMLPlain         = errors.New("yaml plain string")
	ErrUnsupported       = errors.New("unsupported")
	ErrNumber            = errors.New("number")
)

func LeadingZeroErr(pos *Pos) error {
	return NewTokenizeErr(ErrNumberLeadingZero, pos)
}

type ErrImbalancedStructure struct {
	Open, Close *Token
}

func (i *ErrImbalancedStructure) Unwrap() error {
	return ErrDocBalance
}

func (i *ErrImbalancedStructure) Error() string {
	if i.Open == nil {
		return ErrDocBalance.Error() + ": " + UnexpectedErr(string(i.Close.Bytes), i.Close.Pos).Error()
	}
	if i.Close == nil {
		return ErrDocBalance.Error() + ": " + fmt.Sprintf("unmatched %s at %s", string(i.Open.Bytes),
			i.Open.Pos.String())
	}
	return fmt.Sprintf("%s: %s at %s closed by %s at %s",
		ErrDocBalance.Error(),
		string(i.Open.Bytes), i.Open.Pos.String(),
		string(i.Close.Bytes), i.Close.Pos.String())
}
