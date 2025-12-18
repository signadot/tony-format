package stream

import (
	"encoding/binary"
	"fmt"
	"io"
)

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
	IntKey int64   `tony:"field=ik optional"`
	String string  `tony:"field=s optional"`
	Int    int64   `tony:"field=i optional"`
	Float  float64 `tony:"field=f optional"`
	Bool   bool    `tony:"field=b optional"`

	// Comment fields (for EventHeadComment and EventLineComment)
	CommentLines []string `tony:"field=c optional"` // Comment text lines (from IR Node.Lines)
}

// IsValueStart returns true if this event starts a value (as opposed to a key, end marker, or comment).
// Value-starting events are: BeginObject, BeginArray, String, Int, Float, Bool, Null.
func (e *Event) IsValueStart() bool {
	return e.Type == EventBeginObject ||
		e.Type == EventBeginArray ||
		e.Type == EventString ||
		e.Type == EventInt ||
		e.Type == EventFloat ||
		e.Type == EventBool ||
		e.Type == EventNull
}

// EventType represents the type of a structural event.
type EventType int

const (
	EventBeginObject EventType = iota
	EventEndObject
	EventBeginArray
	EventEndArray
	EventKey
	EventIntKey
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
	case EventIntKey:
		return "IntKey"
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

func (t EventType) IsKey() bool {
	switch t {
	case EventKey, EventIntKey:
		return true
	default:
		return false
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
		"IntKey":      EventIntKey,
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

// WriteBinary writes event in compact binary format.
// Format: [type:1byte][fields based on type]
// No per-event length prefix - snapshots use index chunk sizes.
func (e *Event) WriteBinary(w io.Writer) error {
	// Write type (1 byte)
	if err := writeByte(w, byte(e.Type)); err != nil {
		return err
	}

	// Write fields based on type
	switch e.Type {
	case EventKey:
		if err := writeString(w, e.Key); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventIntKey:
		if err := writeVarint(w, e.IntKey); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventString:
		if err := writeString(w, e.String); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventInt:
		if err := writeVarint(w, e.Int); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventFloat:
		if err := binary.Write(w, binary.LittleEndian, e.Float); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventBool:
		var b byte
		if e.Bool {
			b = 1
		}
		if err := writeByte(w, b); err != nil {
			return err
		}
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventNull:
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventBeginObject, EventBeginArray:
		if err := writeString(w, e.Tag); err != nil {
			return err
		}

	case EventHeadComment, EventLineComment:
		if err := writeStringSlice(w, e.CommentLines); err != nil {
			return err
		}

	case EventEndObject, EventEndArray:
		// No additional fields

	default:
		return fmt.Errorf("unknown event type: %d", e.Type)
	}

	return nil
}

// ReadBinary reads event in compact binary format.
func (e *Event) ReadBinary(r io.Reader) error {
	// Read type
	typeByte, err := readByte(r)
	if err != nil {
		return err
	}
	e.Type = EventType(typeByte)

	// Read fields based on type
	switch e.Type {
	case EventKey:
		if e.Key, err = readString(r); err != nil {
			return err
		}
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventIntKey:
		if e.IntKey, err = readVarint(r); err != nil {
			return err
		}
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventString:
		if e.String, err = readString(r); err != nil {
			return err
		}
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventInt:
		if e.Int, err = readVarint(r); err != nil {
			return err
		}
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventFloat:
		if err := binary.Read(r, binary.LittleEndian, &e.Float); err != nil {
			return err
		}
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventBool:
		b, err := readByte(r)
		if err != nil {
			return err
		}
		e.Bool = b != 0
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventNull:
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventBeginObject, EventBeginArray:
		if e.Tag, err = readString(r); err != nil {
			return err
		}

	case EventHeadComment, EventLineComment:
		if e.CommentLines, err = readStringSlice(r); err != nil {
			return err
		}

	case EventEndObject, EventEndArray:
		// No additional fields

	default:
		return fmt.Errorf("unknown event type: %d", e.Type)
	}

	return nil
}

// Binary encoding helpers

func writeByte(w io.Writer, b byte) error {
	_, err := w.Write([]byte{b})
	return err
}

func readByte(r io.Reader) (byte, error) {
	b := make([]byte, 1)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, err
	}
	return b[0], nil
}

func writeVarint(w io.Writer, v int64) error {
	buf := make([]byte, binary.MaxVarintLen64)
	n := binary.PutVarint(buf, v)
	_, err := w.Write(buf[:n])
	return err
}

func readVarint(r io.Reader) (int64, error) {
	return binary.ReadVarint(byteReaderAdapter{r})
}

func writeString(w io.Writer, s string) error {
	// Write length as varint
	if err := writeVarint(w, int64(len(s))); err != nil {
		return err
	}
	// Write string bytes
	_, err := w.Write([]byte(s))
	return err
}

func readString(r io.Reader) (string, error) {
	// Read length
	length, err := readVarint(r)
	if err != nil {
		return "", err
	}
	if length < 0 {
		return "", fmt.Errorf("negative string length: %d", length)
	}
	if length == 0 {
		return "", nil
	}
	// Read string bytes
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func writeStringSlice(w io.Writer, ss []string) error {
	// Write length
	if err := writeVarint(w, int64(len(ss))); err != nil {
		return err
	}
	// Write each string
	for _, s := range ss {
		if err := writeString(w, s); err != nil {
			return err
		}
	}
	return nil
}

func readStringSlice(r io.Reader) ([]string, error) {
	// Read length
	length, err := readVarint(r)
	if err != nil {
		return nil, err
	}
	if length < 0 {
		return nil, fmt.Errorf("negative slice length: %d", length)
	}
	if length == 0 {
		return nil, nil
	}
	// Read strings
	result := make([]string, length)
	for i := range result {
		if result[i], err = readString(r); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// byteReaderAdapter adapts io.Reader to io.ByteReader for binary.ReadVarint
type byteReaderAdapter struct {
	r io.Reader
}

func (b byteReaderAdapter) ReadByte() (byte, error) {
	return readByte(b.r)
}
