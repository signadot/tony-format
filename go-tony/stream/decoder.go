package stream

import (
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/token"
)

// Decoder provides structural event-based decoding.
// Only supports bracketed structures ({...} and [...]).
// Block style (TArrayElt) is not supported.
type Decoder struct {
	source *token.TokenSource // TokenSource uses new Tokenizer internally
	state  *State             // State for tracking structure/path
	opts   *streamOpts        // Options

	// Lookahead buffer for token-to-event conversion
	// We need to peek ahead to determine if TString/TLiteral is a key or value
	pendingTokens []token.Token
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
	var pendingTag string
	for {
		// Get next token (from pending buffer or read from source)
		tok, err := d.nextToken()
		if err != nil {
			return nil, err
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

		// Handle tags - only TTag tokens (starting with !) are tags
		if tok.Type == token.TTag {
			pendingTag = string(tok.Bytes)
			// Continue to get the next token (the actual value)
			continue
		}

		// Convert token to event
		event, err := d.tokenToEvent(tok)
		if err != nil {
			return nil, err
		}

		// Set tag on event if present
		event.Tag = pendingTag
		pendingTag = "" // Reset pending tag

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
			val, err := strconv.ParseInt(string(tok.Bytes), 10, 64)
			if err != nil {
				return nil, err
			}
			return &Event{
				Type: EventInt,
				Int:  val,
			}, nil
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
		val, err := strconv.ParseInt(string(tok.Bytes), 10, 64)
		if err != nil {
			return nil, err
		}
		return &Event{
			Type: EventInt,
			Int:  val,
		}, nil

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

	return nil
}
