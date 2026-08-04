package parse

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// numberNode reads a number token, returning the value and the notation it was written in.
//
// The notation is not part of the value: 0x1f is 31, and it compares, hashes and patches
// as 31.  It comes back separately so the caller can hang it on the node as a presentation
// tag, which is what makes the encoder able to write 0x1f again without the number itself
// having to remember anything.
func numberNode(tok *token.Token) (*ir.Node, string, error) {
	s := string(tok.Bytes)
	if tok.Type == token.TFloat {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid float %w: %s", err, tok.Pos)
		}
		return ir.FromFloat(f), floatNotation(s), nil
	}
	base, _, radix := token.RadixLiteral(s)
	if !radix {
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid integer %w: %s", err, tok.Pos)
		}
		return ir.FromInt(i), "", nil
	}
	// Base 0 auto-detects the same prefixes RadixLiteral just validated. It also
	// accepts forms Tony does not have -- a bare leading zero as octal, '_' as a
	// separator -- but RadixLiteral has already refused those, so they cannot arrive
	// here.
	i, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid integer %w: %s", err, tok.Pos)
	}
	return ir.FromInt(i), token.RadixNotation(base), nil
}

// floatNotation reports the notation tag for a float as it was written: exponent form is
// worth keeping, since 1e9 is easier to read than 1000000000 and the encoder would
// otherwise write the long form back.
func floatNotation(s string) string {
	if strings.ContainsAny(s, "eE") {
		return ir.ExpTag
	}
	return ""
}

// composeNotation puts a notation tag in front of whatever tag the value already carries.
// TagCompose builds a malformed ".mytag" when handed an empty head, so the common case of
// no notation is handled here rather than at each call site.
func composeNotation(notation, tag string) string {
	if notation == "" {
		return tag
	}
	return ir.TagCompose(notation, nil, tag)
}
