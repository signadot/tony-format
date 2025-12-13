package stream

import "fmt"

// Event represents a structural event from the decoder.
// Events correspond to the encoder's API methods, providing a symmetric
// encode/decode interface.
//
//tony:schemagen=event
type Event struct {
	Type EventType `tony:"field=t"`

	// Tag field (applies to value events: String, Int, Float, Bool, Null, BeginObject, BeginArray)
	Tag string `tony:"field=a optional"`

	// Value fields (only one is set based on Type)
	Key    string  `tony:"field=k optional"`
	String string  `tony:"field=s optional"`
	Int    int64   `tony:"field=i optional"`
	Float  float64 `tony:"field=f optional"`
	Bool   bool    `tony:"field=b optional"`

	// Comment fields (for EventHeadComment and EventLineComment)
	CommentLines []string `tony:"field=c optional"` // Comment text lines (from IR Node.Lines)
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

func (t EventType) MarshalText() ([]byte, error) {
	return []byte(t.String()), nil
}

func (t *EventType) UnmarshalText(d []byte) error {
	k := string(d)
	pt, ok := map[string]EventType{
		"BeginObject": EventBeginObject,
		"EndObject":   EventEndObject,
		"BeginArray":  EventBeginArray,
		"EndArray":    EventEndArray,
		"Key":         EventKey,
		"String":      EventString,
		"Int":         EventInt,
		"Float":       EventFloat,
		"Bool":        EventBool,
		"Null":        EventNull,
		"HeadComment": EventHeadComment,
		"LineComment": EventLineComment,
	}[k]
	if ok {
		*t = pt
		return nil
	}
	return fmt.Errorf("unknown type %q", k)
}
