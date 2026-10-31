package stream

import (
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

// Decoder provides structural event-based decoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type Decoder struct {
	// pendingTag is a tag read and not yet given to its value; see ReadEvent.
	pendingTag string
	source     *token.TokenSource // TokenSource uses new Tokenizer internally
	state      *State             // State for tracking structure/path
	opts       *streamOpts        // Options

	// Lookahead buffer for token-to-event conversion
	// We need to peek ahead to determine if TString/TLiteral is a key or value
	pendingTokens []token.Token

	// keySetNull says the last event was a key-set key, `{a b}`, whose null is the
	// next event; see keySetKey.
	keySetNull bool
	// elementBefore says a value or a close came last, so a ',' after it separates.
	// A ',' with no element before it is refused, in an object as in an array.
	elementBefore bool
}

// NewDecoder creates a new Decoder reading from r.
// Requires bracketed format (use WithBrackets() or WithWire()).
// Returns error if bracketing not specified.
func NewDecoder(r io.Reader, opts ...StreamOption) (*Decoder, error) {
	streamOpts := &streamOpts{}
	for _, opt := range opts {
		opt(streamOpts)
	}

	// Validate: must have brackets or wire format
	if !streamOpts.brackets && !streamOpts.wire {
		return nil, &Error{
			Msg: "stream decoder requires bracketed format: use stream.WithBrackets() or stream.WithWire()",
		}
	}

	// Create TokenSource (which uses new Tokenizer internally)
	// Wire format and brackets both use Tony format (brackets are enforced by stream package)
	tokenOpts := []token.TokenOpt{token.TokenTony()}
	source := token.NewTokenSource(r, tokenOpts...)

	return &Decoder{
		source:        source,
		state:         NewState(),
		opts:          streamOpts,
		pendingTokens: make([]token.Token, 0, 10),
	}, nil
}

// ReadEvent reads the next structural event from the stream.
// Returns structural events (BeginObject, Key, String, etc.) that correspond
// to the encoder's API. Low-level tokens (commas, colons) are elided.
// Returns io.EOF when stream is exhausted.
//
// Comment tokens become EventHeadComment and EventLineComment; see commentEvent.
func (d *Decoder) ReadEvent() (*Event, error) {
	event, err := d.readEvent()
	if err != nil {
		return nil, err
	}
	switch event.Type {
	case EventHeadComment, EventLineComment:
		// A comment is not an element, and does not stand between two.
	case EventBeginObject, EventBeginArray, EventKey, EventIntKey:
		d.elementBefore = false
	default:
		d.elementBefore = true
	}
	return event, nil
}

