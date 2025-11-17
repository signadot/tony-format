package main

import (
	"bytes"
	"context"

	"go.lsp.dev/protocol"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/parse"
)

func (s *Server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil {
		return nil, nil
	}

	// Parse the document
	node, err := parse.Parse([]byte(doc.content), parse.ParseTony())
	if err != nil {
		// If parsing fails, return no edits
		return nil, nil
	}

	// Format the document
	var buf bytes.Buffer
	err = encode.Encode(node, &buf,
		encode.EncodeFormat(format.TonyFormat),
		encode.EncodeComments(true),
	)
	if err != nil {
		return nil, nil
	}

	formatted := buf.String()

	// If content hasn't changed, return empty edits
	if formatted == doc.content {
		return []protocol.TextEdit{}, nil
	}

	// Calculate line count for the range
	lines := bytes.Count([]byte(doc.content), []byte("\n"))
	if len(doc.content) > 0 && doc.content[len(doc.content)-1] != '\n' {
		lines++
	}

	// Return a single edit that replaces the entire document
	return []protocol.TextEdit{
		{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End: protocol.Position{
					Line:      uint32(lines),
					Character: 0,
				},
			},
			NewText: formatted,
		},
	}, nil
}
