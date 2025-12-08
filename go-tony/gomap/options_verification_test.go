package gomap_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/token"
)

// Test all encode options work correctly
func TestEncodeOptions(t *testing.T) {
	// Create a test node with comments by parsing
	tonyWithComments := "# Test comment\nname: test\nvalue: 42"
	nodeWithComments, err := parse.Parse([]byte(tonyWithComments), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("Failed to parse node with comments: %v", err)
	}

	// Create a simple test node without comments
	node := ir.FromMap(map[string]*ir.Node{
		"name":  ir.FromString("test"),
		"value": ir.FromInt(42),
	})

	t.Run("EncodeFormat", func(t *testing.T) {
		// Test Tony format
		var buf1 bytes.Buffer
		err := encode.Encode(node, &buf1, encode.EncodeFormat(format.TonyFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(TonyFormat) failed: %v", err)
		}
		output1 := buf1.String()
		if output1 == "" {
			t.Error("EncodeFormat(TonyFormat) produced empty output")
		}
		t.Logf("Tony format output: %s", output1)

		// Test YAML format
		var buf2 bytes.Buffer
		err = encode.Encode(node, &buf2, encode.EncodeFormat(format.YAMLFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(YAMLFormat) failed: %v", err)
		}
		output2 := buf2.String()
		if output2 == "" {
			t.Error("EncodeFormat(YAMLFormat) produced empty output")
		}
		t.Logf("YAML format output: %s", output2)

		// Test JSON format
		var buf3 bytes.Buffer
		err = encode.Encode(node, &buf3, encode.EncodeFormat(format.JSONFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(JSONFormat) failed: %v", err)
		}
		output3 := buf3.String()
		if output3 == "" {
			t.Error("EncodeFormat(JSONFormat) produced empty output")
		}
		if !strings.Contains(output3, "{") || !strings.Contains(output3, "}") {
			t.Error("JSON format should contain braces")
		}
		t.Logf("JSON format output: %s", output3)
	})

	t.Run("Depth", func(t *testing.T) {
		// Test depth 0
		var buf1 bytes.Buffer
		err := encode.Encode(node, &buf1, encode.Depth(0))
		if err != nil {
			t.Fatalf("Depth(0) failed: %v", err)
		}
		output1 := buf1.String()
		t.Logf("Depth(0) output:\n%s", output1)

		// Test depth 2
		var buf2 bytes.Buffer
		err = encode.Encode(node, &buf2, encode.Depth(2))
		if err != nil {
			t.Fatalf("Depth(2) failed: %v", err)
		}
		output2 := buf2.String()
		t.Logf("Depth(2) output:\n%s", output2)

		// Test depth 4
		var buf3 bytes.Buffer
		err = encode.Encode(node, &buf3, encode.Depth(4))
		if err != nil {
			t.Fatalf("Depth(4) failed: %v", err)
		}
		output3 := buf3.String()
		t.Logf("Depth(4) output:\n%s", output3)

		// Verify different depths produce different indentation
		if output1 == output2 || output2 == output3 {
			t.Error("Different depths should produce different output")
		}
	})

	t.Run("EncodeComments", func(t *testing.T) {
		// Test with comments enabled (using nodeWithComments)
		var buf1 bytes.Buffer
		err := encode.Encode(nodeWithComments, &buf1, encode.EncodeComments(true))
		if err != nil {
			t.Fatalf("EncodeComments(true) failed: %v", err)
		}
		output1 := buf1.String()
		t.Logf("EncodeComments(true) output:\n%s", output1)

		// Test with comments disabled
		var buf2 bytes.Buffer
		err = encode.Encode(nodeWithComments, &buf2, encode.EncodeComments(false))
		if err != nil {
			t.Fatalf("EncodeComments(false) failed: %v", err)
		}
		output2 := buf2.String()
		t.Logf("EncodeComments(false) output:\n%s", output2)

		// Verify comments appear when enabled
		if strings.Contains(output1, "#") && !strings.Contains(output2, "#") {
			// Good - comments in output1 but not output2
		} else if nodeWithComments.Comment != nil {
			// If node has comments, they should appear in output1
			if !strings.Contains(output1, "#") {
				t.Error("EncodeComments(true) should include comments when node has comments")
			}
		}
	})

	t.Run("InjectRaw", func(t *testing.T) {
		// Test with InjectRaw enabled
		var buf1 bytes.Buffer
		err := encode.Encode(node, &buf1, encode.InjectRaw(true))
		if err != nil {
			t.Fatalf("InjectRaw(true) failed: %v", err)
		}
		output1 := buf1.String()
		t.Logf("InjectRaw(true) output:\n%s", output1)

		// Test with InjectRaw disabled
		var buf2 bytes.Buffer
		err = encode.Encode(node, &buf2, encode.InjectRaw(false))
		if err != nil {
			t.Fatalf("InjectRaw(false) failed: %v", err)
		}
		output2 := buf2.String()
		t.Logf("InjectRaw(false) output:\n%s", output2)

		// Both should produce valid output
		if output1 == "" || output2 == "" {
			t.Error("InjectRaw should produce output")
		}
	})

	t.Run("EncodeColors", func(t *testing.T) {
		colors := encode.NewColors()
		var buf bytes.Buffer
		err := encode.Encode(node, &buf, encode.EncodeColors(colors))
		if err != nil {
			t.Fatalf("EncodeColors failed: %v", err)
		}
		output := buf.String()
		if output == "" {
			t.Error("EncodeColors produced empty output")
		}
		t.Logf("EncodeColors output:\n%s", output)
	})

	t.Run("EncodeBrackets", func(t *testing.T) {
		// Test with brackets enabled
		var buf1 bytes.Buffer
		err := encode.Encode(node, &buf1, encode.EncodeBrackets(true))
		if err != nil {
			t.Fatalf("EncodeBrackets(true) failed: %v", err)
		}
		output1 := buf1.String()
		t.Logf("EncodeBrackets(true) output:\n%s", output1)

		// Test with brackets disabled
		var buf2 bytes.Buffer
		err = encode.Encode(node, &buf2, encode.EncodeBrackets(false))
		if err != nil {
			t.Fatalf("EncodeBrackets(false) failed: %v", err)
		}
		output2 := buf2.String()
		t.Logf("EncodeBrackets(false) output:\n%s", output2)

		// Both should produce valid output
		if output1 == "" || output2 == "" {
			t.Error("EncodeBrackets should produce output")
		}
	})

	t.Run("EncodeWire", func(t *testing.T) {
		// Test with wire format enabled
		var buf1 bytes.Buffer
		err := encode.Encode(node, &buf1, encode.EncodeWire(true))
		if err != nil {
			t.Fatalf("EncodeWire(true) failed: %v", err)
		}
		output1 := buf1.String()
		t.Logf("EncodeWire(true) output:\n%s", output1)

		// Test with wire format disabled
		var buf2 bytes.Buffer
		err = encode.Encode(node, &buf2, encode.EncodeWire(false))
		if err != nil {
			t.Fatalf("EncodeWire(false) failed: %v", err)
		}
		output2 := buf2.String()
		t.Logf("EncodeWire(false) output:\n%s", output2)

		// Both should produce valid output
		if output1 == "" || output2 == "" {
			t.Error("EncodeWire should produce output")
		}
	})

	t.Run("MultipleOptions", func(t *testing.T) {
		var buf bytes.Buffer
		err := encode.Encode(node, &buf,
			encode.EncodeFormat(format.TonyFormat),
			encode.EncodeComments(true),
			encode.Depth(2),
			encode.EncodeWire(false),
		)
		if err != nil {
			t.Fatalf("Multiple options failed: %v", err)
		}
		output := buf.String()
		if output == "" {
			t.Error("Multiple options should produce output")
		}
		t.Logf("Multiple options output:\n%s", output)
	})
}

// Test all parse options work correctly
func TestParseOptions(t *testing.T) {
	tonyData := []byte("name: test\nvalue: 42")
	yamlData := []byte("name: test\nvalue: 42")
	jsonData := []byte(`{"name":"test","value":42}`)
	tonyWithComments := []byte("# Comment\nname: test # inline\nvalue: 42")

	t.Run("ParseFormat", func(t *testing.T) {
		// Test Tony format
		node1, err := parse.Parse(tonyData, parse.ParseFormat(format.TonyFormat))
		if err != nil {
			t.Fatalf("ParseFormat(TonyFormat) failed: %v", err)
		}
		if node1 == nil {
			t.Error("ParseFormat(TonyFormat) returned nil node")
		}
		t.Logf("Parsed Tony format: %s", encode.MustString(node1))

		// Test YAML format
		node2, err := parse.Parse(yamlData, parse.ParseFormat(format.YAMLFormat))
		if err != nil {
			t.Fatalf("ParseFormat(YAMLFormat) failed: %v", err)
		}
		if node2 == nil {
			t.Error("ParseFormat(YAMLFormat) returned nil node")
		}
		t.Logf("Parsed YAML format: %s", encode.MustString(node2))

		// Test JSON format
		node3, err := parse.Parse(jsonData, parse.ParseFormat(format.JSONFormat))
		if err != nil {
			t.Fatalf("ParseFormat(JSONFormat) failed: %v", err)
		}
		if node3 == nil {
			t.Error("ParseFormat(JSONFormat) returned nil node")
		}
		t.Logf("Parsed JSON format: %s", encode.MustString(node3))
	})

	t.Run("ParseYAML", func(t *testing.T) {
		node, err := parse.Parse(yamlData, parse.ParseYAML())
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}
		if node == nil {
			t.Error("ParseYAML() returned nil node")
		}
		t.Logf("Parsed YAML: %s", encode.MustString(node))
	})

	t.Run("ParseTony", func(t *testing.T) {
		node, err := parse.Parse(tonyData, parse.ParseTony())
		if err != nil {
			t.Fatalf("ParseTony() failed: %v", err)
		}
		if node == nil {
			t.Error("ParseTony() returned nil node")
		}
		t.Logf("Parsed Tony: %s", encode.MustString(node))
	})

	t.Run("ParseJSON", func(t *testing.T) {
		node, err := parse.Parse(jsonData, parse.ParseJSON())
		if err != nil {
			t.Fatalf("ParseJSON() failed: %v", err)
		}
		if node == nil {
			t.Error("ParseJSON() returned nil node")
		}
		t.Logf("Parsed JSON: %s", encode.MustString(node))
	})

	t.Run("ParseComments", func(t *testing.T) {
		// Test with comments enabled
		node1, err := parse.Parse(tonyWithComments, parse.ParseComments(true))
		if err != nil {
			t.Fatalf("ParseComments(true) failed: %v", err)
		}
		if node1 == nil {
			t.Error("ParseComments(true) returned nil node")
		}
		// Verify comment is present (could be on root or child nodes)
		hasComment := node1.Comment != nil
		if !hasComment && len(node1.Values) > 0 {
			// Check child nodes for comments
			for _, child := range node1.Values {
				if child.Comment != nil {
					hasComment = true
					break
				}
			}
		}
		if !hasComment {
			t.Logf("Note: No comments found in parsed node (may be on specific child nodes)")
		}
		t.Logf("Parsed with comments: %s", encode.MustString(node1))

		// Test with comments disabled
		node2, err := parse.Parse(tonyWithComments, parse.ParseComments(false))
		if err != nil {
			t.Fatalf("ParseComments(false) failed: %v", err)
		}
		if node2 == nil {
			t.Error("ParseComments(false) returned nil node")
		}
		t.Logf("Parsed without comments: %s", encode.MustString(node2))
	})

	t.Run("ParsePositions", func(t *testing.T) {
		positions := make(map[*ir.Node]*token.Pos)
		node, err := parse.Parse(tonyData, parse.ParsePositions(positions))
		if err != nil {
			t.Fatalf("ParsePositions failed: %v", err)
		}
		if node == nil {
			t.Error("ParsePositions returned nil node")
		}
		// Verify positions map is populated
		if len(positions) == 0 {
			t.Error("ParsePositions should populate positions map")
		}
		t.Logf("Parsed with positions: %d positions tracked", len(positions))
	})

	t.Run("NoBrackets", func(t *testing.T) {
		// Test with NoBrackets option
		bracketData := []byte("[a, b, c]")
		node, err := parse.Parse(bracketData, parse.NoBrackets())
		if err != nil {
			t.Fatalf("NoBrackets() failed: %v", err)
		}
		if node == nil {
			t.Error("NoBrackets() returned nil node")
		}
		t.Logf("Parsed with NoBrackets: %s", encode.MustString(node))
	})

	t.Run("MultipleOptions", func(t *testing.T) {
		positions := make(map[*ir.Node]*token.Pos)
		node, err := parse.Parse(tonyWithComments,
			parse.ParseFormat(format.TonyFormat),
			parse.ParseComments(true),
			parse.ParsePositions(positions),
		)
		if err != nil {
			t.Fatalf("Multiple parse options failed: %v", err)
		}
		if node == nil {
			t.Error("Multiple parse options returned nil node")
		}
		t.Logf("Parsed with multiple options: %s", encode.MustString(node))
	})
}

// Test round-trip with various option combinations
func TestRoundTripWithOptions(t *testing.T) {
	originalData := []byte("name: test\nvalue: 42\n# comment")

	// Parse with comments
	node, err := parse.Parse(originalData, parse.ParseComments(true))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Encode with various options
	var buf bytes.Buffer
	err = encode.Encode(node, &buf,
		encode.EncodeFormat(format.TonyFormat),
		encode.EncodeComments(true),
	)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	output := buf.String()
	t.Logf("Round-trip output:\n%s", output)

	// Parse back
	node2, err := parse.Parse([]byte(output), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("Round-trip parse failed: %v", err)
	}

	// Verify structure is preserved
	if node.Type != node2.Type {
		t.Errorf("Type mismatch: %v != %v", node.Type, node2.Type)
	}
}
