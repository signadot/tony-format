package stream

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestNewEncoder(t *testing.T) {
	// Test: Requires bracketing option
	_, err := NewEncoder(&bytes.Buffer{})
	if err == nil {
		t.Error("expected error when bracketing not specified")
	}

	// Test: WithBrackets works
	enc, err := NewEncoder(&bytes.Buffer{}, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc == nil {
		t.Error("expected encoder")
	}

	// Test: WithWire works
	enc, err = NewEncoder(&bytes.Buffer{}, WithWire())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc == nil {
		t.Error("expected encoder")
	}
}

func TestEncoderBasic_EmptyObject(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	expected := "{}"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestEncoderBasic_EmptyArray(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	expected := "[]"
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestEncoderBasic_SimpleObject(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteKey("name"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	expected := `{name: "value"}`
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestEncoderBasic_SimpleArray(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("c"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	expected := `["a","b","c"]`
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestEncoder_ValueTypes(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.WriteKey("str"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.WriteKey("int"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteInt(42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.WriteKey("float"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteFloat(3.14); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.WriteKey("bool"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteBool(true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.WriteKey("null"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteNull(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should contain all the values
	if !contains(output, `"hello"`) {
		t.Errorf("output should contain 'hello': %q", output)
	}
	if !contains(output, "42") {
		t.Errorf("output should contain '42': %q", output)
	}
	if !contains(output, "3.14") {
		t.Errorf("output should contain '3.14': %q", output)
	}
	if !contains(output, "true") {
		t.Errorf("output should contain 'true': %q", output)
	}
	if !contains(output, "null") {
		t.Errorf("output should contain 'null': %q", output)
	}
}

func TestEncoder_NestedObject(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteKey("a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteKey("b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteString("value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	expected := `{a: {b: "value"}}`
	if output != expected {
		t.Errorf("expected %q, got %q", expected, output)
	}
}

func TestEncoder_StateTracking(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", enc.Depth())
	}
	if enc.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", enc.CurrentPath())
	}

	if err := enc.WriteKey("a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", enc.CurrentPath())
	}
	if enc.CurrentKey() != "a" {
		t.Errorf("expected current key 'a', got %q", enc.CurrentKey())
	}

	if err := enc.BeginArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", enc.Depth())
	}
	if !enc.IsInArray() {
		t.Error("expected to be in array")
	}

	if err := enc.WriteInt(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.CurrentPath() != "a[0]" {
		t.Errorf("expected path 'a[0]', got %q", enc.CurrentPath())
	}
	if enc.CurrentIndex() != 1 {
		t.Errorf("expected current index 1, got %d", enc.CurrentIndex())
	}

	if err := enc.WriteInt(2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.CurrentPath() != "a[1]" {
		t.Errorf("expected path 'a[1]', got %q", enc.CurrentPath())
	}

	if err := enc.EndArray(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", enc.Depth())
	}
	if !enc.IsInObject() {
		t.Error("expected to be in object")
	}

	if err := enc.WriteKey("b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.CurrentPath() != "b" {
		t.Errorf("expected path 'b', got %q", enc.CurrentPath())
	}

	if err := enc.WriteString("value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if enc.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", enc.Depth())
	}
	if enc.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", enc.CurrentPath())
	}
}

func TestEncoderComments(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Phase 1: Comment methods are no-ops
	if err := enc.WriteHeadComment([]string{"# comment"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteLineComment([]string{"# comment"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should write nothing (comments are skipped in Phase 1)
	output := buf.String()
	if output != "" {
		t.Errorf("expected empty output (comments skipped), got %q", output)
	}
}

func TestEncoder_OffsetTracking(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	initialOffset := enc.Offset()
	if initialOffset != 0 {
		t.Errorf("expected initial offset 0, got %d", initialOffset)
	}

	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	offset1 := enc.Offset()
	if offset1 <= 0 {
		t.Errorf("expected offset > 0 after BeginObject, got %d", offset1)
	}

	if err := enc.WriteKey("key"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	offset2 := enc.Offset()
	if offset2 <= offset1 {
		t.Errorf("expected offset to increase, got %d <= %d", offset2, offset1)
	}

	if err := enc.WriteString("value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	offset3 := enc.Offset()
	if offset3 <= offset2 {
		t.Errorf("expected offset to increase, got %d <= %d", offset3, offset2)
	}

	// Final offset should match output length
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	finalOffset := enc.Offset()
	outputLen := int64(buf.Len())
	if finalOffset != outputLen {
		t.Errorf("expected final offset %d to match output length %d", finalOffset, outputLen)
	}
}

func TestEncoder_Reset(t *testing.T) {
	var buf1, buf2 bytes.Buffer

	enc, err := NewEncoder(&buf1, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write first document
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteKey("a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteInt(1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Reset to new writer
	if err := enc.Reset(&buf2, WithBrackets()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write second document
	if err := enc.BeginObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteKey("b"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.WriteInt(2); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := enc.EndObject(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify outputs
	output1 := buf1.String()
	expected1 := `{a: 1}`
	if output1 != expected1 {
		t.Errorf("expected %q, got %q", expected1, output1)
	}

	output2 := buf2.String()
	expected2 := `{b: 2}`
	if output2 != expected2 {
		t.Errorf("expected %q, got %q", expected2, output2)
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestEncoderDecoder_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		write func(*Encoder) error
		check func(*testing.T, []Event)
	}{
		{
			name: "simple_object",
			write: func(enc *Encoder) error {
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("name"); err != nil {
					return err
				}
				if err := enc.WriteString("value"); err != nil {
					return err
				}
				return enc.EndObject()
			},
			check: func(t *testing.T, events []Event) {
				if len(events) != 4 {
					t.Fatalf("expected 4 events, got %d", len(events))
				}
				if events[0].Type != EventBeginObject {
					t.Errorf("event 0: expected EventBeginObject, got %v", events[0].Type)
				}
				if events[1].Type != EventKey || events[1].Key != "name" {
					t.Errorf("event 1: expected EventKey 'name', got %v %q", events[1].Type, events[1].Key)
				}
				if events[2].Type != EventString || events[2].String != "value" {
					t.Errorf("event 2: expected EventString 'value', got %v %q", events[2].Type, events[2].String)
				}
				if events[3].Type != EventEndObject {
					t.Errorf("event 3: expected EventEndObject, got %v", events[3].Type)
				}
			},
		},
		{
			name: "nested_object",
			write: func(enc *Encoder) error {
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("a"); err != nil {
					return err
				}
				if err := enc.BeginObject(); err != nil {
					return err
				}
				if err := enc.WriteKey("b"); err != nil {
					return err
				}
				if err := enc.WriteString("value"); err != nil {
					return err
				}
				if err := enc.EndObject(); err != nil {
					return err
				}
				return enc.EndObject()
			},
			check: func(t *testing.T, events []Event) {
				if len(events) != 7 {
					t.Fatalf("expected 7 events, got %d", len(events))
				}
			},
		},
		{
			name: "array_with_values",
			write: func(enc *Encoder) error {
				if err := enc.BeginArray(); err != nil {
					return err
				}
				if err := enc.WriteInt(1); err != nil {
					return err
				}
				if err := enc.WriteInt(2); err != nil {
					return err
				}
				if err := enc.WriteInt(3); err != nil {
					return err
				}
				return enc.EndArray()
			},
			check: func(t *testing.T, events []Event) {
				if len(events) != 5 {
					t.Fatalf("expected 5 events, got %d", len(events))
				}
				if events[1].Type != EventInt || events[1].Int != 1 {
					t.Errorf("event 1: expected EventInt 1, got %v %d", events[1].Type, events[1].Int)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			var buf bytes.Buffer
			enc, err := NewEncoder(&buf, WithBrackets())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := tt.write(enc); err != nil {
				t.Fatalf("unexpected error encoding: %v", err)
			}

			// Decode
			dec, err := NewDecoder(bytes.NewReader(buf.Bytes()), WithBrackets())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			events := []Event{}
			for {
				event, err := dec.ReadEvent()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error decoding: %v", err)
				}
				events = append(events, event)
			}

			// Verify
			tt.check(t, events)
		})
	}
}
