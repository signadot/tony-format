package stream

import (
	"io"
	"strconv"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// Encoder provides explicit stack management for streaming Tony document encoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type Encoder struct {
	writer       io.Writer
	state        *State
	offset       int64
	opts         *streamOpts
	lastWasValue bool   // Track if last thing written was a value (for commas)
	pendingTag   string // Tag to apply to next value
}

// StreamOption configures Encoder/Decoder behavior.
type StreamOption func(*streamOpts)

type streamOpts struct {
	brackets bool // Force bracketed style
	wire     bool // Wire format (implies brackets)
}

// WithBrackets forces bracketed style encoding/decoding.
func WithBrackets() StreamOption {
	return func(opts *streamOpts) {
		opts.brackets = true
	}
}

// WithWire enables wire format (implies brackets).
func WithWire() StreamOption {
	return func(opts *streamOpts) {
		opts.wire = true
		opts.brackets = true // Wire format implies brackets
	}
}

// NewEncoder creates a new Encoder writing to w.
// Requires bracketed format (use WithBrackets() or WithWire()).
// Returns error if bracketing not specified.
func NewEncoder(w io.Writer, opts ...StreamOption) (*Encoder, error) {
	streamOpts := &streamOpts{}
	for _, opt := range opts {
		opt(streamOpts)
	}

	// Validate: must have brackets or wire format
	if !streamOpts.brackets && !streamOpts.wire {
		return nil, &Error{
			Msg: "stream encoder requires bracketed format: use stream.WithBrackets() or stream.WithWire()",
		}
	}

	return &Encoder{
		writer:       w,
		state:        NewState(),
		offset:       0,
		opts:         streamOpts,
		lastWasValue: false,
		pendingTag:   "",
	}, nil
}

// Error represents a stream error.
type Error struct {
	Msg string
}

func (e *Error) Error() string {
	return e.Msg
}

// Queryable State Methods

// Depth returns the current nesting depth (0 = top level).
func (e *Encoder) Depth() int {
	return e.state.Depth()
}

// CurrentPath returns the current kinded path (e.g., "", "key", "key[0]").
func (e *Encoder) CurrentPath() string {
	return e.state.CurrentPath()
}

// ParentPath returns the parent path (one level up).
func (e *Encoder) ParentPath() string {
	return e.state.ParentPath()
}

// IsInObject returns true if currently inside an object.
func (e *Encoder) IsInObject() bool {
	return e.state.IsInObject()
}

// IsInArray returns true if currently inside an array.
func (e *Encoder) IsInArray() bool {
	return e.state.IsInArray()
}

// CurrentKey returns the current object key (if in object).
func (e *Encoder) CurrentKey() string {
	return e.state.CurrentKey()
}

// CurrentIndex returns the current array index (if in array).
func (e *Encoder) CurrentIndex() int {
	return e.state.CurrentIndex()
}

// Offset returns the byte offset in the output stream.
func (e *Encoder) Offset() int64 {
	return e.offset
}

// CurrentTag returns the currently pending tag (if any).
// Returns empty string if no tag is pending.
func (e *Encoder) CurrentTag() string {
	return e.pendingTag
}

// Tag sets the tag for the next value to be written.
// The tag will be applied to the next call to BeginObject, BeginArray,
// WriteString, WriteInt, WriteFloat, WriteBool, or WriteNull.
// After writing the value, the tag is cleared.
// Returns an error if a tag is already pending (not yet consumed by a value).
func (e *Encoder) Tag(tag string) error {
	if e.pendingTag != "" {
		return &Error{
			Msg: "tag already pending for the next object",
		}
	}
	e.pendingTag = tag
	return nil
}

// TagCompose composes a tag with arguments and the currently pending tag (if any).
// If there's a pending tag, it composes the new tag with the pending tag.
// The new tag comes first in the composition (e.g., if pending is "!schema" and
// you call TagCompose("!bracket", nil), the result is "!bracket.schema").
// If there's no pending tag, it just sets the new tag.
// The tag will be applied to the next call to BeginObject, BeginArray,
// WriteString, WriteInt, WriteFloat, WriteBool, or WriteNull.
// After writing the value, the tag is cleared.
func (e *Encoder) TagCompose(tag string, args []string) error {
	e.pendingTag = ir.TagCompose(tag, args, e.pendingTag)
	return nil
}

// Structure Control Methods

// BeginObject begins an object (or sparse array).
// Note: Sparse arrays use BeginObject/EndObject (semantic distinction at parse layer).
// Uses the tag set by Tag() if one was set.
func (e *Encoder) BeginObject() error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
		e.lastWasValue = false
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	if err := e.writeBytes([]byte("{")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventBeginObject, Tag: tag}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = false
	return nil
}

// EndObject ends an object.
func (e *Encoder) EndObject() error {
	if err := e.writeBytes([]byte("}")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventEndObject}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// BeginArray begins a regular array.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) BeginArray() error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
		e.lastWasValue = false
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	if err := e.writeBytes([]byte("[")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventBeginArray, Tag: tag}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = false
	return nil
}

