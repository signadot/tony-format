package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"go.lsp.dev/protocol"
)

// Map yt color attributes to LSP semantic token types
func mapColorToSemanticTokenType(nodeType ir.Type, attr encode.ColorAttr) protocol.SemanticTokenTypes {
	switch attr {
	case encode.TagColor:
		return protocol.SemanticTokenKeyword // Tags like !insert, !delete are keywords
	case encode.CommentColor:
		return protocol.SemanticTokenComment
	case encode.FieldColor:
		return protocol.SemanticTokenProperty // Object fields/keys
	case encode.ValueColor:
		switch nodeType {
		case ir.StringType:
			return protocol.SemanticTokenString
		case ir.NumberType:
			return protocol.SemanticTokenNumber
		case ir.BoolType:
			return protocol.SemanticTokenKeyword
		case ir.NullType:
			return protocol.SemanticTokenKeyword
		default:
			return protocol.SemanticTokenString
		}
	case encode.SepColor:
		return protocol.SemanticTokenOperator // Separators like :, -, etc.
	case encode.MergeColor:
		return protocol.SemanticTokenOperator // Merge operators like <<
	case encode.LiteralSingleColor, encode.LiteralMultiColor:
		return protocol.SemanticTokenString
	default:
		return protocol.SemanticTokenString
	}
}

// Map yt color attributes to LSP semantic token modifiers
func mapColorToSemanticTokenModifiers(nodeType ir.Type, attr encode.ColorAttr) []protocol.SemanticTokenModifiers {
	modifiers := []protocol.SemanticTokenModifiers{}

	if attr == encode.TagColor {
		modifiers = append(modifiers, "definition")
	}

	if attr == encode.MergeColor {
		modifiers = append(modifiers, "modification")
	}

	return modifiers
}

// getLineContent returns the content of a specific line from the document
func getLineContent(content string, lineNum int) string {
	lines := []rune(content)
	currentLine := 0
	lineStart := 0

	for i, r := range lines {
		if currentLine == lineNum {
			lineStart = i
			break
		}
		if r == '\n' {
			currentLine++
		}
	}

	// Find end of line
	lineEnd := lineStart
	for lineEnd < len(lines) && lines[lineEnd] != '\n' {
		lineEnd++
	}

	return string(lines[lineStart:lineEnd])
}

// findTokenInLine finds a token in a line starting at a given column
func findTokenInLine(line string, startCol int, tokenText string) (int, int) {
	// Try to find the token text starting at or near startCol
	lineRunes := []rune(line)
	if startCol >= len(lineRunes) {
		return startCol, 0
	}

	// Look for the token text starting from startCol
	tokenRunes := []rune(tokenText)
	for i := startCol; i <= len(lineRunes)-len(tokenRunes); i++ {
		match := true
		for j, tr := range tokenRunes {
			if i+j >= len(lineRunes) || lineRunes[i+j] != tr {
				match = false
				break
			}
		}
		if match {
			return i, len(tokenRunes)
		}
	}

	// If exact match not found, return startCol with estimated length
	return startCol, len(tokenRunes)
}

