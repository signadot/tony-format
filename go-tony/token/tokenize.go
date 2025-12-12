package token

import (
	"bytes"
	"io"
)

type tkState struct {
	cb, sb int
	// reset every '\n'
	lnIndent      int
	lineStartOffset int64 // absolute offset where current line started (after newline)
	kvSep         bool
	bElt          int
}

func Tokenize(dst []Token, src []byte, opts ...TokenOpt) ([]Token, error) {
	// Create Tokenizer for non-streaming mode
	tokenizer := NewTokenizerFromBytes(src, opts...)
	
	// Get the full document (Tokenizer already added trailing newline)
	d := tokenizer.doc
	n := len(d)
	
	// Handle initial indent
	i := 0
	indent := readIndent(d)
	if indent != 0 {
		tok := &Token{
			Type:  TIndent,
			Bytes: bytes.Repeat([]byte{' '}, indent),
			Pos:   tokenizer.posDoc.Pos(i),
		}
		dst = append(dst, *tok)
		tokenizer.ts.lnIndent = indent
		tokenizer.ts.lineStartOffset = int64(i) // Document starts at offset 0
		tokenizer.ts.kvSep = false
		tokenizer.ts.bElt = 0
		i += indent
	} else {
		tokenizer.ts.lineStartOffset = 0 // Document starts at offset 0
	}

	// Main loop: use Tokenizer.TokenizeOne() for all tokenization
	for i < n {
		// Call TokenizeOne with bufferStartOffset = 0 (full document starts at offset 0)
		tokens, consumed, err := tokenizer.TokenizeOne(d, i, 0)

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
			tokenizer.lastToken = &tok
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
