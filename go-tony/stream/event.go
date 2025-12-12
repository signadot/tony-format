package stream

// Event represents a structural event from the decoder.
// Events correspond to the encoder's API methods, providing a symmetric
// encode/decode interface.
type Event struct {
	Type EventType `tony:"field=type"`

	// Tag field (applies to value events: String, Int, Float, Bool, Null, BeginObject, BeginArray)
	Tag string `tony:"field=tag"`

	// Value fields (only one is set based on Type)
	Key    string  `tony:"field=key"`
	String string  `tony:"field=string"`
	Int    int64   `tony:"field=int"`
	Float  float64 `tony:"field=float"`
	Bool   bool    `tony:"field=bool"`

	// Comment fields (for EventHeadComment and EventLineComment)
	CommentLines []string `tony:"field=commentLines"` // Comment text lines (from IR Node.Lines)
}

// EventType represents the type of a structural event.
type EventType int

const (
	EventBeginObject EventType = iota
	EventEndObject
	EventBeginArray
	EventEndArray
	EventKey
	EventString
	EventInt
	EventFloat
	EventBool
	EventNull
	EventHeadComment // Head comment (precedes a value) - IR: CommentType node with 1 value in Values
	EventLineComment // Line comment (on same line as value) - IR: CommentType node in Comment field
)

func (t EventType) String() string {
	switch t {
	case EventBeginObject:
		return "BeginObject"
	case EventEndObject:
		return "EndObject"
	case EventBeginArray:
		return "BeginArray"
	case EventEndArray:
		return "EndArray"
	case EventKey:
		return "Key"
	case EventString:
		return "String"
	case EventInt:
		return "Int"
	case EventFloat:
		return "Float"
	case EventBool:
		return "Bool"
	case EventNull:
		return "Null"
	case EventHeadComment:
		return "HeadComment"
	case EventLineComment:
		return "LineComment"
	default:
		return "Unknown"
	}
}
