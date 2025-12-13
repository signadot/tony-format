package stream

import (
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// NodeToEvents converts an ir.Node to a sequence of events.
// Returns events that can be written via Encoder.
//
// Phase 1: Comments are skipped (not included in events).
// Phase 2: Comments are converted to EventHeadComment or EventLineComment.
func NodeToEvents(node *ir.Node) ([]Event, error) {
	// TODO: Implement conversion
	// 1. Handle different node types
	// 2. For objects: EventBeginObject, EventKey, value events, EventEndObject
	// 3. For arrays: EventBeginArray, value events, EventEndArray
	// 4. For primitives: EventString, EventInt, etc.
	// 5. Phase 1: Skip comments
	// 6. Phase 2: Handle head comments (CommentType with 1 value) and line comments (CommentType in Comment field)

	return nil, nil
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

	// Use State to track structure depth and current context
	state := NewState()

	// Stack tracks the nodes being built (using State's depth)
	var stack []nodeFrame
	var root *ir.Node
	var pendingHeadComment *ir.Node

	for i, ev := range events {
		if err := state.ProcessEvent(&ev); err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}

		switch ev.Type {
		case EventBeginObject:
			node := ir.FromMap(map[string]*ir.Node{}).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			if len(stack) == 0 {
				root = node
			} else {
				parent := &stack[len(stack)-1]
				if parent.node.Type == ir.ObjectType {
					// Use FromKeyVals pattern - build key-value pair
					keyNode := ir.FromString(parent.key)
					parent.node.Fields = append(parent.node.Fields, keyNode)
					parent.node.Values = append(parent.node.Values, node)
					node.Parent = parent.node
					node.ParentIndex = len(parent.node.Values) - 1
					node.ParentField = parent.key
					keyNode.Parent = parent.node
					keyNode.ParentIndex = len(parent.node.Fields) - 1
					keyNode.ParentField = parent.key
				} else if parent.node.Type == ir.ArrayType {
					parent.node.Values = append(parent.node.Values, node)
					node.Parent = parent.node
					node.ParentIndex = len(parent.node.Values) - 1
				}
			}
			stack = append(stack, nodeFrame{node: node})

		case EventEndObject:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventEndObject at event %d", i)
			}
			stack = stack[:len(stack)-1]

		case EventBeginArray:
			node := ir.FromSlice([]*ir.Node{}).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			if len(stack) == 0 {
				root = node
			} else {
				parent := &stack[len(stack)-1]
				if parent.node.Type == ir.ObjectType {
					keyNode := ir.FromString(parent.key)
					parent.node.Fields = append(parent.node.Fields, keyNode)
					parent.node.Values = append(parent.node.Values, node)
					node.Parent = parent.node
					node.ParentIndex = len(parent.node.Values) - 1
					node.ParentField = parent.key
					keyNode.Parent = parent.node
					keyNode.ParentIndex = len(parent.node.Fields) - 1
					keyNode.ParentField = parent.key
				} else if parent.node.Type == ir.ArrayType {
					parent.node.Values = append(parent.node.Values, node)
					node.Parent = parent.node
					node.ParentIndex = len(parent.node.Values) - 1
				}
			}
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

		case EventString:
			node := ir.FromString(ev.String).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			addValueToStack(&stack, node, &root)

		case EventInt:
			node := ir.FromInt(ev.Int).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			addValueToStack(&stack, node, &root)

		case EventFloat:
			node := ir.FromFloat(ev.Float).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			addValueToStack(&stack, node, &root)

		case EventBool:
			node := ir.FromBool(ev.Bool).WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			addValueToStack(&stack, node, &root)

		case EventNull:
			node := ir.Null().WithTag(ev.Tag)

			if pendingHeadComment != nil {
				pendingHeadComment.Values = []*ir.Node{node}
				node.Parent = pendingHeadComment
				node.ParentIndex = 0
				node = pendingHeadComment
				pendingHeadComment = nil
			}

			addValueToStack(&stack, node, &root)

		case EventHeadComment:
			pendingHeadComment = &ir.Node{
				Type:  ir.CommentType,
				Lines: ev.CommentLines,
			}

		case EventLineComment:
			if len(stack) == 0 {
				if root != nil {
					root.Comment = &ir.Node{
						Type:  ir.CommentType,
						Lines: ev.CommentLines,
					}
					root.Comment.Parent = root
				}
			} else {
				parent := &stack[len(stack)-1]
				if parent.node.Type == ir.ObjectType && len(parent.node.Values) > 0 {
					lastValue := parent.node.Values[len(parent.node.Values)-1]
					lastValue.Comment = &ir.Node{
						Type:  ir.CommentType,
						Lines: ev.CommentLines,
					}
					lastValue.Comment.Parent = lastValue
				} else if parent.node.Type == ir.ArrayType && len(parent.node.Values) > 0 {
					lastValue := parent.node.Values[len(parent.node.Values)-1]
					lastValue.Comment = &ir.Node{
						Type:  ir.CommentType,
						Lines: ev.CommentLines,
					}
					lastValue.Comment.Parent = lastValue
				}
			}
		}
	}

	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed structures: %d remaining", len(stack))
	}

	return root, nil
}

// addValueToStack adds a value node to the current stack context
type nodeFrame struct {
	node *ir.Node
	key  string
}

func addValueToStack(stack *[]nodeFrame, node *ir.Node, root **ir.Node) {
	if len(*stack) == 0 {
		*root = node
	} else {
		parent := &(*stack)[len(*stack)-1]
		if parent.node.Type == ir.ObjectType {
			keyNode := ir.FromString(parent.key)
			parent.node.Fields = append(parent.node.Fields, keyNode)
			parent.node.Values = append(parent.node.Values, node)
			node.Parent = parent.node
			node.ParentIndex = len(parent.node.Values) - 1
			node.ParentField = parent.key
			keyNode.Parent = parent.node
			keyNode.ParentIndex = len(parent.node.Fields) - 1
			keyNode.ParentField = parent.key
		} else if parent.node.Type == ir.ArrayType {
			parent.node.Values = append(parent.node.Values, node)
			node.Parent = parent.node
			node.ParentIndex = len(parent.node.Values) - 1
		}
	}
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
