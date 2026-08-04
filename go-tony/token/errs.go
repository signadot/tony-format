package token

import (
	"errors"
	"fmt"
)

var (
	ErrBadUTF8 = errors.New("bad utf8")
	// ErrPartialRune is a UTF-8 sequence cut off by the end of the data a
	// scanner was given, rather than invalid input. In streaming mode it means
	// "refill the buffer and retry", so the tokenizer turns it into io.EOF; if
	// it survives to the real end of a document the sequence is genuinely
	// truncated, and it reports as ErrBadUTF8, which it wraps.
	ErrPartialRune       = fmt.Errorf("%w: truncated sequence", ErrBadUTF8)
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
	// ErrDigitLeading is an unquoted scalar that begins with a digit and does not
	// finish as a number: "30s", "100m", "1Gi", "1.2.3". Without it the number
	// scanner takes the digits and leaves the rest as a separate token, and the
	// document fails later as two adjacent values — an error that names neither the
	// scalar nor the cause, and in a mapping points at the following line.
	ErrDigitLeading = errors.New("scalar starts with a digit but is not a number")
	// ErrNotQuoted is a value that is not a quoted string at all — it does not open
	// with a quote character. Distinct from ErrUnterminated, which is a quoted string
	// whose closing quote is missing or misplaced.
	ErrNotQuoted = errors.New("not a quoted string")
	// ErrTrailing is a quoted string with bytes after its closing quote, i.e. the byte
	// range handed to the decoder is wider than the string it contains.
	ErrTrailing = errors.New("trailing bytes after closing quote")
)

func LeadingZeroErr(pos *Pos) error {
	return NewTokenizeErr(ErrNumberLeadingZero, pos)
}

func DigitLeadingErr(lit []byte, pos *Pos) error {
	return NewTokenizeErr(
		fmt.Errorf("%w: %q; quote it to use it as a string", ErrDigitLeading, string(lit)), pos)
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
