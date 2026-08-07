package encode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A "|+" block keeps a trailing newline that the encoder writes itself, leaving
// the cursor at column 0 with the line's indent unwritten. writeNL used to skip
// entirely in that state, so the key after such a block came out at column 0 --
// either making the document unreadable or, when it happened to parse, silently
// reparenting the key out of the object it belonged to.
//
// Every case here is a nested block followed by a sibling, and every one asserts
// through parse: the string survives byte-exact and the sibling is still where it
// was put. CRLF is the shape that found this (GitHub's web UI submits it), but it
// is not about carriage returns -- any content selecting "|+" does it.
func TestBlockLiteralKeepsSiblingIndent(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"crlf", "a\r\nb\r\n"},
		{"crlf single line", "a\r\n"},
		{"lf trailing blank line", "a\nb\n\n"},
		{"lf trailing space", "a\nb \n"},
		{"lf plain", "a\nb\n"},
		{"lf no trailing newline", "a\nb"},
		{"blank lines within", "a\n\nb\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := ir.FromKeyVals([]ir.KeyVal{
				{Key: ir.FromString("payload"), Val: ir.FromKeyVals([]ir.KeyVal{
					{Key: ir.FromString("body"), Val: ir.FromString(tc.body)},
					{Key: ir.FromString("id"), Val: ir.FromInt(2)},
				})},
				{Key: ir.FromString("ref"), Val: ir.FromString("x/y#1")},
			})

			var buf bytes.Buffer
			if err := Encode(node, &buf, EncodeFormat(format.TonyFormat)); err != nil {
				t.Fatalf("encode: %v", err)
			}
			out := buf.String()

			got, err := parse.Parse([]byte(out))
			if err != nil {
				t.Fatalf("encoded document does not parse: %v\noutput: %q", err, out)
			}

			// GetPath reports a missing path as a nil node with a nil error, so
			// both have to be checked or a lost key arrives as a panic.
			body, err := got.GetPath("$.payload.body")
			if err != nil || body == nil {
				t.Fatalf("payload.body not found: %v\noutput: %q", err, out)
			}
			if body.String != tc.body {
				t.Errorf("body: got %q, want %q\noutput: %q", body.String, tc.body, out)
			}

			// The sibling after the block is the one that used to escape.
			id, err := got.GetPath("$.payload.id")
			if err != nil || id == nil {
				t.Fatalf("payload.id not found -- the key escaped its parent: %v\noutput: %q", err, out)
			}
			if id.Int64 == nil || *id.Int64 != 2 {
				t.Errorf("payload.id: got %v, want 2\noutput: %q", id.Int64, out)
			}
		})
	}
}

func TestMultilineStringWithLineComments(t *testing.T) {
	// Test parsing and encoding a multiline string with line comments
	// that will be encoded as a multiline string (strings.Join(node.Lines, "") == node.String)
	// Format: multiple quoted strings on separate lines, each with optional line comments
	input := `"line1"  # comment for line 1
"line2"  # comment for line 2
"line3"`

	node, err := parse.Parse([]byte(input), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	// Verify it's a multiline string
	if node.Type.String() != "String" {
		t.Fatalf("Expected string type, got %s", node.Type.String())
	}
	if len(node.Lines) == 0 {
		t.Fatal("Expected multiline string with Lines populated")
	}
	// For multiline strings, Join(Lines, "") should equal String (no newlines between lines)
	expectedString := strings.Join(node.Lines, "")
	if expectedString != node.String {
		t.Fatalf("Expected multiline string where Join(Lines, \"\") == String, but Join=%q, String=%q",
			expectedString, node.String)
	}

	// Verify line comments are present
	if node.Comment == nil {
		t.Fatal("Expected line comments to be present")
	}
	if len(node.Comment.Lines) == 0 {
		t.Fatal("Expected Comment.Lines to be populated")
	}
	// Comment.Lines should have one entry per string line (may be empty string if no comment)
	if len(node.Comment.Lines) != len(node.Lines) {
		t.Fatalf("Expected %d comment lines (one per string line), got %d",
			len(node.Lines), len(node.Comment.Lines))
	}

	// Encode it
	var buf bytes.Buffer
	err = Encode(node, &buf,
		EncodeFormat(format.TonyFormat),
		EncodeComments(true),
	)
	if err != nil {
		t.Fatalf("Failed to encode: %v", err)
	}

	output := buf.String()
	t.Logf("Input:\n%s\n", input)
	t.Logf("Parsed - Lines: %v, Comment.Lines: %v\n", node.Lines, node.Comment.Lines)
	t.Logf("Output:\n%s\n", output)

	// Verify the encoded output contains the line comments
	// Each line should have its comment after it (for non-empty comments)
	outputLines := strings.Split(strings.TrimSpace(output), "\n")
	commentCount := 0
	for _, line := range outputLines {
		if strings.Contains(line, "#") {
			commentCount++
		}
	}
	if commentCount == 0 {
		t.Error("Expected encoded output to contain line comments, but found none")
		t.Logf("Output was: %q", output)
	}

	// Verify each non-empty comment line appears in the output
	for i, commentLine := range node.Comment.Lines {
		if commentLine != "" {
			// The comment line includes leading whitespace, so check for the comment text
			commentText := strings.TrimSpace(commentLine)
			if !strings.Contains(output, commentText) {
				t.Errorf("Comment line %d (%q) not found in output", i, commentText)
			}
		}
	}

	// Verify the output is a multiline string format (should use |- or similar)
	// and contains the string lines with their comments
	if !strings.Contains(output, `"line1"`) {
		t.Error("Output should contain first line")
	}
	if !strings.Contains(output, `"line2"`) {
		t.Error("Output should contain second line")
	}
	if !strings.Contains(output, `"line3"`) {
		t.Error("Output should contain third line")
	}
}
