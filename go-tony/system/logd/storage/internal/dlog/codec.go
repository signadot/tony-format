package dlog

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/signadot/tony-format/go-tony/stream"
)

// Log entries are stored as a compact binary event stream — the same encoding snapshot
// blob data already uses (snap.Builder writes stream.Event.WriteBinary), so a log file is
// now one encoding throughout rather than binary framing wrapped around tony text.
//
// Entries used to be written as block-style tony text, which made a log file readable with
// a pager at the storage layer. That is no longer how these are inspected, and the text
// form cost space and parse time on every read of every entry.
//
// Reading still accepts the old form: encodeEntry never emits it, but decodeEntry detects
// and parses it, so logs written before this change keep working with no migration step.

// legacyTextEntry reports whether d is an entry in the old block-style tony text form.
//
// The two encodings cannot be confused. A binary entry opens with a stream.EventType byte,
// and those run 0..12 (EventBeginObject..EventLineComment). A text entry opens with the
// '!entry' tag, so its first byte is '!' (0x21). Any byte at or above 0x20 is therefore
// text, and the check does not depend on the tag's spelling beyond its first character.
func legacyTextEntry(d []byte) bool {
	return len(d) > 0 && d[0] >= 0x20
}

// encodeEntry serializes an entry to the binary event stream stored in the log.
func encodeEntry(e *Entry) ([]byte, error) {
	node, err := e.ToTonyIR()
	if err != nil {
		return nil, fmt.Errorf("failed to build entry IR: %w", err)
	}
	events, err := stream.NodeToEvents(node)
	if err != nil {
		return nil, fmt.Errorf("failed to convert entry to events: %w", err)
	}
	buf := &bytes.Buffer{}
	for i := range events {
		if err := events[i].WriteBinary(buf); err != nil {
			return nil, fmt.Errorf("failed to write entry event %d: %w", i, err)
		}
	}
	return buf.Bytes(), nil
}

// decodeEntry parses an entry from the log, in either encoding.
func decodeEntry(d []byte) (*Entry, error) {
	entry := &Entry{}
	if legacyTextEntry(d) {
		if err := entry.FromTony(d); err != nil {
			return nil, err
		}
		return entry, nil
	}
	r := bytes.NewReader(d)
	var events []stream.Event
	for {
		var ev stream.Event
		if err := ev.ReadBinary(r); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("failed to read entry event %d: %w", len(events), err)
		}
		events = append(events, ev)
	}
	node, err := stream.EventsToNode(events)
	if err != nil {
		return nil, fmt.Errorf("failed to build entry from events: %w", err)
	}
	if err := entry.FromTonyIR(node); err != nil {
		return nil, err
	}
	return entry, nil
}
