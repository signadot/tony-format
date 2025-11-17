package encode

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/tony/format"
	"github.com/signadot/tony-format/tony/parse"
)

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
