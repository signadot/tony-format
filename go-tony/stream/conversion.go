package stream

import (
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// NodeToEvents converts an ir.Node to a sequence of events.
// Returns events that can be written via Encoder.
//
// Phase 2: Comments are converted to EventHeadComment or EventLineComment.
// Head comments (CommentType node with 1 value) emit EventHeadComment before the value.
// Line comments (CommentType node in Comment field) emit EventLineComment after the value.
func NodeToEvents(node *ir.Node) ([]Event, error) {
	var events []Event
	if err := nodeToEvents(node, &events); err != nil {
		return nil, err
	}
	return events, nil
}

func nodeToEvents(node *ir.Node, events *[]Event) error {
	if node == nil {
		return fmt.Errorf("node cannot be nil")
	}

	if node.Type == ir.CommentType {
		if len(node.Values) != 1 {
			return fmt.Errorf("comment node must have exactly 1 value for head comment")
		}
		*events = append(*events, Event{Type: EventHeadComment, CommentLines: node.Lines})
		return nodeToEvents(node.Values[0], events)
	}

	switch node.Type {
	case ir.ObjectType:
		*events = append(*events, Event{Type: EventBeginObject, Tag: node.Tag})
		for i := 0; i < len(node.Fields); i++ {
			keyNode := node.Fields[i]
			valueNode := node.Values[i]
			if keyNode.Type == ir.NumberType && keyNode.Int64 != nil {
				*events = append(*events, Event{Type: EventIntKey, IntKey: *keyNode.Int64})
			} else {
				*events = append(*events, Event{Type: EventKey, Key: keyNode.String})
			}
			if err := nodeToEvents(valueNode, events); err != nil {
				return err
			}
			if valueNode.Comment != nil {
				*events = append(*events, Event{Type: EventLineComment, CommentLines: valueNode.Comment.Lines})
			}
		}
		*events = append(*events, Event{Type: EventEndObject})

	case ir.ArrayType:
		*events = append(*events, Event{Type: EventBeginArray, Tag: node.Tag})
		for _, valueNode := range node.Values {
			if err := nodeToEvents(valueNode, events); err != nil {
				return err
			}
			if valueNode.Comment != nil {
				*events = append(*events, Event{Type: EventLineComment, CommentLines: valueNode.Comment.Lines})
			}
		}
		*events = append(*events, Event{Type: EventEndArray})

	case ir.StringType:
		*events = append(*events, Event{Type: EventString, String: node.String, Tag: node.Tag})
		if node.Comment != nil {
			*events = append(*events, Event{Type: EventLineComment, CommentLines: node.Comment.Lines})
		}

	case ir.NumberType:
		if node.Float64 != nil {
			*events = append(*events, Event{Type: EventFloat, Float: *node.Float64, Tag: node.Tag})
		} else if node.Int64 != nil {
			*events = append(*events, Event{Type: EventInt, Int: *node.Int64, Tag: node.Tag})
		} else {
			return fmt.Errorf("number node has neither Float64 nor Int64 set")
		}
		if node.Comment != nil {
			*events = append(*events, Event{Type: EventLineComment, CommentLines: node.Comment.Lines})
		}

	case ir.BoolType:
		*events = append(*events, Event{Type: EventBool, Bool: node.Bool, Tag: node.Tag})
		if node.Comment != nil {
			*events = append(*events, Event{Type: EventLineComment, CommentLines: node.Comment.Lines})
		}

	case ir.NullType:
		*events = append(*events, Event{Type: EventNull, Tag: node.Tag})
		if node.Comment != nil {
			*events = append(*events, Event{Type: EventLineComment, CommentLines: node.Comment.Lines})
		}

	default:
		return fmt.Errorf("unsupported node type: %v", node.Type)
	}

	return nil
}

type nodeFrame struct {
	node   *ir.Node
	key    string
	intKey *int64
}

// wrapWithHeadComment wraps a node with a pending head comment if present
func wrapWithHeadComment(node *ir.Node, pendingComment **ir.Node) *ir.Node {
	if *pendingComment == nil {
		return node
	}
	(*pendingComment).Values = []*ir.Node{node}
	node.Parent = *pendingComment
	node.ParentIndex = 0
	result := *pendingComment
	*pendingComment = nil
	return result
}

// addNodeToParent adds a node to its parent container (object or array)
func addNodeToParent(stack *[]nodeFrame, node *ir.Node, root **ir.Node) {
	if len(*stack) == 0 {
		*root = node
		return
	}

	parent := &(*stack)[len(*stack)-1]
	if parent.node.Type == ir.ObjectType {
		var keyNode *ir.Node
		key := ""
		if parent.intKey != nil {
			keyNode = ir.FromInt(*parent.intKey)
		} else {
			keyNode = ir.FromString(parent.key)
			key = parent.key
		}

		parent.node.Fields = append(parent.node.Fields, keyNode)
		parent.node.Values = append(parent.node.Values, node)
		node.Parent = parent.node
		node.ParentIndex = len(parent.node.Values) - 1
		node.ParentField = key
		keyNode.Parent = parent.node
		keyNode.ParentIndex = len(parent.node.Fields) - 1

	} else if parent.node.Type == ir.ArrayType {
		parent.node.Values = append(parent.node.Values, node)
		node.Parent = parent.node
		node.ParentIndex = len(parent.node.Values) - 1
	}
}

// EventsToNode converts a sequence of events to an ir.Node.
// Takes events read from Decoder.
//
// Phase 1: Comment events are not present (comments skipped).
// Phase 2: Comment events are converted to IR comment nodes.
func EventsToNode(events []Event) (*ir.Node, error) {
	if len(events) == 0 {
		return nil, nil
	}

	state := NewState()
	var stack []nodeFrame
	var root *ir.Node
	var pendingHeadComment *ir.Node

	for i, ev := range events {
		if err := state.ProcessEvent(&ev); err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}

		switch ev.Type {
		case EventBeginObject:
			node := wrapWithHeadComment(ir.FromMap(map[string]*ir.Node{}).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)
			stack = append(stack, nodeFrame{node: node})

		case EventEndObject:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventEndObject at event %d", i)
			}
			stack = stack[:len(stack)-1]

		case EventBeginArray:
			node := wrapWithHeadComment(ir.FromSlice([]*ir.Node{}).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)
			stack = append(stack, nodeFrame{node: node})

		case EventEndArray:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventEndArray at event %d", i)
			}
			stack = stack[:len(stack)-1]

		case EventKey:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventKey at event %d (not in object)", i)
			}
			parent := &stack[len(stack)-1]
			if parent.node.Type != ir.ObjectType {
				return nil, fmt.Errorf("unexpected EventKey at event %d (not in object)", i)
			}
			parent.key = ev.Key

		case EventIntKey:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventIntKey at event %d (not in object)", i)
			}
			parent := &stack[len(stack)-1]
			if parent.node.Type != ir.ObjectType {
				return nil, fmt.Errorf("unexpected EventKey at event %d (not in object)", i)
			}
			parent.intKey = &ev.IntKey

		case EventString:
			node := wrapWithHeadComment(ir.FromString(ev.String).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)

		case EventInt:
			node := wrapWithHeadComment(ir.FromInt(ev.Int).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)

		case EventFloat:
			node := wrapWithHeadComment(ir.FromFloat(ev.Float).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)

		case EventBool:
			node := wrapWithHeadComment(ir.FromBool(ev.Bool).WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)

		case EventNull:
			node := wrapWithHeadComment(ir.Null().WithTag(ev.Tag), &pendingHeadComment)
			addNodeToParent(&stack, node, &root)

		case EventHeadComment:
			pendingHeadComment = &ir.Node{
				Type:  ir.CommentType,
				Lines: ev.CommentLines,
			}

		case EventLineComment:
			commentNode := &ir.Node{
				Type:  ir.CommentType,
				Lines: ev.CommentLines,
			}

			if len(stack) == 0 {
				if root != nil {
					root.Comment = commentNode
					commentNode.Parent = root
				}
			} else {
				parent := &stack[len(stack)-1]
				if len(parent.node.Values) > 0 {
					lastValue := parent.node.Values[len(parent.node.Values)-1]
					lastValue.Comment = commentNode
					commentNode.Parent = lastValue
				}
			}
		}
	}

	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed structures: %d remaining", len(stack))
	}

	return root, nil
}

// EncodeNode encodes an ir.Node to bytes using Encoder.
// Convenience function: NodeToEvents + Encoder.
func EncodeNode(node *ir.Node, w io.Writer, opts ...StreamOption) error {
	// TODO: Implement
	// 1. Convert node to events
	// 2. Create encoder
	// 3. Write events via encoder

	return nil
}

// DecodeNode decodes bytes to ir.Node using Decoder.
// Convenience function: Decoder + EventsToNode.
func DecodeNode(r io.Reader, opts ...StreamOption) (*ir.Node, error) {
	// TODO: Implement
	// 1. Create decoder
	// 2. Read all events
	// 3. Convert events to node

	return nil, nil
}
