package stream

import (
	"io"
	"strconv"

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
// Phase 1: Comment tokens are skipped (no comment events emitted).
// Phase 2: Comment tokens are converted to EventHeadComment or EventLineComment.
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

		// Phase 1: Skip comment tokens
		if tok.Type == token.TComment {
			continue
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

// peekToken returns the next token without consuming it.
func (d *Decoder) peekToken() (token.Token, error) {
	// If we have pending tokens, return the first one
	if len(d.pendingTokens) > 0 {
		return d.pendingTokens[0], nil
	}

	// Read from source
	tokens, err := d.source.Read()
	if err != nil {
		return token.Token{}, err
	}

	if len(tokens) == 0 {
		return token.Token{}, io.EOF
	}

	// Save all tokens in pending buffer
	d.pendingTokens = append(d.pendingTokens, tokens...)
	return d.pendingTokens[0], nil
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

// ParentPath returns the parent path (one level up).
func (d *Decoder) ParentPath() string {
	return d.state.ParentPath()
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
func (d *Decoder) CurrentKey() string {
	return d.state.CurrentKey()
}

// CurrentIndex returns the current array index (if in array).
func (d *Decoder) CurrentIndex() int {
	return d.state.CurrentIndex()
}

// Offset returns the byte offset within the chunk being read.
// Note: Offset tracking is deferred - returns 0 for now.
func (d *Decoder) Offset() int64 {
	// TODO: Track offset from TokenSource
	return 0
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
