package main

import (
	"context"

	"go.lsp.dev/protocol"
)

func (s *Server) Completion(ctx context.Context, params *protocol.CompletionParams) (*protocol.CompletionList, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil {
		return nil, nil
	}

	pos := params.Position
	line := int(pos.Line)
	col := int(pos.Character)

	// Get the line content up to the cursor
	contentLines := []rune(doc.content)
	currentLineStart := 0
	currentLine := 0
	for i, r := range contentLines {
		if currentLine == line {
			currentLineStart = i
			break
		}
		if r == '\n' {
			currentLine++
		}
	}

	lineEnd := currentLineStart + col
	if lineEnd > len(contentLines) {
		lineEnd = len(contentLines)
	}
	for i := currentLineStart; i < len(contentLines) && i < lineEnd; i++ {
		if contentLines[i] == '\n' {
			lineEnd = i
			break
		}
	}

	var lineContent string
	if currentLineStart < len(contentLines) {
		if lineEnd > len(contentLines) {
			lineEnd = len(contentLines)
		}
		lineContent = string(contentLines[currentLineStart:lineEnd])
	} else {
		lineContent = ""
	}

	// Provide completions based on context
	completions := []protocol.CompletionItem{}

	// If we're at the start of a line or after certain characters, suggest common constructs
	if col == 0 || (col > 0 && len(lineContent) > 0 && (lineContent[col-1] == ' ' || lineContent[col-1] == '\n')) {
		// Suggest common Tony keywords/constructs
		completions = append(completions,
			protocol.CompletionItem{
				Label:      "null",
				Kind:       protocol.CompletionItemKindKeyword,
				InsertText: "null",
			},
			protocol.CompletionItem{
				Label:      "true",
				Kind:       protocol.CompletionItemKindKeyword,
				InsertText: "true",
			},
			protocol.CompletionItem{
				Label:      "false",
				Kind:       protocol.CompletionItemKindKeyword,
				InsertText: "false",
			},
		)
	}

	// If we see '!', suggest tag completions
	if col > 0 && len(lineContent) > 0 && lineContent[col-1] == '!' {
		completions = append(completions,
			protocol.CompletionItem{
				Label:      "!insert",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: "!insert ",
				Documentation: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: "Insert operation tag",
				},
			},
			protocol.CompletionItem{
				Label:      "!delete",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: "!delete",
				Documentation: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: "Delete operation tag",
				},
			},
			protocol.CompletionItem{
				Label:      "!replace",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: "!replace ",
				Documentation: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: "Replace operation tag",
				},
			},
		)
	}

	// If we see ':', suggest value completions
	if col > 0 && len(lineContent) > 0 && lineContent[col-1] == ':' {
		completions = append(completions,
			protocol.CompletionItem{
				Label:      "null",
				Kind:       protocol.CompletionItemKindValue,
				InsertText: " null",
			},
			protocol.CompletionItem{
				Label:      "empty object",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: " {}\n",
			},
			protocol.CompletionItem{
				Label:      "empty array",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: " []\n",
			},
		)
	}

	// If we see '|', suggest block literal
	if col > 0 && len(lineContent) > 0 && lineContent[col-1] == '|' {
		completions = append(completions,
			protocol.CompletionItem{
				Label:      "block literal",
				Kind:       protocol.CompletionItemKindSnippet,
				InsertText: "|\n  ",
				Documentation: protocol.MarkupContent{
					Kind:  protocol.Markdown,
					Value: "Block literal string",
				},
			},
		)
	}

	return &protocol.CompletionList{
		IsIncomplete: false,
		Items:       completions,
	}, nil
}