// collectSemanticTokens traverses the AST and collects semantic tokens
func (s *Server) collectSemanticTokens(doc *document) []uint32 {
	if doc.node == nil {
		return nil
	}

	tokens := []uint32{}

	// We need to collect tokens in order of appearance
	// Store tokens with their positions
	type tokenInfo struct {
		line      uint32
		character uint32
		length    uint32
		tokenType protocol.SemanticTokenTypes
		modifiers []protocol.SemanticTokenModifiers
	}

	var tokenList []tokenInfo

	// Visit all nodes and collect semantic tokens
	var visit func(*ir.Node)
	visit = func(node *ir.Node) {
		if node == nil {
			return
		}

		pos := doc.positions[node]
		if pos != nil {
			line, col := pos.LineCol()
			lineContent := getLineContent(doc.content, line)

			// Handle tags - tags appear before values
			if node.Tag != "" {
				// Tag format is typically "!tag" or "!tag "
				tagText := node.Tag
				if tagText[0] != '!' {
					tagText = "!" + tagText
				}
				char, length := findTokenInLine(lineContent, col, tagText)
				tokenList = append(tokenList, tokenInfo{
					line:      uint32(line),
					character: uint32(char),
					length:    uint32(length),
					tokenType: mapColorToSemanticTokenType(node.Type, encode.TagColor),
					modifiers: mapColorToSemanticTokenModifiers(node.Type, encode.TagColor),
				})
			}

			// Handle comments
			if node.Type == ir.CommentType {
				if node.String != "" {
					char, length := findTokenInLine(lineContent, col, node.String)
					tokenList = append(tokenList, tokenInfo{
						line:      uint32(line),
						character: uint32(char),
						length:    uint32(length),
						tokenType: mapColorToSemanticTokenType(node.Type, encode.CommentColor),
						modifiers: mapColorToSemanticTokenModifiers(node.Type, encode.CommentColor),
					})
				}
			}

			// Handle object fields (keys)
			if node.Parent != nil && node.Parent.Type == ir.ObjectType {
				// Check if this is a field (key)
				for i, field := range node.Parent.Fields {
					if field == node {
						// This is a field key
						fieldText := node.String
						if fieldText != "" {
							char, length := findTokenInLine(lineContent, col, fieldText)
							tokenList = append(tokenList, tokenInfo{
								line:      uint32(line),
								character: uint32(char),
								length:    uint32(length),
								tokenType: mapColorToSemanticTokenType(node.Type, encode.FieldColor),
								modifiers: mapColorToSemanticTokenModifiers(node.Type, encode.FieldColor),
							})

							// Find and highlight the separator ":"
							if i < len(node.Parent.Values) {
								sepStart := char + length
								// Skip whitespace to find ":"
								for sepStart < len([]rune(lineContent)) && []rune(lineContent)[sepStart] == ' ' {
									sepStart++
								}
								if sepStart < len([]rune(lineContent)) && []rune(lineContent)[sepStart] == ':' {
									tokenList = append(tokenList, tokenInfo{
										line:      uint32(line),
										character: uint32(sepStart),
										length:    1,
										tokenType: mapColorToSemanticTokenType(ir.ObjectType, encode.SepColor),
										modifiers: mapColorToSemanticTokenModifiers(ir.ObjectType, encode.SepColor),
									})
								}
							}
						}
						break
					}
				}
			}

			// Handle values (but not if they're object fields or already handled)
			isField := false
			if node.Parent != nil && node.Parent.Type == ir.ObjectType {
				for _, field := range node.Parent.Fields {
					if field == node {
						isField = true
						break
					}
				}
			}

			if !isField && node.Type != ir.ObjectType && node.Type != ir.ArrayType && node.Type != ir.CommentType {
				var valueText string
				switch node.Type {
				case ir.StringType:
					valueText = node.String
				case ir.NumberType:
					// Try to find number in source first (most accurate)
					lineRunes := []rune(lineContent)
					if col < len(lineRunes) {
						start := col
						// Skip whitespace
						for start < len(lineRunes) && lineRunes[start] == ' ' {
							start++
						}
						end := start
						// Find number (including negative, decimal)
						if end < len(lineRunes) && lineRunes[end] == '-' {
							end++
						}
						for end < len(lineRunes) && ((lineRunes[end] >= '0' && lineRunes[end] <= '9') || lineRunes[end] == '.' || lineRunes[end] == 'e' || lineRunes[end] == 'E' || lineRunes[end] == '+' || lineRunes[end] == '-') {
							end++
						}
						if end > start {
							valueText = string(lineRunes[start:end])
						}
					}
					// Fallback to node values if not found in source
					if valueText == "" {
						if node.Number != "" {
							valueText = string(node.Number)
						} else if node.Int64 != nil {
							valueText = fmt.Sprintf("%d", *node.Int64)
						} else if node.Float64 != nil {
							valueText = fmt.Sprintf("%g", *node.Float64)
						}
					}
				case ir.BoolType:
					if node.Bool {
						valueText = "true"
					} else {
						valueText = "false"
					}
				case ir.NullType:
					valueText = "null"
				}

				if valueText != "" && node.Tag == "" {
					char, length := findTokenInLine(lineContent, col, valueText)
					tokenList = append(tokenList, tokenInfo{
						line:      uint32(line),
						character: uint32(char),
						length:    uint32(length),
						tokenType: mapColorToSemanticTokenType(node.Type, encode.ValueColor),
						modifiers: mapColorToSemanticTokenModifiers(node.Type, encode.ValueColor),
					})
				}
			}
		}

		// Visit children
		for _, child := range node.Values {
			visit(child)
		}
		for _, field := range node.Fields {
			visit(field)
		}
		if node.Comment != nil {
			visit(node.Comment)
		}
	}

	visit(doc.node)

	// Sort tokens by line and character (required for LSP delta encoding)
	sort.Slice(tokenList, func(i, j int) bool {
		if tokenList[i].line != tokenList[j].line {
			return tokenList[i].line < tokenList[j].line
		}
		return tokenList[i].character < tokenList[j].character
	})

	// Build token type and modifier maps
	// These must match the legend in main.go
	tokenTypes := []protocol.SemanticTokenTypes{
		protocol.SemanticTokenComment,
		protocol.SemanticTokenKeyword,
		protocol.SemanticTokenString,
		protocol.SemanticTokenNumber,
		protocol.SemanticTokenOperator,
		protocol.SemanticTokenProperty,
	}

	tokenModifiers := []protocol.SemanticTokenModifiers{
		protocol.SemanticTokenModifierDefinition,
		protocol.SemanticTokenModifierModification,
	}

	typeMap := make(map[protocol.SemanticTokenTypes]uint32)
	for i, tt := range tokenTypes {
		typeMap[tt] = uint32(i)
	}

	modifierMap := make(map[protocol.SemanticTokenModifiers]uint32)
	for i, tm := range tokenModifiers {
		modifierMap[tm] = uint32(i)
	}

	// Encode tokens in LSP format
	var prevLine uint32 = 0
	var prevChar uint32 = 0

	for _, ti := range tokenList {
		deltaLine := ti.line - prevLine
		deltaChar := uint32(0)
		if deltaLine == 0 {
			deltaChar = ti.character - prevChar
		} else {
			deltaChar = ti.character
		}

		tokenType, ok := typeMap[ti.tokenType]
		if !ok {
			// Default to string if not found
			tokenType = 2
		}

		tokenModifierBits := uint32(0)
		for _, mod := range ti.modifiers {
			if modIdx, ok := modifierMap[mod]; ok {
				tokenModifierBits |= (1 << modIdx)
			}
		}

		tokens = append(tokens, deltaLine, deltaChar, ti.length, tokenType, tokenModifierBits)

		prevLine = ti.line
		prevChar = ti.character
	}

	return tokens
}

func (s *Server) SemanticTokensFull(ctx context.Context, params *protocol.SemanticTokensParams) (*protocol.SemanticTokens, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil || doc.node == nil {
		return &protocol.SemanticTokens{
			Data: []uint32{},
		}, nil
	}

	tokens := s.collectSemanticTokens(doc)

	return &protocol.SemanticTokens{
		Data: tokens,
	}, nil
}

func (s *Server) SemanticTokensRange(ctx context.Context, params *protocol.SemanticTokensRangeParams) (*protocol.SemanticTokens, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil || doc.node == nil {
		return &protocol.SemanticTokens{
			Data: []uint32{},
		}, nil
	}

	// For range, we filter tokens within the range
	// For simplicity, we'll return all tokens for now
	// A more sophisticated implementation would filter by range
	tokens := s.collectSemanticTokens(doc)

	return &protocol.SemanticTokens{
		Data: tokens,
	}, nil
}
