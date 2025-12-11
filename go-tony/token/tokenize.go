package token

import (
	"bytes"
	"io"

	"github.com/signadot/tony-format/go-tony/format"
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

	// Main loop: use tokenizeOne() for all tokenization
	var lastToken *Token
	for i < n {
		// Call tokenizeOne with allTokens=dst and docPrefix=d[:i] for non-streaming mode
		// absOffset is 0 because d is the full document starting at offset 0
		tokens, consumed, err := tokenizeOne(
			d,           // buffer (full document)
			i,           // relative offset in buffer
			0,           // buffer start offset (0 for full document)
			ts,          // state
			posDoc,      // posDoc
			opt,         // options
			lastToken,   // last token
			nil,         // recentBuf (nil = use docPrefix)
			dst,         // allTokens (deprecated/unused)
			d[:i],       // docPrefix (for commentPrefix)
		)

		if err == io.EOF {
			// Should not happen in non-streaming mode, but handle gracefully
			break
		}
		if err != nil {
			return nil, err
		}

		// Append all tokens (handles multiline strings)
		for _, tok := range tokens {
			dst = append(dst, tok)
			lastToken = &tok
		}

		// Advance position
		i += consumed

		// Handle whitespace (consumed bytes but no token)
		if len(tokens) == 0 && consumed > 0 {
			// Whitespace was consumed, continue
			continue
		}
	}

	return dst, nil
}
