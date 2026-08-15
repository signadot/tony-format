package encode

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

type EncState struct {
	// atCol0 records that nothing has been written on the current line -- the one
	// question writeNL needs answered. It says nothing about whether the line's
	// indent has been written yet, so writeNL supplies the indent in this state
	// and only skips the newline; a caller that is at column 0 with the indent
	// already written must leave this false.
	//
	// This replaced a column counter that was maintained at two dozen sites and read
	// in exactly one, where it was compared against zero. The arithmetic was all in
	// service of that comparison, and two places wrote bytes without updating it.
	atCol0        bool
	depth, indent int
	brackets      bool
	comments      bool
	injectRaw     bool
	literal       bool

	format format.Format
	wire   bool

	colorType ir.Type
	colorAttr ColorAttr
	Color     func(ir.Type, ColorAttr, string) string
}

// Encode writes node to w.
//
// A nil node is an error and not a document. It arises because a patch reports a
// deletion by returning nil -- tony.Patch does, and so does the IR generally --
// so a caller that writes a patch's result without checking hands one here. That
// used to segfault at the first field read, which told the caller nothing about
// which of its documents had gone; say it instead.
func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
	if node == nil {
		return fmt.Errorf("cannot encode a nil node: nothing is not a document (a deletion is reported as a nil node)")
	}
	es := &EncState{
		indent: 2,
		atCol0: true,
	}
	for _, opt := range opts {
		opt(es)
	}
	if !es.brackets {
		es.brackets = es.format.IsJSON()
	}
	if es.comments {
		es.comments = !es.format.IsJSON() && !es.wire
	}
	if err := encode(node, w, es); err != nil {
		return err
	}
	if !es.comments {
		es.atCol0 = false
		es.depth = 0
		return writeNL(w, es)
	}
	trailing := node.Comment
	if node.Type == ir.CommentType && len(node.Values) == 1 {
		trailing = node.Values[0].Comment
	}
	if trailing == nil {
		return writeString(w, "\n")
	}
	lines := []string{}
	if isMultiLineString(trailing.Parent) {
		n := len(trailing.Parent.Lines)
		if len(trailing.Lines) > n {
			lines = trailing.Lines[n:]
		}
	} else {
		lines = trailing.Lines[1:]
	}
	if len(lines) == 0 {
		return writeString(w, "\n")
	}
	if err := writeString(w, "\n"); err != nil {
		return err
	}
	for _, ln := range lines {
		if es.Color != nil {
			ln = es.Color(ir.CommentType, ValueColor, ln)
		}
		if err := writeString(w, ln+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// Helper functions for writing
func writeNL(w io.Writer, es *EncState) error {
	if es.wire {
		return nil
	}
	indentString := strings.Repeat(strings.Repeat(" ", es.indent), es.depth)
	if es.atCol0 {
		// Already at column 0, but the indent for this line has not been written:
		// the newline came from content (a block literal's kept trailing newline,
		// or an empty line inside raw content) rather than from here. Opening
		// another line would leave a blank one behind, and skipping outright is
		// what dropped the indent and let the next key escape its parent.
		if indentString == "" {
			return nil
		}
		if err := writeString(w, indentString); err != nil {
			return err
		}
		es.atCol0 = false
		return nil
	}
	if err := writeString(w, "\n"+indentString); err != nil {
		return err
	}
	es.atCol0 = indentString == ""
	return nil
}

func writeString(w io.Writer, s string) error {
	_, err := w.Write([]byte(s))
	return err
}

func writeTag(w io.Writer, tag string, es *EncState) error {
	if es.format == format.JSONFormat {
		return fmt.Errorf("%w: cannot encode tags in %s", ErrEncoding, es.format)
	}
	if es.Color == nil {
		return writeString(w, tag)
	}
	return writeString(w, es.Color(es.colorType, TagColor, tag))
}

// Separator helpers

func writeCommaSeparator(w io.Writer, es *EncState, cType ir.Type, forMLString bool) error {
	var sep = ","
	switch es.format {
	case format.TonyFormat:
		if es.wire {
			sep = " "
		} else if !forMLString {
			return nil
		} else if !es.brackets {
			return nil
		}
	case format.JSONFormat:
		if es.wire {
			sep = ","
		}
	case format.YAMLFormat:
		if !esBracket(es) {
			sep = ""
		} else if es.wire {
			sep = ", "
		}
	default:
		panic("format")
	}
	es.atCol0 = false
	if es.Color != nil {
		sep = es.Color(cType, SepColor, sep)
	}
	return writeString(w, sep)
}

// String quoting helper

func quoteString(v string, es *EncState) string {
	switch es.format {
	case format.JSONFormat:
		return token.Quote(v, false)
	case format.YAMLFormat:
		if len(v) == 0 {
			return token.Quote(v, false)
		} else if token.NeedsQuote(v) {
			return token.Quote(v, false)
		} else {
			switch v[0] {
			case '*', '&', '%', '@', ':', '#', ',', '{', '[', '(', '-':
				return token.Quote(v, false)
			}
		}
		return v
	case format.TonyFormat:
		if token.NeedsQuote(v) {
			return token.Quote(v, true)
		}
		return v
	default:
		return v
	}
}

// Color application helpers

func applyColor(es *EncState, nodeType ir.Type, attr ColorAttr, v string) string {
	if es.Color == nil {
		return v
	}
	return es.Color(nodeType, attr, v)
}

func applyValueColor(es *EncState, nodeType ir.Type, v string) string {
	return applyColor(es, nodeType, ValueColor, v)
}

func applyStringColor(es *EncState, v string) string {
	attr := LiteralSingleColor
	for _, qc := range []string{"\"", "'"} {
		if strings.HasPrefix(v, qc) && strings.HasSuffix(v, qc) {
			attr = ValueColor
			break
		}
	}
	return applyColor(es, ir.StringType, attr, v)
}

// Main encode function

func encode(node *ir.Node, w io.Writer, es *EncState) error {
	es.colorType = node.Type
	if err := writeTagIfPresent(node, w, es); err != nil {
		return err
	}

	switch node.Type {
	case ir.ObjectType:
		return encodeObject(node, w, es)
	case ir.ArrayType:
		return encodeArray(node, w, es)
	case ir.StringType:
		return encodeString(node, w, es)
	case ir.NumberType:
		return encodeNumber(node, w, es)
	case ir.BoolType:
		return encodeBool(node, w, es)
	case ir.NullType:
		return encodeNull(node, w, es)
	case ir.CommentType:
		return encodeComment(node, w, es)
	default:
		panic("type")
	}
}

func writeTagIfPresent(node *ir.Node, w io.Writer, es *EncState) error {
	// Presentation tags are consumed as rendering directives elsewhere in this
	// package, so they are never written back out as tags.
	tag := ir.StripPresentation(node.Tag)
	if tag == "" {
		return nil
	}
	if err := writeTag(w, tag, es); err != nil {
		return err
	}
	es.atCol0 = false
	switch node.Type {
	case ir.ObjectType, ir.ArrayType:
		if len(node.Values) > 0 && !es.wire {
			return writeNL(w, es)
		}
		return writeString(w, " ")
	default:
		return writeString(w, " ")
	}
}

// writeFieldHeadComment writes the head comment of a field whose value is a
// SCALAR above the field's line, and answers the value to encode.
//
// "# above c" before "c: 2" parses onto c's value, and a comment before a value
// is written above the line that value is on -- which for a scalar is the field's
// own line. Encoding it where the value goes instead pushed the scalar onto a
// line of its own:
//
//	c:
//	  # above c
//	  2
//
// so encode(parse(x)) was not x. A container is different and already right: its
// comment sits inside the block, above the first line of it, which is where the
// document had it.
func writeFieldHeadComment(val *ir.Node, w io.Writer, es *EncState) (*ir.Node, error) {
	if !es.comments || es.wire || esBracket(es) {
		return val, nil
	}
	if val.Type != ir.CommentType || len(val.Values) != 1 {
		return val, nil
	}
	switch val.Values[0].Type {
	case ir.ObjectType, ir.ArrayType:
		return val, nil
	}
	es.colorType = ir.CommentType
	es.colorAttr = ValueColor
	for _, ln := range val.Lines {
		if err := writeRaw(w, ln, es); err != nil {
			return nil, err
		}
		if err := writeNL(w, es); err != nil {
			return nil, err
		}
	}
	return val.Values[0], nil
}

// writeBlockLatch writes the line comment of a field's value when that value is
// a block container, which has no line of its own to end.
//
// "a: # c" over an indented object parses the comment onto the object, and the
// only line it can go back on is the field's. A bracketed collection writes its
// own after the closing token (writeCloseLineComment); a block one had nowhere
// to write it and lost it.
func writeBlockLatch(val *ir.Node, w io.Writer, es *EncState) error {
	if !es.comments || es.wire || esBracket(es) || val.Comment == nil {
		return nil
	}
	// A value carrying the bracket tag writes braces of its own even here, so it
	// ends a line and writeCloseLineComment puts the comment after the close.
	// es.brackets is the ENCLOSING state and does not answer this.
	if ir.TagHas(val.Tag, ir.BracketTag) {
		return nil
	}
	switch val.Type {
	case ir.ObjectType:
		if len(val.Fields) == 0 {
			return nil // writes braces, so it ends a line of its own
		}
	case ir.ArrayType:
		if len(val.Values) == 0 {
			return nil
		}
	default:
		return nil // a scalar writes its own line comment
	}
	return writeLineCommentLines(w, val.Comment, es)
}

// encodeObject
func encodeObject(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.brackets && ir.TagHas(node.Tag, ir.BracketTag) {
		es.brackets = true
		defer func() { es.brackets = false }()
	}

	n := len(node.Fields)
	if err := writeObjectOpen(w, es, n); err != nil {
		return err
	}
	var (
		skipValue = false
		err       error
	)
	for i, yField := range node.Fields {
		val := node.Values[i]
		if err := writeObjectFieldPrefix(i, node, w, es); err != nil {
			return err
		}
		var err2 error
		val, err2 = writeFieldHeadComment(val, w, es)
		if err2 != nil {
			return err2
		}
		skipValue, err = encodeObjectField(yField, val, w, es)
		if err != nil {
			return err
		}
		if err := writeBlockLatch(val, w, es); err != nil {
			return err
		}
		if !skipValue {
			if err := encodeObjectValue(val, w, es); err != nil {
				return err
			}
		}
		if i < len(node.Fields)-1 {
			if err := writeCommaSeparator(w, es, ir.ObjectType, false); err != nil {
				return err
			}
		}
	}
	if err := writeObjectClose(w, es, n); err != nil {
		return err
	}
	return writeCloseLineComment(node, w, es, n)
}

// writeCloseLineComment writes the line comment of an object or array that ends
// in a closing token, as in "a: {} # c". A scalar writes its own line comment
// when it encodes itself; a collection cannot, because the comment belongs after
// the close. Without this the comment parsed onto the collection was dropped.
//
// Only a collection that writes a closing token can carry one: an indented block
// has no line of its own to end, and a comment on such a node came from before
// the block ("a: # c" over an indented object), which is not this position. The
// condition is writeObjectClose/writeArrayClose's, inverted.
func writeCloseLineComment(node *ir.Node, w io.Writer, es *EncState, n int) error {
	if !esBracket(es) && n != 0 {
		return nil
	}
	return writeLineCommentLines(w, node.Comment, es)
}

func writeObjectOpen(w io.Writer, es *EncState, nFields int) error {
	if !esBracket(es) && nFields != 0 {
		return nil
	}
	open := "{"
	es.atCol0 = false
	if err := writeString(w, open); err != nil {
		return err
	}
	es.depth++
	return nil
}

func writeObjectClose(w io.Writer, es *EncState, nFields int) error {
	if !esBracket(es) && nFields != 0 {
		return nil
	}
	es.depth--
	if !es.wire && nFields != 0 {
		if err := writeNL(w, es); err != nil {
			return err
		}
	}
	es.atCol0 = false
	return writeString(w, "}")
}

func writeObjectFieldPrefix(i int, node *ir.Node, w io.Writer, es *EncState) error {
	if es.wire {
		return nil
	}
	if es.brackets {
		return writeNL(w, es)
	}
	if i == 0 {
		if node.Parent != nil && node.Parent.Type == ir.ArrayType {
			return nil
		}
		if node.Tag != "" {
			return nil
		}
		if node.Parent != nil && node.Parent.Type == ir.CommentType {
			return nil
		}
	}
	return writeNL(w, es)
}

// encodeObjectField returns (skipValue, error) where skipValue indicates
// whether the value should be skipped (already written, e.g., merge field with injectRaw)
func encodeObjectField(yField, yVal *ir.Node, w io.Writer, es *EncState) (bool, error) {
	switch yField.Type {
	case ir.NullType:
		return encodeMergeField(yField, yVal, w, es)
	case ir.NumberType:
		err := encodeNumberField(yField, w, es)
		return false, err
	case ir.StringType:
		err := writeField(w, yField.String, es)
		return false, err
	default:
		return false, nil
	}
}

// encodeMergeField returns (skipValue, error) where skipValue is true
// when injectRaw is true (value already written as raw)
func encodeMergeField(yField, yVal *ir.Node, w io.Writer, es *EncState) (bool, error) {
	if es.format.IsJSON() {
		return false, format.ErrBadFormat
	}
	if !es.injectRaw {
		err := writeField(w, ir.MergeKey, es)
		return false, err
	}
	// Save and restore colorAttr to avoid using block literal colors for raw merge content
	oldColorAttr := es.colorAttr
	es.colorAttr = MergeRawColor
	es.colorType = yVal.Type

	switch yVal.Type {
	case ir.StringType:
		if err := writeRaw(w, yVal.String, es); err != nil {
			es.colorAttr = oldColorAttr
			return false, err
		}
	case ir.ObjectType:
		buf := bytes.NewBuffer(nil)
		subEncState := &EncState{}
		*subEncState = *es
		subEncState.atCol0 = true
		subEncState.depth = 0
		if err := encode(yVal, buf, subEncState); err != nil {
			es.colorAttr = oldColorAttr
			return false, err
		}
		if err := writeRaw(w, buf.String(), es); err != nil {
			es.colorAttr = oldColorAttr
			return false, err
		}
	default:
		es.colorAttr = oldColorAttr
		return false, fmt.Errorf("cannot encode null field (merge) with type %s", yVal.Type)
	}

	es.colorAttr = oldColorAttr
	return true, nil // Skip value encoding, already written
}

func encodeNumberField(yField *ir.Node, w io.Writer, es *EncState) error {
	if es.format.IsJSON() {
		return fmt.Errorf("%w: integer keys unsupported in %s", ErrEncoding, es.format)
	}
	if yField.Int64 == nil {
		return fmt.Errorf("number typed key without int value")
	}
	v := strconv.FormatInt(*yField.Int64, 10)
	es.atCol0 = false
	sep := ":"
	if es.Color != nil {
		v = applyColor(es, ir.NumberType, FieldColor, v)
		sep = applyColor(es, ir.ObjectType, SepColor, sep)
	}
	return writeString(w, v+sep)
}

func encodeObjectValue(node *ir.Node, w io.Writer, es *EncState) error {
	es.depth++
	defer func() { es.depth-- }()
	switch node.Type {
	case ir.ObjectType:
		if node.Tag != "" || es.wire && !es.format.IsJSON() || es.brackets || len(node.Fields) == 0 {
			if err := writeString(w, " "); err != nil {
				return err
			}
			es.atCol0 = false
		}
		br := false
		if esBracket(es) || ir.TagHas(node.Tag, ir.BracketTag) {
			es.depth--
			br = true
		}
		err := encode(node, w, es)
		if br {
			es.depth++
		}
		return err
	case ir.ArrayType:
		br := false
		if !esBracket(es) || ir.TagHas(node.Tag, ir.BracketTag) {
			es.depth--
			br = true
		}
		// Only write space if there's a tag or we're in bracket/wire mode.
		// Block arrays (non-bracket) go directly after colon with newline.
		// Empty arrays always need a space before the bracket.
		if node.Tag != "" || esBracket(es) || len(node.Values) == 0 {
			if err := writeString(w, " "); err != nil {
				return err
			}
			es.atCol0 = false
		}
		err := encode(node, w, es)
		if br {
			es.depth++
		}
		return err
	case ir.CommentType:
		if !esBracket(es) && node.Values[0].Type == ir.ArrayType {
			es.depth--
		}
		err := encodeCommentUnderField(node, w, es)
		if !esBracket(es) && node.Values[0].Type == ir.ArrayType {
			es.depth++
		}
		return err
	case ir.StringType:
		es.colorType = ir.StringType
		if !es.wire || !es.format.IsJSON() {
			if err := writeString(w, " "); err != nil {
				return err
			}
			es.atCol0 = false
		}
		if err := writeTagIfPresent(node, w, es); err != nil {
			return err
		}

		if doBlockLit(node, es) {
			es.depth--
			err := encodeBlockLit(node, w, es)
			es.depth++
			return err
		}
		if doMString(node, es) {
			return encodeMString(node, w, es)
		}
		return encodeStringOrLit(node, w, es)

	default:
		return encodeSimpleLeafValue(node, w, es)
	}
}

func encodeCommentUnderField(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.wire {
		if err := writeNL(w, es); err != nil {
			return err
		}
	}
	err := encode(node, w, es)
	return err
}

func encodeSimpleLeafValue(yVal *ir.Node, w io.Writer, es *EncState) error {
	if !es.wire || !es.format.IsJSON() {
		if err := writeString(w, " "); err != nil {
			return err
		}
		es.atCol0 = false
	}
	return encode(yVal, w, es)
}

// Array encoding

func encodeArray(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.brackets && ir.TagHas(node.Tag, ir.BracketTag) {
		es.brackets = true
		defer func() { es.brackets = false }()
	}
	n := len(node.Values)
	if err := writeArrayOpen(w, es, n); err != nil {
		return err
	}

	for i, v := range node.Values {

		if err := writeArrayElementPrefix(i, node, w, es); err != nil {
			return err
		}
		if err := writeArrayElementMarker(w, es); err != nil {
			return err
		}
		doDepth := !esBracket(es) && !ir.TagHas(v.Tag, ir.BracketTag)
		if doDepth {
			es.depth++
		}
		if err := encode(v, w, es); err != nil {
			return err
		}
		if i < len(node.Values)-1 {
			if err := writeCommaSeparator(w, es, ir.ArrayType, isMultiLineString(v)); err != nil {
				return err
			}
		}
		if doDepth {
			es.depth--
		}
	}
	if err := writeArrayClose(w, es, n); err != nil {
		return err
	}
	return writeCloseLineComment(node, w, es, n)
}

func writeArrayOpen(w io.Writer, es *EncState, nValues int) error {
	if !esBracket(es) && nValues != 0 {
		return nil
	}
	open := "["
	if err := writeString(w, open); err != nil {
		return err
	}
	es.atCol0 = false
	es.depth++
	return nil
}

func writeArrayClose(w io.Writer, es *EncState, nValues int) error {
	if !esBracket(es) && nValues != 0 {
		return nil
	}
	es.depth--
	if !es.wire && nValues > 0 {
		if err := writeNL(w, es); err != nil {
			return err
		}
	}
	es.atCol0 = false
	return writeString(w, "]")
}

func writeArrayElementPrefix(i int, node *ir.Node, w io.Writer, es *EncState) error {
	if i == 0 && !esBracket(es) {
		ncp := node.NonCommentParent()
		if ncp != nil && ncp.Type == ir.ArrayType {
			return nil
		}
		if node.Tag != "" {
			return nil
		}
		if node.Parent != ncp && ncp != nil && ncp.Type == ir.ObjectType {
			return nil
		}
	}
	return writeNL(w, es)
}

func writeArrayElementMarker(w io.Writer, es *EncState) error {
	if esBracket(es) {
		return nil
	}
	sep := "-"
	if es.Color != nil {
		sep = applyColor(es, ir.ArrayType, SepColor, sep)
	}
	sep += " "
	if err := writeString(w, sep); err != nil {
		return err
	}
	es.atCol0 = false
	return nil
}

// String encoding

func encodeString(node *ir.Node, w io.Writer, es *EncState) error {
	es.colorType = ir.StringType
	if doBlockLit(node, es) {
		return encodeBlockLit(node, w, es)
	}
	if !es.wire && len(node.Lines) != 0 && isTony(es) && strings.Join(node.Lines, "") == node.String {
		return encodeMString(node, w, es)
	}
	return encodeStringOrLit(node, w, es)
}

func encodeBlockLit(node *ir.Node, w io.Writer, es *EncState) error {
	startBLit := "|"
	v := node.String
	if v == "" || v[len(v)-1] != '\n' {
		startBLit += "-"
	} else {
		n := len(v) - 2
		for n >= 0 {
			if v[n] != '\n' && v[n] != ' ' && v[n] != '\r' {
				break
			}
			n--
		}
		if n < len(v)-2 {
			startBLit += "+"
		}
	}
	if err := writeString(w, startBLit); err != nil {
		return err
	}
	es.atCol0 = false
	es.depth++
	defer func() { es.depth-- }()
	if err := writeNL(w, es); err != nil {
		return err
	}
	es.colorAttr = LiteralMultiColor
	v = node.String
	if v != "" && v[len(v)-1] == '\n' {
		v = v[:len(v)-1]
	}
	if err := writeRaw(w, v, es); err != nil {
		return err
	}
	if strings.HasSuffix(startBLit, "+") {
		// "|+" keeps the trailing newline that was stripped above, closing the block's
		// last line. writeRaw leaves the cursor mid-line, so this cannot go through
		// writeNL -- it is one of the two places that write to w directly.
		//
		// Recording that we are now at column 0 is what makes the caller's writeNL skip
		// rather than open a second, empty line. Without it the value came back with one
		// more trailing newline than went in. The old column counter was never updated
		// here at all, so the state simply lied about where the cursor was.
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return err
		}
		es.atCol0 = true
	}
	return nil
}

func encodeMString(node *ir.Node, w io.Writer, es *EncState) error {
	commentLines := []string{}
	if node.Comment != nil && len(node.Comment.Lines) > 0 {
		commentLines = node.Comment.Lines
	}
	for i, ln := range node.Lines {
		if err := writeNL(w, es); err != nil {
			return err
		}
		ln = token.Quote(ln, true)
		ln = applyValueColor(es, ir.StringType, ln)
		if err := writeString(w, ln); err != nil {
			return err
		}
		es.atCol0 = false
		if i < len(commentLines) {
			if es.comments {
				commentText := commentLines[i]
				if commentText != "" {
					es.atCol0 = false
					commentText = applyValueColor(es, ir.CommentType, commentText)
					if err := writeString(w, commentText); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func encodeStringOrLit(node *ir.Node, w io.Writer, es *EncState) error {
	v := quoteString(node.String, es)
	es.atCol0 = false
	v = applyStringColor(es, v)
	if err := writeString(w, v); err != nil {
		return err
	}

	if node.Comment != nil {
		if err := writeLineCommentLines(w, node.Comment, es); err != nil {
			return err
		}
	}
	return nil
}

// Number encoding

// formatFloat renders a float so that reading it back yields the same float.
//
// Two things have to hold and neither did.  The text must parse as a number at all:
// 'f' format wrote the largest float64 as its 309 digit decimal expansion, which reads
// back as an integer token and overflows int64, so the encoder emitted a document Tony
// could not parse.  'g' switches to an exponent where that would happen.
//
// And the text must parse as a *float*: 'g' writes 1.0 as "1" and 1e2 as "100", which
// read back as integers, so an integral float silently changed type on a round trip.  A
// ".0" is appended when nothing else marks the value as a float.
//
// Zero is written "0.0" whatever its sign.  -0.0 and 0.0 are the same value -- DeepEqual
// says so, and Hash is made to agree -- and one value gets one text.
//
// Infinities and NaN have no Tony syntax.  They cannot come from parsing, since a number
// too large for float64 is refused, but they can arrive through the Go API, and writing
// "+Inf" would put back the unparseable output this function exists to prevent.
// expText renders a float in exponent form, for a value written that way and carrying
// ExpTag.  Go writes a padded, signed exponent -- 1e9 as "1e+09" -- which is not the text
// that went in, so the '+' and the padding come back off: one text per value, and the one
// the author wrote.
func expText(f float64) (string, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "", fmt.Errorf("%w: %v has no number syntax", ErrEncoding, f)
	}
	v := strconv.FormatFloat(f, 'e', -1, 64)
	i := strings.IndexAny(v, "eE")
	if i < 0 {
		return v, nil // no exponent to tidy; FormatFloat 'e' always writes one
	}
	mant, exp := v[:i], v[i+1:]
	neg := strings.HasPrefix(exp, "-")
	exp = strings.TrimLeft(exp, "+-")
	exp = strings.TrimLeft(exp, "0")
	if exp == "" {
		exp = "0"
	}
	if neg {
		exp = "-" + exp
	}
	return mant + "e" + exp, nil
}

func formatFloat(f float64) (string, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "", fmt.Errorf("%w: %v has no number syntax", ErrEncoding, f)
	}
	v := strconv.FormatFloat(f, 'g', -1, 64)
	if v == "0" || v == "-0" {
		return "0.0", nil
	}
	if !strings.ContainsAny(v, ".eE") {
		v += ".0"
	}
	return v, nil
}

// formatInt renders an integer in the notation its presentation tag names.
//
// JSON has no radix notation, so there the notation is dropped and the decimal value is
// written: 0x1f goes out as 31, which is the same number.  Tony and YAML both read the
// prefixed forms, so both keep them -- and for YAML that is the point, since 0o644 means
// 420 to a YAML reader where a bare 0644 means 420 to one reader and 644 to another.
func formatInt(node *ir.Node, es *EncState) string {
	i := *node.Int64
	if !es.format.IsJSON() {
		switch {
		case ir.TagHas(node.Tag, ir.HexTag):
			return radixText(i, 16, "0x")
		case ir.TagHas(node.Tag, ir.OctTag):
			return radixText(i, 8, "0o")
		case ir.TagHas(node.Tag, ir.BinTag):
			return radixText(i, 2, "0b")
		}
	}
	return strconv.FormatInt(i, 10)
}

// radixText writes i in the given base, with the sign ahead of the prefix so that the
// result reads back as the same number: "-0x1f", not "0x-1f".  Digits are lower case, one
// text per value, as the normalized form asks.
func radixText(i int64, base int, prefix string) string {
	if i < 0 {
		// Negating math.MinInt64 overflows, so the magnitude is taken unsigned.
		return "-" + prefix + strconv.FormatUint(-uint64(i), base)
	}
	return prefix + strconv.FormatUint(uint64(i), base)
}

func encodeNumber(node *ir.Node, w io.Writer, es *EncState) error {
	if node.Int64 != nil {
		v := formatInt(node, es)
		v = applyValueColor(es, ir.NumberType, v)
		es.atCol0 = false
		if err := writeString(w, v); err != nil {
			return err
		}
	}
	if node.Float64 != nil {
		v, err := formatFloat(*node.Float64)
		if err != nil {
			return err
		}
		if ir.TagHas(node.Tag, ir.ExpTag) {
			v, err = expText(*node.Float64)
			if err != nil {
				return err
			}
		}
		v = applyValueColor(es, ir.NumberType, v)
		es.atCol0 = false
		if err := writeString(w, v); err != nil {
			return err
		}
	}
	return writeLineCommentLines(w, node.Comment, es)
}

// Bool encoding

func encodeBool(node *ir.Node, w io.Writer, es *EncState) error {
	v := strconv.FormatBool(node.Bool)
	v = applyValueColor(es, ir.BoolType, v)
	if err := writeString(w, v); err != nil {
		return err
	}
	es.atCol0 = false
	return writeLineCommentLines(w, node.Comment, es)
}

// Null encoding

func encodeNull(node *ir.Node, w io.Writer, es *EncState) error {
	v := "null"
	v = applyValueColor(es, ir.NullType, v)
	if _, err := w.Write([]byte(v)); err != nil {
		return err
	}
	es.atCol0 = false
	return writeLineCommentLines(w, node.Comment, es)
}

// Comment encoding

func encodeComment(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.comments {
		if len(node.Values) != 0 {
			return encode(node.Values[0], w, es)
		}
		return nil
	}
	es.colorType = ir.CommentType
	es.colorAttr = ValueColor
	endNL := true
	for i, ln := range node.Lines {
		if err := writeRaw(w, ln, es); err != nil {
			return err
		}
		if !endNL && i == len(node.Lines)-1 {
			continue
		}
		if err := writeNL(w, es); err != nil {
			return err
		}
	}
	if len(node.Values) != 0 {
		return encode(node.Values[0], w, es)
	}
	return nil
}

// Field writing

func writeField(w io.Writer, f string, es *EncState) error {
	sep := ":"
	if isJSON(es) || token.NeedsQuote(f) {
		f = token.Quote(f, true)
	}
	fColor := f
	if es.Color != nil {
		if f == ir.MergeKey {
			fColor = applyColor(es, ir.ObjectType, MergeColor, f)
		} else {
			fColor = applyColor(es, ir.ObjectType, FieldColor, f)
		}
		sep = applyColor(es, ir.ObjectType, SepColor, sep)
	}
	ff := fColor + sep
	if err := writeString(w, ff); err != nil {
		return err
	}
	es.atCol0 = false
	return nil
}

// Raw writing

func writeRaw(w io.Writer, v string, es *EncState) error {
	lines := strings.Split(v, "\n")
	if len(lines) == 0 {
		return nil
	}
	n := len(lines)
	for i, ln := range lines {
		colorLn := ln
		if es.Color != nil && ln != "" {
			colorLn = es.Color(es.colorType, es.colorAttr, ln)
		}
		if err := writeString(w, colorLn); err != nil {
			return err
		}
		if i == n-1 {
			// Only update col if line is non-empty. If content ended with newline,
			// the last line is empty and we should keep col from previous writeNL
			// to ensure subsequent writeNL calls don't skip.
			if len(ln) > 0 {
				es.atCol0 = len(ln) == 0
			}
			break
		}
		es.atCol0 = false
		if err := writeNL(w, es); err != nil {
			es.atCol0 = len(ln) == 0
			return err
		}
		es.atCol0 = len(ln) == 0
	}
	return nil
}

// Line comment writing

func writeLineCommentLines(w io.Writer, c *ir.Node, es *EncState) error {
	if !es.comments || c == nil || len(c.Lines) == 0 {
		return nil
	}

	// Only write Lines[0] (the line comment on the same line as the value).
	// Lines[1:] (trailing comments) are written by the finalization code in Encode().
	ln := c.Lines[0]
	es.atCol0 = false
	ln = applyValueColor(es, ir.CommentType, ln)
	return writeString(w, ln)
}

func doBlockLit(node *ir.Node, es *EncState) bool {
	if es.wire || es.format.IsJSON() {
		return false
	}
	// A block literal writes its content out raw, so nothing inside one can be escaped.
	// U+FFFD has to be escaped or the document will not read back -- the scanners take a
	// decoded RuneError as bad utf8 -- so a value containing one takes the quoted form
	// instead, which escapes per line. This wins over an explicit literal request: a
	// formatting preference is not worth emitting a document that cannot be parsed.
	if strings.ContainsRune(node.String, utf8.RuneError) {
		return false
	}
	if es.literal || ir.TagHas(node.Tag, ir.LiteralTag) {
		return true
	}
	return strings.Contains(node.String, "\n")
}

func doMString(node *ir.Node, es *EncState) bool {
	if !es.format.IsTony() {
		return false
	}
	if es.wire {
		return false
	}
	return isMultiLineString(node)
}

func isMultiLineString(node *ir.Node) bool {
	if node.Type != ir.StringType {
		return false
	}
	if len(node.Lines) == 0 {
		return false
	}
	if strings.Join(node.Lines, "") != node.String {
		return false
	}
	return true
}

// Format check helpers

func isJSON(es *EncState) bool {
	return es.format == format.JSONFormat
}

func isWire(es *EncState) bool {
	return es.wire
}

func isJSONOrWire(es *EncState) bool {
	return isJSON(es) || isWire(es)
}

func esBracket(es *EncState) bool {
	if es.wire {
		return true
	}
	switch es.format {
	case format.JSONFormat:
		return true
	default:
		return es.brackets
	}
}

func isTony(es *EncState) bool {
	return es.format == format.TonyFormat
}
