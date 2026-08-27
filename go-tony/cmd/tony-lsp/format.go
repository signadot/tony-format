package main

import (
	"bytes"
	"context"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/parse"
	"go.lsp.dev/protocol"
)

func (s *Server) Formatting(ctx context.Context, params *protocol.DocumentFormattingParams) ([]protocol.TextEdit, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil {
		return nil, nil
	}

	// Parse the document. WITH comments: the encode below asks for them, and
	// parsing without meant the formatter dropped every comment in the file.
	nodes, err := parse.ParseMulti([]byte(doc.content), parse.ParseTony(), parse.ParseComments(true))
	if err != nil {
		// If parsing fails, return no edits
		return nil, nil
	}
	// A document holding no VALUE -- one which is only a comment header, say --
	// formats to nothing, and an edit replacing the file with nothing is not a
	// formatting. ParseMulti answers no documents for one rather than one nil
	// document, so the loop below would build an empty result and offer it.
	if len(nodes) == 0 {
		return nil, nil
	}

	// Format the document
	var buf bytes.Buffer
	for i, node := range nodes {
		if i > 0 {
			buf.WriteString("\n---\n")
		}
		err = encode.Encode(node, &buf,
			encode.EncodeFormat(format.TonyFormat),
			encode.EncodeComments(true),
		)
		if err != nil {
			return nil, nil
		}
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