func (d *Decoder) readEvent() (*Event, error) {
	// A key-set key, `{a b}`, has a null value, and a tag written after the key goes
	// on it: `{a !t b}` is {a: !t null, b: null}, as the parser reads it
	// (docs/tony.md, Implicit Null; v9dn9vfbh12kssj2wsn0).
	if d.keySetNull {
		d.keySetNull = false
		event := &Event{Type: EventNull}
		next, err := d.nextToken()
		for err == nil && next.Type == token.TIndent {
			next, err = d.nextToken()
		}
		if err == nil {
			if next.Type == token.TTag {
				event.Tag = string(next.Bytes)
			} else {
				d.pushBack(next)
			}
		}
		if err := d.state.ProcessEvent(event); err != nil {
			return nil, err
		}
		return event, nil
	}

	for {
		// Get next token (from pending buffer or read from source)
		tok, err := d.nextToken()
		if err != nil {
			// A stream that ends on a tag has a tag with no value, refused as the
			// parser refuses it; it read as no document (vsdn9vfbh12ksnb6wsn0).
			if err == io.EOF && d.pendingTag != "" {
				return nil, &Error{Msg: "a tag must be followed by a value: " + d.pendingTag}
			}
			return nil, err
		}

		// A tag with no value is a tagged null, as the parser reads it: a `}`, `]` or
		// `,` after a tag ends the value there (token.balanceOne, pkj422gkh12kr24gj1n0).
		// Given to the closing bracket instead, the tag made an event the state
		// refused as a key with no value, and the session's reader ended on it
		// (yc5vx3z2h12kr89yvdn0).
		//
		// A line break or a comment before the bracket does not change that, and the
		// comment goes after the null, as it does after a written one; held until
		// the bracket, it went before the null as its head (vxdn9vfbh12krkkawsn0).
		if d.pendingTag != "" && d.endsTaggedValue(tok) {
			d.pushBack(tok)
			event := &Event{Type: EventNull, Tag: d.pendingTag}
			d.pendingTag = ""
			if err := d.state.ProcessEvent(event); err != nil {
				return nil, err
			}
			return event, nil
		}

		// A ',' separates two elements, so one with no element before it -- `[,1]`,
		// `[1,,2]`, `{a: 1,,b: 2}` -- is refused, as the parser refuses it. Dropped
		// with the other structural tokens, `[1, , 2]` read as [1, 2]
		// (v9dn9vfbh12kssj2wsn0).
		if tok.Type == token.TComma && d.state.Depth() > 0 {
			if !d.elementBefore {
				at := ""
				if tok.Pos != nil {
					at = " " + tok.Pos.String()
				}
				return nil, &Error{Msg: "non separating ','" + at}
			}
			d.elementBefore = false
		}

		// Skip structural tokens (commas, colons, indents)
		if tok.Type == token.TComma || tok.Type == token.TColon || tok.Type == token.TIndent {
			continue
		}

		// A comment is something the stream carries, not noise it drops. It used to
		// be dropped here, which is why nothing a client wrote ever reached a store:
		// the strip at patch time was the second gate, this was the first.
		//
		// Which comment it is was already decided by the tokenizer, and this does not
		// get to decide it again: TComment heads the value that follows, TLineComment
		// trails the value before it. Consecutive tokens of either kind compose into
		// ONE event, because a value has one set of preceding comments and one line
		// comment, both of which may run to several lines (docs/ir.md).
		if tok.Type == token.TComment || tok.Type == token.TLineComment {
			ev, err := d.commentEvent(tok)
			if err != nil {
				return nil, err
			}
			if ev == nil {
				continue // nothing was written there; see commentEvent
			}
			return ev, nil
		}

		// Handle tags - only TTag tokens (starting with !) are tags. The tag is
		// held on the decoder, not in this call: a comment between the tag and its
		// value is an event of its own, and the tag has to still be there when the
		// next call reaches the value. Held in a local, `!t # c` followed by the
		// value on the next line lost the tag (p478tacqh12krg32msn0 item 18).
		if tok.Type == token.TTag {
			// One node has one tag, as the parser says. The second replaced the first:
			// `[!t !u]` read as [!u null] (vndn9vfbh12ksgk6wsn0).
			if d.pendingTag != "" {
				return nil, &Error{Msg: "two tags on one node, " + d.pendingTag + " and " + string(tok.Bytes) +
					": tags compose with '.', as " + d.pendingTag + "." + strings.TrimPrefix(string(tok.Bytes), "!")}
			}
			d.pendingTag = string(tok.Bytes)
			// Continue to get the next token (the actual value)
			continue
		}

		// Convert token to event
		event, err := d.tokenToEvent(tok)
		if err != nil {
			return nil, err
		}

		// A key cannot be tagged, as the parser says: the tag was dropped with the
		// key it went on, `{!t a: 1}` read as {a: 1} (vddn9vfbh12krdv4wsn0).
		if d.pendingTag != "" && (event.Type == EventKey || event.Type == EventIntKey) {
			return nil, &Error{Msg: "key cannot be tagged: " + d.pendingTag}
		}

		// Set tag on event if present. A radix literal arrives with its notation as
		// its tag (intEvent), which goes in front of the written one as the parser
		// puts it (parse.composeNotation).
		if event.Tag != "" && d.pendingTag != "" {
			event.Tag = ir.TagCompose(event.Tag, nil, d.pendingTag)
		} else if d.pendingTag != "" {
			event.Tag = d.pendingTag
		}
		d.pendingTag = "" // Reset pending tag

		// Update state with event
		if err := d.state.ProcessEvent(event); err != nil {
			return nil, err
		}

		return event, nil
	}
}

// commentEvent gathers a run of comment tokens of one kind into a single event.
//
// The text conventions are the parser's, so a document that goes out through
// encode and comes back through here is the one that left: a head comment is
// trimmed, and a line comment keeps the whitespace between the value and its '#',
// which is what holds a column of them aligned.
func (d *Decoder) commentEvent(first token.Token) (*Event, error) {
	head := first.Type == token.TComment
	line := func(tok token.Token) string {
		if head {
			return strings.TrimSpace(string(tok.Bytes))
		}
		return string(tok.Bytes)
	}
	ev := &Event{Type: EventHeadComment, CommentLines: []string{line(first)}}
	if !head {
		ev.Type = EventLineComment
	}
	for {
		tok, err := d.nextToken()
		if err != nil {
			// The stream ended on a comment. It is still a comment: hand it back
			// and let the next read report the end.
			break
		}
		if tok.Type == token.TIndent {
			continue // indentation between comment lines is not a break in them
		}
		if tok.Type != first.Type {
			d.pushBack(tok)
			break
		}
		ev.CommentLines = append(ev.CommentLines, line(tok))
	}
	// The tokenizer marks a place a line comment COULD have been -- after a key,
	// and once per line of a multiline string -- with an empty one, so that a
	// column of them keeps its alignment. Empty places within a run are kept for
	// that reason; a run that is nothing but empty places is not a comment at all,
	// and emitting one would hang an empty comment on the value before it.
	if !slices.ContainsFunc(ev.CommentLines, func(l string) bool { return strings.TrimSpace(l) != "" }) {
		return nil, nil
	}
	if err := d.state.ProcessEvent(ev); err != nil {
		return nil, err
	}
	return ev, nil
}

