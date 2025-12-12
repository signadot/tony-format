// Package stream provides streaming encode/decode for Tony documents.
//
// The stream package provides structural event-based encoding and decoding
// optimized for streaming use cases like snapshot indexing. It only supports
// bracketed structures ({...} and [...]) and does not handle formatting
// options like colors, comments, or block style.
//
// For general parsing/encoding with full feature support, use the parse
// and encode packages instead.
//
// # Example: Encoding
//
//	enc, err := stream.NewEncoder(writer, stream.WithBrackets())
//	if err != nil {
//	    return err
//	}
//	enc.BeginObject()
//	enc.WriteKey("name")
//	enc.WriteString("value")
//	enc.EndObject()
//
// # Example: Encoding with Tags
//
//	enc, err := stream.NewEncoder(writer, stream.WithBrackets())
//	if err != nil {
//	    return err
//	}
//	enc.BeginObject()
//	enc.WriteKey("name")
//	enc.Tag("!schema(string)")
//	enc.WriteString("John")
//	enc.EndObject()
//
// # Example: Decoding
//
//	dec, err := stream.NewDecoder(reader, stream.WithBrackets())
//	if err != nil {
//	    return err
//	}
//	event, _ := dec.ReadEvent()  // EventBeginObject (with Tag field if present)
//	event, _ := dec.ReadEvent()  // EventKey("name")
//	event, _ := dec.ReadEvent()  // EventString("value", with Tag field if present)
//	event, _ := dec.ReadEvent()  // EventEndObject
//
// # Comments
//
// The API is comment-ready (aligned with IR specification):
//   - Head comments: precede a value (IR: CommentType node with 1 value in Values)
//   - Line comments: on same line as value (IR: CommentType node in Comment field)
//
// Comment support is deferred to Phase 2. In Phase 1, comment methods are no-ops
// and comment tokens are skipped.
//
// # Tags
//
// Tags are supported and appear before values in the Tony format (e.g., !schema(...) value).
// When encoding, call Tag() before writing a value to set the tag for that value.
// The tag is automatically cleared after the value is written. When decoding, tags are
// automatically read and included in the Event.Tag field.
package stream
