package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/token"
	"go.lsp.dev/protocol"
)

type documentStore struct {
	mu   sync.RWMutex
	docs map[string]*document
}

type document struct {
	uri       string
	content   string
	version   int32
	nodes     []*ir.Node
	positions map[*ir.Node]*token.Pos
}

func (ds *documentStore) get(uri string) *document {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.docs[uri]
}

func (ds *documentStore) put(uri string, content string, version int32) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	// Parse with position tracking
	positions := make(map[*ir.Node]*token.Pos)
	nodes, err := parse.ParseMulti([]byte(content), parse.ParseTony(), parse.ParsePositions(positions))
	if err != nil {
		// Store nil nodes on parse error, but keep the content
		ds.docs[uri] = &document{
			uri:       uri,
			content:   content,
			version:   version,
			nodes:     nil,
			positions: positions,
		}
		return
	}

	ds.docs[uri] = &document{
		uri:       uri,
		content:   content,
		version:   version,
		nodes:     nodes,
		positions: positions,
	}
}

func (ds *documentStore) remove(uri string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	delete(ds.docs, uri)
}

func (s *Server) publishDiagnostics(ctx context.Context, uri string) {
	doc := s.docs.get(uri)
	if doc == nil {
		return
	}

	diagnostics := s.validateDocument(doc)

	if s.conn != nil {
		s.conn.Notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, &protocol.PublishDiagnosticsParams{
			URI:         protocol.DocumentURI(uri),
			Diagnostics: diagnostics,
		})
	}
}

func (s *Server) validateDocument(doc *document) []protocol.Diagnostic {
	diagnostics := []protocol.Diagnostic{}

	if len(doc.nodes) == 0 {
		// Parse error - try to get position from error
		_, err := parse.Parse([]byte(doc.content), parse.ParseTony())
		if err != nil {
			diagnostic := protocol.Diagnostic{
				Range: protocol.Range{
					Start: protocol.Position{Line: 0, Character: 0},
					End:   protocol.Position{Line: 0, Character: 0},
				},
				Severity: protocol.DiagnosticSeverityError,
				Message:  err.Error(),
				Source:   "tony",
			}

			// Try to parse position from error string
			if pos := extractPosition(err.Error()); pos != nil {
				diagnostic.Range = protocol.Range{
					Start: protocol.Position{
						Line:      uint32(pos.line),
						Character: uint32(pos.col),
					},
					End: protocol.Position{
						Line:      uint32(pos.line),
						Character: uint32(pos.col + 1),
					},
				}
			}

			diagnostics = append(diagnostics, diagnostic)
		}
	}

	return diagnostics
}

type position struct {
	line int
	col  int
}

func extractPosition(errMsg string) *position {
	// Look for "line=X, col=Y" pattern
	var line, col int
	_, err := fmt.Sscanf(errMsg, "%*[^l]line=%d%*[^c]col=%d", &line, &col)
	if err != nil {
		return nil
	}
	return &position{line: line, col: col}
}

func (s *Server) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	s.docs.put(string(params.TextDocument.URI), params.TextDocument.Text, params.TextDocument.Version)
	s.publishDiagnostics(ctx, string(params.TextDocument.URI))
	return nil
}

func (s *Server) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil {
		return nil
	}

	// Apply changes
	content := doc.content
	for _, change := range params.ContentChanges {
		// Check if Range is a zero value (full document replacement)
		// Range is a struct, so check if it's a zero range
		rangeVal := change.Range
		if rangeVal.Start.Line == 0 && rangeVal.Start.Character == 0 && rangeVal.End.Line == 0 && rangeVal.End.Character == 0 {
			// Full document replacement
			content = change.Text
		} else {
			// Incremental change
			start := rangeVal.Start
			end := rangeVal.End
			contentRunes := []rune(content)
			startOffset := lineColToOffset(content, int(start.Line), int(start.Character))
			endOffset := lineColToOffset(content, int(end.Line), int(end.Character))
			if startOffset < len(contentRunes) && endOffset <= len(contentRunes) {
				content = string(contentRunes[:startOffset]) + change.Text + string(contentRunes[endOffset:])
			}
		}
	}

	s.docs.put(string(params.TextDocument.URI), content, params.TextDocument.Version)
	s.publishDiagnostics(ctx, string(params.TextDocument.URI))
	return nil
}

func (s *Server) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	s.docs.remove(string(params.TextDocument.URI))
	return nil
}

func lineColToOffset(content string, line, col int) int {
	currentLine := 0
	currentCol := 0
	for i, r := range content {
		if currentLine == line && currentCol == col {
			return i
		}
		if r == '\n' {
			currentLine++
			currentCol = 0
		} else {
			currentCol++
		}
	}
	return len(content)
}