// endsTaggedValue reports whether tok, or the first token after it past line
// breaks and comments, is a `}`, `]` or `,`, which ends a pending tag's value as a
// tagged null. The tokens it looks at past tok are returned to the buffer.
func (d *Decoder) endsTaggedValue(tok token.Token) bool {
	var seen []token.Token
	for tok.Type == token.TIndent || tok.Type == token.TComment || tok.Type == token.TLineComment {
		next, err := d.nextToken()
		if err != nil {
			break
		}
		seen = append(seen, next)
		tok = next
	}
	for i := len(seen) - 1; i >= 0; i-- {
		d.pushBack(seen[i])
	}
	return tok.Type == token.TRCurl || tok.Type == token.TRSquare || tok.Type == token.TComma
}

// pushBack returns a token to the head of the pending buffer, for a peek that
// turned out not to belong to what was being read.
func (d *Decoder) pushBack(tok token.Token) {
	d.pendingTokens = append([]token.Token{tok}, d.pendingTokens...)
}

// nextToken returns the next token, reading from source if pending buffer is empty.
func (d *Decoder) nextToken() (token.Token, error) {
	// If we have pending tokens, return the first one
	if len(d.pendingTokens) > 0 {
		tok := d.pendingTokens[0]
		d.pendingTokens = d.pendingTokens[1:]
		return tok, nil
	}

	// Read from source
	tokens, err := d.source.Read()
	if err != nil {
		// If source returns EOF or other error, propagate it
		return token.Token{}, err
	}

	if len(tokens) == 0 {
		// No tokens in this batch - source is exhausted
		return token.Token{}, io.EOF
	}

	// Return first token, save rest in pending buffer
	tok := tokens[0]
	d.pendingTokens = append(d.pendingTokens, tokens[1:]...)
	return tok, nil
}

// keySetKey is a key written with no ':' where an object's next key goes: a key-set
// key, whose null value readEvent emits next (docs/tony.md, Key Sets). Read as a
// value, it went into the object under the empty key (v9dn9vfbh12kssj2wsn0).
func (d *Decoder) keySetKey(key *Event) *Event {
	d.keySetNull = true
	return key
}

