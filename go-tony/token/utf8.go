package token

import "unicode/utf8"

// partialRune reports whether d begins a UTF-8 sequence that d is too short to
// hold: the bytes present are a valid prefix, the rune itself is cut off.
//
// utf8.DecodeRune reports that case as (RuneError, 1) — indistinguishable from
// a genuinely invalid byte — so every scanner that decodes runes out of a
// partial buffer needs this to tell "refill and retry" from "reject the input".
// A scanner that gets it wrong fails the whole document whenever a multi-byte
// rune happens to straddle a buffer refill.
func partialRune(d []byte) bool {
	return len(d) > 0 && !utf8.FullRune(d)
}

// badRune classifies a utf8.DecodeRune failure at the head of d: ErrPartialRune
// when d merely ran out mid-sequence, ErrBadUTF8 when the bytes are invalid.
func badRune(d []byte) error {
	if partialRune(d) {
		return ErrPartialRune
	}
	return ErrBadUTF8
}