// EndArray ends an array.
func (e *Encoder) EndArray() error {
	if err := e.writeBytes([]byte("]")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventEndArray}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// Value Writing Methods

// WriteKey writes an object key.
func (e *Encoder) WriteKey(key string) error {
	// Add comma if needed (not first key-value pair in object)
	if e.lastWasValue && e.state.IsInObject() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
		e.lastWasValue = false
	}

	// Quote key if needed
	needsQuote := token.NeedsQuote(key)
	var keyBytes []byte
	if needsQuote {
		keyBytes = []byte(token.Quote(key, true))
	} else {
		keyBytes = []byte(key)
	}

	if err := e.writeBytes(keyBytes); err != nil {
		return err
	}

	// Write colon with space
	if err := e.writeBytes([]byte(": ")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventKey, Key: key}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = false
	return nil
}

func (e *Encoder) WriteIntKey(key int) error {
	// Add comma if needed (not first key-value pair in object)
	if e.lastWasValue && e.state.IsInObject() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
		e.lastWasValue = false
	}

	if err := e.writeBytes([]byte(strconv.Itoa(key))); err != nil {
		return err
	}

	// Write colon with space
	if err := e.writeBytes([]byte(": ")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventIntKey, IntKey: int64(key)}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = false
	return nil
}

// WriteString writes a string value.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) WriteString(value string) error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	// Quote string
	quoted := token.Quote(value, true)
	if err := e.writeBytes([]byte(quoted)); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventString, Tag: tag, String: value}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// WriteInt writes an integer value.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) WriteInt(value int64) error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	// Format integer
	intStr := strconv.FormatInt(value, 10)
	if err := e.writeBytes([]byte(intStr)); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventInt, Tag: tag, Int: value}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// WriteFloat writes a float value.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) WriteFloat(value float64) error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	// Format float
	floatStr := strconv.FormatFloat(value, 'g', -1, 64)
	if err := e.writeBytes([]byte(floatStr)); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventFloat, Tag: tag, Float: value}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// WriteBool writes a boolean value.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) WriteBool(value bool) error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	// Format boolean
	var boolStr string
	if value {
		boolStr = "true"
	} else {
		boolStr = "false"
	}
	if err := e.writeBytes([]byte(boolStr)); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventBool, Tag: tag, Bool: value}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// WriteNull writes a null value.
// Uses the tag set by Tag() if one was set.
func (e *Encoder) WriteNull() error {
	// Add comma if needed (not first element in array)
	if e.lastWasValue && e.state.IsInArray() {
		if err := e.writeBytes([]byte(",")); err != nil {
			return err
		}
	}

	// Write tag if present
	tag := e.pendingTag
	e.pendingTag = "" // Clear pending tag
	if tag != "" {
		if err := e.writeTag(tag); err != nil {
			return err
		}
		if err := e.writeBytes([]byte(" ")); err != nil {
			return err
		}
	}

	// Write null
	if err := e.writeBytes([]byte("null")); err != nil {
		return err
	}

	// Update state
	event := Event{Type: EventNull, Tag: tag}
	if err := e.state.ProcessEvent(&event); err != nil {
		return err
	}

	e.lastWasValue = true
	return nil
}

// Comment Writing Methods

// WriteHeadComment writes a head comment (precedes a value).
// IR: CommentType node with 1 value in Values.
// Phase 1: No-op (comment support deferred).
func (e *Encoder) WriteHeadComment(lines []string) error {
	// Phase 1: No-op
	// Phase 2: Write comment tokens before next value
	return nil
}

// WriteLineComment writes a line comment (on same line as value).
// IR: CommentType node in Comment field.
// Phase 1: No-op (comment support deferred).
func (e *Encoder) WriteLineComment(lines []string) error {
	// Phase 1: No-op
	// Phase 2: Write comment tokens after current value
	return nil
}

// Control Methods

// Flush flushes any buffered data.
func (e *Encoder) Flush() error {
	// For now, io.Writer doesn't buffer, so nothing to flush
	// If we add buffering later, flush here
	return nil
}

// Reset resets the encoder to write to a new writer.
func (e *Encoder) Reset(w io.Writer, opts ...StreamOption) error {
	streamOpts := &streamOpts{}
	for _, opt := range opts {
		opt(streamOpts)
	}

	// Validate: must have brackets or wire format
	if !streamOpts.brackets && !streamOpts.wire {
		return &Error{
			Msg: "stream encoder requires bracketed format: use stream.WithBrackets() or stream.WithWire()",
		}
	}

	e.writer = w
	e.state = NewState()
	e.offset = 0
	e.opts = streamOpts
	e.lastWasValue = false
	e.pendingTag = ""

	return nil
}

// writeTag writes a tag to the writer.
func (e *Encoder) writeTag(tag string) error {
	return e.writeBytes([]byte(tag))
}

// writeBytes writes bytes to the writer and updates offset.
func (e *Encoder) writeBytes(data []byte) error {
	n, err := e.writer.Write(data)
	if err != nil {
		return err
	}
	e.offset += int64(n)
	return nil
}