// tokenToEvent converts a token to an Event.
// May peek ahead to determine if TString/TLiteral is a key or value.
func (d *Decoder) tokenToEvent(tok token.Token) (*Event, error) {
	switch tok.Type {
	case token.TLCurl:
		return &Event{Type: EventBeginObject}, nil

	case token.TRCurl:
		return &Event{Type: EventEndObject}, nil

	case token.TLSquare:
		return &Event{Type: EventBeginArray}, nil

	case token.TRSquare:
		return &Event{Type: EventEndArray}, nil

	case token.TString, token.TLiteral:
		// Determine if this token is a key or value by reading ahead one token.
		// TString/TLiteral tokens can be either:
		//   - Keys: when followed by TColon (e.g., "key": value or "0": value in sparse arrays)
		//   - String values: when NOT followed by TColon
		// Use read+unread pattern: read next token, check if colon, unread if not colon.
		nextTok, err := d.nextToken()
		if err != nil {
			// Can't read next token (EOF) - this token must be a string value
			return &Event{
				Type:   EventString,
				String: tok.String(),
			}, nil
		}

		if nextTok.Type == token.TColon {
			return &Event{
				Type: EventKey,
				Key:  tok.String(),
			}, nil
		}

		// NOT followed by colon = it's a string value
		// Put nextTok back (unread) so ReadEvent() can process it in the next iteration
		// The nextTok could be any type (string, number, object start, etc.) - that's fine,
		// it will be handled by the next ReadEvent() call
		d.pendingTokens = append([]token.Token{nextTok}, d.pendingTokens...)
		if d.state.atKey() {
			return d.keySetKey(&Event{Type: EventKey, Key: tok.String()}), nil
		}
		// TString/TLiteral tokens are string tokens (tokenizer has already determined this),
		// so return EventString
		return &Event{
			Type:   EventString,
			String: tok.String(),
		}, nil

	case token.TMString, token.TMLit:
		// Multiline strings are always values
		return &Event{
			Type:   EventString,
			String: tok.String(),
		}, nil

	case token.TInteger:
		// Determine if this is a sparse array key or an integer value
		// If followed by TColon, it's a sparse array key; otherwise it's an integer value
		// Use read+unread pattern: read next token, check if colon, unread if not colon.
		nextTok, err := d.nextToken()
		if err != nil {
			// Can't read next token (EOF) - this token must be an integer value
			return intEvent(tok)
		}

		if nextTok.Type == token.TColon {
			if tok.Type == token.TInteger {
				i, err := strconv.ParseUint(string(tok.Bytes), 10, 32)
				if err != nil {
					return nil, err
				}
				return &Event{
					Type:   EventIntKey,
					IntKey: int64(i),
				}, nil

			}
			return &Event{
				Type: EventKey,
				Key:  string(tok.Bytes),
			}, nil
		}

		// NOT followed by colon = it's an integer value
		// Put nextTok back (unread) so ReadEvent() can process it in the next iteration
		d.pendingTokens = append([]token.Token{nextTok}, d.pendingTokens...)
		if d.state.atKey() {
			// A key set of integers, `{1 2 3}`, is a sparse array of nulls.
			i, err := strconv.ParseUint(string(tok.Bytes), 10, 32)
			if err != nil {
				return nil, err
			}
			return d.keySetKey(&Event{Type: EventIntKey, IntKey: int64(i)}), nil
		}
		return intEvent(tok)

	case token.TFloat:
		val, err := strconv.ParseFloat(string(tok.Bytes), 64)
		if err != nil {
			return nil, err
		}
		return &Event{
			Type:  EventFloat,
			Float: val,
		}, nil

	case token.TTrue:
		return &Event{
			Type: EventBool,
			Bool: true,
		}, nil

	case token.TFalse:
		return &Event{
			Type: EventBool,
			Bool: false,
		}, nil

	case token.TNull:
		return &Event{
			Type: EventNull,
		}, nil

	default:
		return nil, &Error{
			Msg: "unexpected token type: " + tok.Type.String(),
		}
	}
}

// Queryable State Methods (delegate to internal State)

// Depth returns the current nesting depth (0 = top level).
func (d *Decoder) Depth() int {
	return d.state.Depth()
}

// CurrentPath returns the current kinded path (e.g., "", "key", "key[0]").
func (d *Decoder) CurrentPath() string {
	return d.state.CurrentPath()
}

// IsInObject returns true if currently inside an object.
func (d *Decoder) IsInObject() bool {
	return d.state.IsInObject()
}

// IsInArray returns true if currently inside an array.
func (d *Decoder) IsInArray() bool {
	return d.state.IsInArray()
}

// CurrentKey returns the current object key (if in object).
func (d *Decoder) CurrentKey() (string, bool) {
	return d.state.CurrentKey()
}

// CurrentIndex returns the current array index (if in array).
func (d *Decoder) CurrentIndex() (int, bool) {
	return d.state.CurrentIndex()
}

// Reset resets the decoder to read from a new reader.
func (d *Decoder) Reset(r io.Reader, opts ...StreamOption) error {
	streamOpts := &streamOpts{}
	for _, opt := range opts {
		opt(streamOpts)
	}

	// Validate: must have brackets or wire format
	if !streamOpts.brackets && !streamOpts.wire {
		return &Error{
			Msg: "stream decoder requires bracketed format: use stream.WithBrackets() or stream.WithWire()",
		}
	}

	// Create new TokenSource
	// Wire format and brackets both use Tony format (brackets are enforced by stream package)
	tokenOpts := []token.TokenOpt{token.TokenTony()}
	d.source = token.NewTokenSource(r, tokenOpts...)
	d.state = NewState()
	d.opts = streamOpts
	d.pendingTokens = d.pendingTokens[:0]
	d.keySetNull = false
	d.elementBefore = false
	d.pendingTag = "" // a tag the last stream left went on the next one's first value

	return nil
}

// intEvent is the event for an integer token, read as the parser reads one
// (parse.numberNode): a radix literal -- 0x1f, 0o644, 0b101 -- is its value, with
// the notation as its tag. It was parsed in base 10 only, so a document holding one
// was refused, and a refused document ends a logd or docd session
// (4ynqp7wqh12krg32msn0 item 5).
func intEvent(tok token.Token) (*Event, error) {
	s := string(tok.Bytes)
	base, _, radix := token.RadixLiteral(s)
	if !radix {
		val, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return &Event{Type: EventInt, Int: val}, nil
	}
	val, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return nil, err
	}
	return &Event{Type: EventInt, Int: val, Tag: token.RadixNotation(base)}, nil
}
