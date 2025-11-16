package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/token"
	"go.lsp.dev/protocol"
)

func (s *Server) Hover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc := s.docs.get(string(params.TextDocument.URI))
	if doc == nil || doc.node == nil {
		return nil, nil
	}

	pos := params.Position
	line := int(pos.Line)
	col := int(pos.Character)

	// Find the node at the given position using tracked positions
	targetNode := s.findNodeAtPosition(doc.node, doc.positions, line, col)
	if targetNode == nil {
		return nil, nil
	}

	// Build hover information
	hoverText := buildHoverText(targetNode)
	if hoverText == "" {
		return nil, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: hoverText,
		},
	}, nil
}

func (s *Server) findNodeAtPosition(root *ir.Node, positions map[*ir.Node]*token.Pos, line, col int) *ir.Node {
	// Find the most specific node that contains this position
	var bestNode *ir.Node
	var bestPos *token.Pos

	// Visit all nodes and find the one whose position matches
	var visit func(*ir.Node)
	visit = func(node *ir.Node) {
		if node == nil {
			return
		}

		pos := positions[node]
		if pos != nil {
			posLine, posCol := pos.LineCol()
			// Check if this position matches or is close
			if posLine == line {
				// Same line - check column proximity
				if bestPos == nil || abs(int(posCol)-col) < abs(int(bestPos.Col())-col) {
					bestNode = node
					bestPos = pos
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
	}

	visit(root)
	return bestNode
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func buildHoverText(node *ir.Node) string {
	if node == nil {
		return ""
	}

	var parts []string

	// Type information
	typeInfo := getTypeInfo(node)
	if typeInfo != "" {
		parts = append(parts, fmt.Sprintf("**Type:** %s", typeInfo))
	}

	// Tag information
	if node.Tag != "" {
		parts = append(parts, fmt.Sprintf("**Tag:** `%s`", node.Tag))
	}

	// Value information
	valueInfo := getValueInfo(node)
	if valueInfo != "" {
		parts = append(parts, fmt.Sprintf("**Value:** %s", valueInfo))
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, "\n\n")
}

func getTypeInfo(node *ir.Node) string {
	switch node.Type {
	case ir.NullType:
		return "null"
	case ir.BoolType:
		return "boolean"
	case ir.NumberType:
		if node.Int64 != nil {
			return "integer"
		}
		return "float"
	case ir.StringType:
		return "string"
	case ir.ArrayType:
		return "array"
	case ir.ObjectType:
		return "object"
	case ir.CommentType:
		return "comment"
	default:
		return "unknown"
	}
}

func getValueInfo(node *ir.Node) string {
	switch node.Type {
	case ir.NullType:
		return "`null`"
	case ir.BoolType:
		if node.Bool {
			return "`true`"
		}
		return "`false`"
	case ir.NumberType:
		if node.Int64 != nil {
			return fmt.Sprintf("`%d`", *node.Int64)
		}
		if node.Float64 != nil {
			return fmt.Sprintf("`%g`", *node.Float64)
		}
	case ir.StringType:
		if node.String != "" {
			val := node.String
			if len(val) > 50 {
				val = val[:50] + "..."
			}
			return fmt.Sprintf("`%s`", val)
		}
	case ir.ArrayType:
		if node.Values != nil {
			return fmt.Sprintf("array with %d elements", len(node.Values))
		}
	case ir.ObjectType:
		if node.Fields != nil {
			return fmt.Sprintf("object with %d keys", len(node.Fields))
		}
	}
	return ""
}
