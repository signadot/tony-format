package gomap_test

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/token"
)

func TestMapOptions(t *testing.T) {
	type TestStruct struct {
		Name  string `tony:"field=name"`
		Value int    `tony:"field=value"`
	}
	s := TestStruct{Name: "test", Value: 42}

	t.Run("EncodeFormat", func(t *testing.T) {
		// Test Tony format
		bytes1, err := gomap.ToTony(&s, gomap.EncodeFormat(format.TonyFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(TonyFormat) failed: %v", err)
		}
		if len(bytes1) == 0 {
			t.Error("EncodeFormat(TonyFormat) produced empty output")
		}

		// Test YAML format
		bytes2, err := gomap.ToTony(&s, gomap.EncodeFormat(format.YAMLFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(YAMLFormat) failed: %v", err)
		}
		if len(bytes2) == 0 {
			t.Error("EncodeFormat(YAMLFormat) produced empty output")
		}

		// Test JSON format
		bytes3, err := gomap.ToTony(&s, gomap.EncodeFormat(format.JSONFormat))
		if err != nil {
			t.Fatalf("EncodeFormat(JSONFormat) failed: %v", err)
		}
		if !strings.Contains(string(bytes3), "{") || !strings.Contains(string(bytes3), "}") {
			t.Error("JSON format should contain braces")
		}
	})

	t.Run("Depth", func(t *testing.T) {
		// Test depth 0
		bytes1, err := gomap.ToTony(&s, gomap.Depth(0))
		if err != nil {
			t.Fatalf("Depth(0) failed: %v", err)
		}
		output1 := string(bytes1)

		// Test depth 2
		bytes2, err := gomap.ToTony(&s, gomap.Depth(2))
		if err != nil {
			t.Fatalf("Depth(2) failed: %v", err)
		}
		output2 := string(bytes2)

		// Verify different depths produce different output
		if output1 == output2 {
			t.Error("Different depths should produce different output")
		}
	})

	t.Run("EncodeComments", func(t *testing.T) {
		// Create IR node with comments by parsing
		tonyWithComments := "# Test comment\nname: test\nvalue: 42"
		nodeWithComments, err := parse.Parse([]byte(tonyWithComments), parse.ParseComments(true))
		if err != nil {
			t.Fatalf("Failed to parse node with comments: %v", err)
		}

		// Convert to struct
		var s1 TestStruct
		err = gomap.FromTonyIR(nodeWithComments, &s1)
		if err != nil {
			t.Fatalf("FromTonyIR failed: %v", err)
		}

		// Test with comments enabled
		bytes1, err := gomap.ToTony(&s1, gomap.EncodeComments(true))
		if err != nil {
			t.Fatalf("EncodeComments(true) failed: %v", err)
		}
		output1 := string(bytes1)

		// Test with comments disabled
		bytes2, err := gomap.ToTony(&s1, gomap.EncodeComments(false))
		if err != nil {
			t.Fatalf("EncodeComments(false) failed: %v", err)
		}
		output2 := string(bytes2)

		// Both should produce valid output
		if len(output1) == 0 || len(output2) == 0 {
			t.Error("EncodeComments should produce output")
		}
	})

	t.Run("InjectRaw", func(t *testing.T) {
		bytes1, err := gomap.ToTony(&s, gomap.InjectRaw(true))
		if err != nil {
			t.Fatalf("InjectRaw(true) failed: %v", err)
		}
		if len(bytes1) == 0 {
			t.Error("InjectRaw(true) produced empty output")
		}

		bytes2, err := gomap.ToTony(&s, gomap.InjectRaw(false))
		if err != nil {
			t.Fatalf("InjectRaw(false) failed: %v", err)
		}
		if len(bytes2) == 0 {
			t.Error("InjectRaw(false) produced empty output")
		}
	})

	t.Run("EncodeColors", func(t *testing.T) {
		colors := encode.NewColors()
		bytes, err := gomap.ToTony(&s, gomap.EncodeColors(colors))
		if err != nil {
			t.Fatalf("EncodeColors failed: %v", err)
		}
		if len(bytes) == 0 {
			t.Error("EncodeColors produced empty output")
		}
	})

	t.Run("EncodeBrackets", func(t *testing.T) {
		bytes1, err := gomap.ToTony(&s, gomap.EncodeBrackets(true))
		if err != nil {
			t.Fatalf("EncodeBrackets(true) failed: %v", err)
		}
		output1 := string(bytes1)
		if !strings.Contains(output1, "{") || !strings.Contains(output1, "}") {
			t.Error("EncodeBrackets(true) should include brackets")
		}

		bytes2, err := gomap.ToTony(&s, gomap.EncodeBrackets(false))
		if err != nil {
			t.Fatalf("EncodeBrackets(false) failed: %v", err)
		}
		output2 := string(bytes2)
		if strings.Contains(output2, "{") && strings.Contains(output2, "}") {
			t.Error("EncodeBrackets(false) should not include brackets")
		}
	})

	t.Run("EncodeWire", func(t *testing.T) {
		bytes1, err := gomap.ToTony(&s, gomap.EncodeWire(true))
		if err != nil {
			t.Fatalf("EncodeWire(true) failed: %v", err)
		}
		output1 := string(bytes1)
		// Wire format should be compact (no newlines typically)
		if strings.Count(output1, "\n") > 2 {
			t.Logf("Wire format output: %s", output1)
		}

		bytes2, err := gomap.ToTony(&s, gomap.EncodeWire(false))
		if err != nil {
			t.Fatalf("EncodeWire(false) failed: %v", err)
		}
		output2 := string(bytes2)
		// Pretty format should have more structure
		if len(output2) == 0 {
			t.Error("EncodeWire(false) produced empty output")
		}
	})

		t.Run("MultipleOptions", func(t *testing.T) {
		bytes, err := gomap.ToTony(&s,
			gomap.EncodeFormat(format.TonyFormat),
			gomap.EncodeComments(true),
			gomap.Depth(2),
			gomap.EncodeWire(false),
		)
		if err != nil {
			t.Fatalf("Multiple options failed: %v", err)
		}
		if len(bytes) == 0 {
			t.Error("Multiple options should produce output")
		}
	})
}

func TestUnmapOptions(t *testing.T) {
	type TestStruct struct {
		Name  string `tony:"field=name"`
		Value int    `tony:"field=value"`
	}
	tonyData := []byte("name: test\nvalue: 42")
	yamlData := []byte("name: test\nvalue: 42")
	jsonData := []byte(`{"name":"test","value":42}`)
	tonyWithComments := []byte("# Comment\nname: test # inline\nvalue: 42")

	t.Run("ParseFormat", func(t *testing.T) {
		// Test Tony format
		var s1 TestStruct
		err := gomap.FromTony(tonyData, &s1, gomap.ParseFormat(format.TonyFormat))
		if err != nil {
			t.Fatalf("ParseFormat(TonyFormat) failed: %v", err)
		}
		if s1.Name != "test" || s1.Value != 42 {
			t.Errorf("ParseFormat(TonyFormat) incorrect values: %+v", s1)
		}

		// Test YAML format
		var s2 TestStruct
		err = gomap.FromTony(yamlData, &s2, gomap.ParseFormat(format.YAMLFormat))
		if err != nil {
			t.Fatalf("ParseFormat(YAMLFormat) failed: %v", err)
		}
		if s2.Name != "test" || s2.Value != 42 {
			t.Errorf("ParseFormat(YAMLFormat) incorrect values: %+v", s2)
		}

		// Test JSON format
		var s3 TestStruct
		err = gomap.FromTony(jsonData, &s3, gomap.ParseFormat(format.JSONFormat))
		if err != nil {
			t.Fatalf("ParseFormat(JSONFormat) failed: %v", err)
		}
		if s3.Name != "test" || s3.Value != 42 {
			t.Errorf("ParseFormat(JSONFormat) incorrect values: %+v", s3)
		}
	})

	t.Run("ParseYAML", func(t *testing.T) {
		var s TestStruct
		err := gomap.FromTony(yamlData, &s, gomap.ParseYAML())
		if err != nil {
			t.Fatalf("ParseYAML() failed: %v", err)
		}
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("ParseYAML() incorrect values: %+v", s)
		}
	})

	t.Run("ParseTony", func(t *testing.T) {
		var s TestStruct
		err := gomap.FromTony(tonyData, &s, gomap.ParseTony())
		if err != nil {
			t.Fatalf("ParseTony() failed: %v", err)
		}
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("ParseTony() incorrect values: %+v", s)
		}
	})

	t.Run("ParseJSON", func(t *testing.T) {
		var s TestStruct
		err := gomap.FromTony(jsonData, &s, gomap.ParseJSON())
		if err != nil {
			t.Fatalf("ParseJSON() failed: %v", err)
		}
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("ParseJSON() incorrect values: %+v", s)
		}
	})

	t.Run("ParseComments", func(t *testing.T) {
		// Test with comments enabled
		var s1 TestStruct
		err := gomap.FromTony(tonyWithComments, &s1, gomap.ParseComments(true))
		if err != nil {
			t.Fatalf("ParseComments(true) failed: %v", err)
		}
		if s1.Name != "test" || s1.Value != 42 {
			t.Errorf("ParseComments(true) incorrect values: %+v", s1)
		}

		// Test with comments disabled
		var s2 TestStruct
		err = gomap.FromTony(tonyWithComments, &s2, gomap.ParseComments(false))
		if err != nil {
			t.Fatalf("ParseComments(false) failed: %v", err)
		}
		if s2.Name != "test" || s2.Value != 42 {
			t.Errorf("ParseComments(false) incorrect values: %+v", s2)
		}
	})

	t.Run("ParsePositions", func(t *testing.T) {
		positions := make(map[*ir.Node]*token.Pos)
		var s TestStruct
		err := gomap.FromTony(tonyData, &s, gomap.ParsePositions(positions))
		if err != nil {
			t.Fatalf("ParsePositions failed: %v", err)
		}
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("ParsePositions incorrect values: %+v", s)
		}
		// Verify positions map is populated
		if len(positions) == 0 {
			t.Error("ParsePositions should populate positions map")
		}
	})

	t.Run("NoBrackets", func(t *testing.T) {
		bracketData := []byte("[a, b, c]")
		var result []string
		err := gomap.FromTony(bracketData, &result, gomap.NoBrackets())
		if err != nil {
			t.Fatalf("NoBrackets() failed: %v", err)
		}
		if len(result) == 0 {
			t.Error("NoBrackets() should parse array")
		}
	})

	t.Run("MultipleOptions", func(t *testing.T) {
		positions := make(map[*ir.Node]*token.Pos)
		var s TestStruct
		err := gomap.FromTony(tonyWithComments, &s,
			gomap.ParseFormat(format.TonyFormat),
			gomap.ParseComments(true),
			gomap.ParsePositions(positions),
		)
		if err != nil {
			t.Fatalf("Multiple parse options failed: %v", err)
		}
		if s.Name != "test" || s.Value != 42 {
			t.Errorf("Multiple parse options incorrect values: %+v", s)
		}
		if len(positions) == 0 {
			t.Error("Multiple parse options should populate positions")
		}
	})
}

func TestRoundTripWithGomapOptions(t *testing.T) {
	type TestStruct struct {
		Name  string `tony:"field=name"`
		Value int    `tony:"field=value"`
	}
	s := TestStruct{Name: "test", Value: 42}

	// Round-trip with encode options
	bytes, err := gomap.ToTony(&s,
		gomap.EncodeFormat(format.TonyFormat),
		gomap.EncodeComments(true),
	)
	if err != nil {
		t.Fatalf("ToTony with options failed: %v", err)
	}

	// Parse back with parse options
	var s2 TestStruct
	err = gomap.FromTony(bytes, &s2,
		gomap.ParseTony(),
		gomap.ParseComments(true),
	)
	if err != nil {
		t.Fatalf("FromTony with options failed: %v", err)
	}

	// Verify round-trip
	if s.Name != s2.Name || s.Value != s2.Value {
		t.Errorf("Round-trip failed: original=%+v, result=%+v", s, s2)
	}
}
