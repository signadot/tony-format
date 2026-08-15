package stream

import (
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/ir"
)

// NodeToEvents converts an ir.Node to a sequence of events.
// Returns events that can be written via Encoder.
//
// Head comments (a CommentType node with 1 value) emit EventHeadComment before
// the value; line comments (a CommentType node in the Comment field) emit
// EventLineComment after it.
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
		// A comment does not wrap a comment: one value has one set of preceding
		// comments, which compose as lines. Refused here rather than written,
		// because the stream cannot express the difference -- two wrappers and one
		// wrapper of two lines are the same pair of events -- so what goes in
		// cannot come back, and the loss would be silent (3cdjz00jh12krns4g1n0).
		if node.Values[0].Type == ir.CommentType {
			return fmt.Errorf("a head comment wraps a head comment at %s: a value has one set of "+
				"preceding comments, composed as lines", node.Path())
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
		}
		*events = append(*events, Event{Type: EventEndObject})

	case ir.ArrayType:
		*events = append(*events, Event{Type: EventBeginArray, Tag: node.Tag})
		for _, valueNode := range node.Values {
			if err := nodeToEvents(valueNode, events); err != nil {
				return err
			}
		}
		*events = append(*events, Event{Type: EventEndArray})

	case ir.StringType:
		*events = append(*events, Event{Type: EventString, String: node.String, Tag: node.Tag})

	case ir.NumberType:
		if node.Float64 != nil {
			*events = append(*events, Event{Type: EventFloat, Float: *node.Float64, Tag: node.Tag})
		} else if node.Int64 != nil {
			*events = append(*events, Event{Type: EventInt, Int: *node.Int64, Tag: node.Tag})
		} else {
			return fmt.Errorf("number node has neither Float64 nor Int64 set")
		}

	case ir.BoolType:
		*events = append(*events, Event{Type: EventBool, Bool: node.Bool, Tag: node.Tag})

	case ir.NullType:
		*events = append(*events, Event{Type: EventNull, Tag: node.Tag})

	default:
		return fmt.Errorf("unsupported node type: %v", node.Type)
	}

	// One place, after the value it belongs to -- which for a container is after
	// its EndObject/EndArray, where a reader expects it.
	emitLineComment(node, events)
	return nil
}

type nodeFrame struct {
	node   *ir.Node
	key    string
	intKey *int64
}

// wrapWithHeadComment wraps a node with a pending head comment if present
// emitLineComment writes a value's trailing comment.
//
// One caller: nodeToEvents, after the value it belongs to. Both the value's own
// case and its container used to write it, so every line comment appeared twice
// in the stream; it round-tripped only because the second overwrote the first
// with the same lines. Emitting after the switch also puts a container's own
// comment after its EndObject, where it belongs.
func emitLineComment(node *ir.Node, events *[]Event) {
	if node == nil || node.Comment == nil {
		return
	}
	*events = append(*events, Event{Type: EventLineComment, CommentLines: node.Comment.Lines})
}

// wrapWithHeadComment wraps a node in the comment waiting for it, if there is
// one. There is at most one: a value has one set of preceding comments, and a
// comment does not wrap a comment -- see nodeToEvents, which refuses to write
// the shape, and EventsToNode, which refuses to read it.
func wrapWithHeadComment(node *ir.Node, pending **ir.Node) *ir.Node {
	if *pending == nil {
		return node
	}
	wrap := *pending
	wrap.Values = []*ir.Node{node}
	node.Parent = wrap
	node.ParentIndex = 0
	*pending = nil
	return wrap
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
// Comment events become the IR shapes NodeToEvents writes them from: a head
// comment wraps the value that follows it, a line comment rides on the value
// before it.
func EventsToNode(events []Event) (*ir.Node, error) {
	if len(events) == 0 {
		return nil, nil
	}

	state := NewState()
	var stack []nodeFrame
	var root *ir.Node
	var pendingHead *ir.Node

	for i, ev := range events {
		if err := state.ProcessEvent(&ev); err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}

		switch ev.Type {
		case EventBeginObject:
			// The wrapper goes to the parent; the OBJECT goes on the stack. A
			// head comment wraps the container in a CommentType node, and
			// pushing the wrapper made the next EventKey look for its object and
			// find a comment: "unexpected EventKey (not in object)". A commented
			// document was written to the log and could never be read back.
			obj := ir.FromMap(map[string]*ir.Node{}).WithTag(ev.Tag)
			addNodeToParent(&stack, wrapWithHeadComment(obj, &pendingHead), &root)
			stack = append(stack, nodeFrame{node: obj})

		case EventEndObject:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected EventEndObject at event %d", i)
			}
			stack = stack[:len(stack)-1]

		case EventBeginArray:
			arr := ir.FromSlice([]*ir.Node{}).WithTag(ev.Tag)
			addNodeToParent(&stack, wrapWithHeadComment(arr, &pendingHead), &root)
			stack = append(stack, nodeFrame{node: arr})

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
			node := wrapWithHeadComment(ir.FromString(ev.String).WithTag(ev.Tag), &pendingHead)
			addNodeToParent(&stack, node, &root)

		case EventInt:
			node := wrapWithHeadComment(ir.FromInt(ev.Int).WithTag(ev.Tag), &pendingHead)
			addNodeToParent(&stack, node, &root)

		case EventFloat:
			node := wrapWithHeadComment(ir.FromFloat(ev.Float).WithTag(ev.Tag), &pendingHead)
			addNodeToParent(&stack, node, &root)

		case EventBool:
			node := wrapWithHeadComment(ir.FromBool(ev.Bool).WithTag(ev.Tag), &pendingHead)
			addNodeToParent(&stack, node, &root)

		case EventNull:
			node := wrapWithHeadComment(ir.Null().WithTag(ev.Tag), &pendingHead)
			addNodeToParent(&stack, node, &root)

		case EventHeadComment:
			if pendingHead != nil {
				return nil, fmt.Errorf("two head comments before one value at event %d: a value has "+
					"one set of preceding comments, composed as lines", i)
			}
			pendingHead = &ir.Node{
				Type:  ir.CommentType,
				Lines: ev.CommentLines,
			}

		case EventLineComment:
			commentNode := &ir.Node{
				Type:  ir.CommentType,
				Lines: ev.CommentLines,
			}

			// The line comment belongs to the VALUE, not to what was said above
			// it, so a head comment's wrapper is looked through -- which is where
			// the parser puts it, and where mergeop's comment operator puts it. Set
			// on the wrapper instead, a value carrying both comments came back from
			// the stream with the line one attached a level too high, and compared
			// as different to the document it was written from
			// (3cdjz00jh12krns4g1n0).
			var target *ir.Node
			if len(stack) == 0 {
				target = root
			} else if parent := &stack[len(stack)-1]; len(parent.node.Values) > 0 {
				target = parent.node.Values[len(parent.node.Values)-1]
			}
			if target = ir.Uncomment(target); target != nil {
				target.Comment = commentNode
				commentNode.Parent = target
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
