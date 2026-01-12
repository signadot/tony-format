package encode

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/token"
)

type EncState struct {
	line, col     int
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

func Encode(node *ir.Node, w io.Writer, opts ...EncodeOption) error {
	es := &EncState{
		indent: 2,
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
		es.col = 1
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
	if es.col == 0 {
		return nil
	}
	indentString := strings.Repeat(strings.Repeat(" ", es.indent), es.depth)
	if err := writeString(w, "\n"+indentString); err != nil {
		return err
	}
	es.line++
	es.col = len(indentString)
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
	es.col += len(sep)
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
	tag := ir.TagRemove(node.Tag, "!bracket")
	tag = ir.TagRemove(tag, "!literal")
	if tag == "" {
		return nil
	}
	if err := writeTag(w, tag, es); err != nil {
		return err
	}
	es.col += len(tag)
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

// encodeObject
func encodeObject(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.brackets && ir.TagHas(node.Tag, "!bracket") {
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
		if !skipValue {
			if err := writeObjectFieldPrefix(i, node, w, es); err != nil {
				return err
			}
		}
		skipValue, err = encodeObjectField(yField, val, w, es)
		if err != nil {
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
	return writeObjectClose(w, es, n)
}

func writeObjectOpen(w io.Writer, es *EncState, nFields int) error {
	if !esBracket(es) && nFields != 0 {
		return nil
	}
	open := "{"
	es.col++
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
	es.col++
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
		subEncState.line = 0
		subEncState.col = 0
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
	es.col += len(v)
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
			es.col += 1
		}
		br := false
		if esBracket(es) || ir.TagHas(node.Tag, "!bracket") {
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
		if !esBracket(es) || ir.TagHas(node.Tag, "!bracket") {
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
			es.col += 1
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
			es.col += 1
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
		es.col += 1
	}
	return encode(yVal, w, es)
}

// Array encoding

func encodeArray(node *ir.Node, w io.Writer, es *EncState) error {
	if !es.brackets && ir.TagHas(node.Tag, "!bracket") {
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
		doDepth := !esBracket(es) && !ir.TagHas(v.Tag, "!bracket")
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
	return writeArrayClose(w, es, n)
}

func writeArrayOpen(w io.Writer, es *EncState, nValues int) error {
	if !esBracket(es) && nValues != 0 {
		return nil
	}
	open := "["
	if err := writeString(w, open); err != nil {
		return err
	}
	es.col += 1
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
	es.col++
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
	es.col += 2
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
	es.col += len(startBLit)
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
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return err
		}
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
		es.col += len(ln)
		if i < len(commentLines) {
			if es.comments {
				commentText := commentLines[i]
				if commentText != "" {
					es.col += len(commentText)
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
	es.col += len(v)
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

func encodeNumber(node *ir.Node, w io.Writer, es *EncState) error {
	if node.Int64 != nil {
		v := strconv.FormatInt(*node.Int64, 10)
		v = applyValueColor(es, ir.NumberType, v)
		es.col += len(v)
		if err := writeString(w, v); err != nil {
			return err
		}
	}
	if node.Float64 != nil {
		v := strconv.FormatFloat(*node.Float64, 'f', -1, 64)
		// Ensure zero floats encode as "0.0" not "0"
		if v == "0" || v == "-0" {
			v = "0.0"
		}
		v = applyValueColor(es, ir.NumberType, v)
		es.col += len(v)
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
	es.col += len(v)
	return writeLineCommentLines(w, node.Comment, es)
}

// Null encoding

func encodeNull(node *ir.Node, w io.Writer, es *EncState) error {
	v := "null"
	v = applyValueColor(es, ir.NullType, v)
	if _, err := w.Write([]byte(v)); err != nil {
		return err
	}
	es.col += 4
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
	col := &es.col
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
	*col += len(f) + len(sep)
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
			es.col = len(ln)
			break
		}
		es.col = 1
		if err := writeNL(w, es); err != nil {
			es.col = len(ln)
			return err
		}
		es.col = len(ln)
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
	es.col += len(ln)
	ln = applyValueColor(es, ir.CommentType, ln)
	return writeString(w, ln)
}

func doBlockLit(node *ir.Node, es *EncState) bool {
	if es.wire || es.format.IsJSON() {
		return false
	}
	if es.literal || ir.TagHas(node.Tag, "!literal") {
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
