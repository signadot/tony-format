package stream

import (
	"bytes"
	"io"
	"testing"
)

func TestNewDecoder(t *testing.T) {
	// Test: Requires bracketing option
	_, err := NewDecoder(&bytes.Buffer{})
	if err == nil {
		t.Error("expected error when bracketing not specified")
	}

	// Test: WithBrackets works
	dec, err := NewDecoder(&bytes.Buffer{}, WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec == nil {
		t.Error("expected decoder")
	}

	// Test: WithWire works
	dec, err = NewDecoder(&bytes.Buffer{}, WithWire())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dec == nil {
		t.Error("expected decoder")
	}
}

func TestDecoderBasic_EmptyObject(t *testing.T) {
	data := []byte(`{}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event, err := dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginObject {
		t.Errorf("expected EventBeginObject, got %v", event.Type)
	}
	if dec.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", dec.Depth())
	}

	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventEndObject {
		t.Errorf("expected EventEndObject, got %v", event.Type)
	}
	if dec.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", dec.Depth())
	}

	// Should be EOF now
	_, err = dec.ReadEvent()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestDecoderBasic_EmptyArray(t *testing.T) {
	data := []byte(`[]`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event, err := dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginArray {
		t.Errorf("expected EventBeginArray, got %v", event.Type)
	}

	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventEndArray {
		t.Errorf("expected EventEndArray, got %v", event.Type)
	}

	_, err = dec.ReadEvent()
	if err != io.EOF {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestDecoderBasic_SimpleObject(t *testing.T) {
	data := []byte(`{"name":"value"}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events (BeginObject, Key, String, EndObject), got %d", len(events))
	}

	if events[0].Type != EventBeginObject {
		t.Errorf("event 0: expected EventBeginObject, got %v", events[0].Type)
	}
	if events[1].Type != EventKey {
		t.Errorf("event 1: expected EventKey, got %v", events[1].Type)
	}
	if events[1].Key != "name" {
		t.Errorf("event 1: expected key 'name', got %q", events[1].Key)
	}
	if events[2].Type != EventString {
		t.Errorf("event 2: expected EventString, got %v", events[2].Type)
	}
	if events[2].String != "value" {
		t.Errorf("event 2: expected string 'value', got %q", events[2].String)
	}
	if events[3].Type != EventEndObject {
		t.Errorf("event 3: expected EventEndObject, got %v", events[3].Type)
	}
}

func TestDecoderBasic_SimpleArray(t *testing.T) {
	data := []byte(`["a","b","c"]`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	if events[0].Type != EventBeginArray {
		t.Errorf("event 0: expected EventBeginArray, got %v", events[0].Type)
	}
	if events[1].Type != EventString || events[1].String != "a" {
		t.Errorf("event 1: expected EventString 'a', got %v %q", events[1].Type, events[1].String)
	}
	if events[2].Type != EventString || events[2].String != "b" {
		t.Errorf("event 2: expected EventString 'b', got %v %q", events[2].Type, events[2].String)
	}
	if events[3].Type != EventString || events[3].String != "c" {
		t.Errorf("event 3: expected EventString 'c', got %v %q", events[3].Type, events[3].String)
	}
	if events[4].Type != EventEndArray {
		t.Errorf("event 4: expected EventEndArray, got %v", events[4].Type)
	}
}

func TestDecoder_ValueTypes(t *testing.T) {
	data := []byte(`{"str":"hello","int":42,"float":3.14,"bool":true,"null":null}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	// Should have: BeginObject, Key("str"), String("hello"), Key("int"), Int(42),
	// Key("float"), Float(3.14), Key("bool"), Bool(true), Key("null"), Null(), EndObject
	if len(events) < 12 {
		t.Fatalf("expected at least 12 events, got %d", len(events))
	}

	// Find and verify each value type
	foundStr := false
	foundInt := false
	foundFloat := false
	foundBool := false
	foundNull := false

	for i, event := range events {
		if event.Type == EventKey && event.Key == "str" {
			if i+1 < len(events) && events[i+1].Type == EventString && events[i+1].String == "hello" {
				foundStr = true
			}
		}
		if event.Type == EventKey && event.Key == "int" {
			if i+1 < len(events) && events[i+1].Type == EventInt && events[i+1].Int == 42 {
				foundInt = true
			}
		}
		if event.Type == EventKey && event.Key == "float" {
			if i+1 < len(events) && events[i+1].Type == EventFloat && events[i+1].Float == 3.14 {
				foundFloat = true
			}
		}
		if event.Type == EventKey && event.Key == "bool" {
			if i+1 < len(events) && events[i+1].Type == EventBool && events[i+1].Bool == true {
				foundBool = true
			}
		}
		if event.Type == EventKey && event.Key == "null" {
			if i+1 < len(events) && events[i+1].Type == EventNull {
				foundNull = true
			}
		}
	}

	if !foundStr {
		t.Error("did not find string value")
	}
	if !foundInt {
		t.Error("did not find int value")
	}
	if !foundFloat {
		t.Error("did not find float value")
	}
	if !foundBool {
		t.Error("did not find bool value")
	}
	if !foundNull {
		t.Error("did not find null value")
	}
}

func TestDecoder_NestedObject(t *testing.T) {
	data := []byte(`{"a":{"b":"value"}}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	// Should have: BeginObject, Key("a"), BeginObject, Key("b"), String("value"), EndObject, EndObject
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d", len(events))
	}

	if events[0].Type != EventBeginObject {
		t.Errorf("event 0: expected EventBeginObject, got %v", events[0].Type)
	}
	if events[1].Type != EventKey || events[1].Key != "a" {
		t.Errorf("event 1: expected EventKey 'a', got %v %q", events[1].Type, events[1].Key)
	}
	if events[2].Type != EventBeginObject {
		t.Errorf("event 2: expected EventBeginObject, got %v", events[2].Type)
	}
	if events[3].Type != EventKey || events[3].Key != "b" {
		t.Errorf("event 3: expected EventKey 'b', got %v %q", events[3].Type, events[3].Key)
	}
	if events[4].Type != EventString || events[4].String != "value" {
		t.Errorf("event 4: expected EventString 'value', got %v %q", events[4].Type, events[4].String)
	}
	if events[5].Type != EventEndObject {
		t.Errorf("event 5: expected EventEndObject, got %v", events[5].Type)
	}
	if events[6].Type != EventEndObject {
		t.Errorf("event 6: expected EventEndObject, got %v", events[6].Type)
	}
}

func TestDecoder_StateTracking(t *testing.T) {
	data := []byte(`{"a":[1,2],"b":"value"}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read BeginObject
	event, err := dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginObject {
		t.Fatalf("expected EventBeginObject, got %v", event.Type)
	}
	if dec.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", dec.Depth())
	}
	if dec.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", dec.CurrentPath())
	}

	// Read Key("a")
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventKey || event.Key != "a" {
		t.Fatalf("expected EventKey 'a', got %v %q", event.Type, event.Key)
	}
	if dec.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", dec.CurrentPath())
	}
	if dec.CurrentKey() != "a" {
		t.Errorf("expected current key 'a', got %q", dec.CurrentKey())
	}

	// Read BeginArray
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginArray {
		t.Fatalf("expected EventBeginArray, got %v", event.Type)
	}
	if dec.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", dec.Depth())
	}
	if !dec.IsInArray() {
		t.Error("expected to be in array")
	}
	if dec.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", dec.CurrentPath())
	}

	// Read Int(1)
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventInt || event.Int != 1 {
		t.Fatalf("expected EventInt 1, got %v %d", event.Type, event.Int)
	}
	if dec.CurrentPath() != "a[0]" {
		t.Errorf("expected path 'a[0]', got %q", dec.CurrentPath())
	}
	if dec.CurrentIndex() != 1 {
		t.Errorf("expected current index 1, got %d", dec.CurrentIndex())
	}

	// Read Int(2)
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventInt || event.Int != 2 {
		t.Fatalf("expected EventInt 2, got %v %d", event.Type, event.Int)
	}
	if dec.CurrentPath() != "a[1]" {
		t.Errorf("expected path 'a[1]', got %q", dec.CurrentPath())
	}

	// Read EndArray
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventEndArray {
		t.Fatalf("expected EventEndArray, got %v", event.Type)
	}
	if dec.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", dec.Depth())
	}
	if !dec.IsInObject() {
		t.Error("expected to be in object")
	}
	if dec.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", dec.CurrentPath())
	}

	// Read Key("b")
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventKey || event.Key != "b" {
		t.Fatalf("expected EventKey 'b', got %v %q", event.Type, event.Key)
	}
	if dec.CurrentPath() != "b" {
		t.Errorf("expected path 'b', got %q", dec.CurrentPath())
	}

	// Read String("value")
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventString || event.String != "value" {
		t.Fatalf("expected EventString 'value', got %v %q", event.Type, event.String)
	}

	// Read EndObject
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventEndObject {
		t.Fatalf("expected EventEndObject, got %v", event.Type)
	}
	if dec.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", dec.Depth())
	}
	if dec.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", dec.CurrentPath())
	}
}

func TestDecoderComments(t *testing.T) {
	// Object with comments (will be skipped in Phase 1)
	// Note: Comments need to be on separate lines or properly terminated
	data := []byte(`{"name":"value"}` + "\n" + `# comment` + "\n")
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		// Phase 1: Should not see EventHeadComment or EventLineComment
		if event.Type == EventHeadComment || event.Type == EventLineComment {
			t.Errorf("Phase 1: should not see comment events, got %v", event.Type)
		}
		events = append(events, event)
	}

	// Should have: BeginObject, Key("name"), String("value"), EndObject
	// Comments are skipped, so no comment events
	if len(events) != 4 {
		t.Fatalf("expected 4 events (comments skipped), got %d: %v", len(events), events)
	}
}

func TestDecoder_LiteralKey(t *testing.T) {
	// Test literal (unquoted) keys
	data := []byte(`{key:"value"}`)
	dec, err := NewDecoder(bytes.NewReader(data), WithBrackets())
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
			t.Fatalf("unexpected error: %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	if events[1].Type != EventKey || events[1].Key != "key" {
		t.Errorf("expected EventKey 'key', got %v %q", events[1].Type, events[1].Key)
	}
}

func TestDecoder_Reset(t *testing.T) {
	data1 := []byte(`{"a":1}`)
	data2 := []byte(`{"b":2}`)

	dec, err := NewDecoder(bytes.NewReader(data1), WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read first document
	event, err := dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginObject {
		t.Errorf("expected EventBeginObject, got %v", event.Type)
	}

	// Reset to new reader
	err = dec.Reset(bytes.NewReader(data2), WithBrackets())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Read second document
	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventBeginObject {
		t.Errorf("expected EventBeginObject, got %v", event.Type)
	}

	event, err = dec.ReadEvent()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Type != EventKey || event.Key != "b" {
		t.Errorf("expected EventKey 'b', got %v %q", event.Type, event.Key)
	}
}

// TestDecoder_NestedDepth3_AllCombinations tests all combinations of
// object/sparse array/array at depth 3.
// Sparse arrays are objects with integer keys: {"0": val, "1": val}
func TestDecoder_NestedDepth3_AllCombinations(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []struct {
			eventType EventType
			key       string // for EventKey
			stringVal string // for EventString
			intVal    int64  // for EventInt
		}
	}{
		// Object containing Object containing Object
		{
			name: "Object_Object_Object",
			data: []byte(`{"a":{"b":{"c":"value"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventString, "", "value", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Object containing Array
		{
			name: "Object_Object_Array",
			data: []byte(`{"a":{"b":["x","y"]}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Object containing Sparse Array
		{
			name: "Object_Object_SparseArray",
			data: []byte(`{"a":{"b":{"0":"x","1":"y"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array is an object
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Array containing Object
		{
			name: "Object_Array_Object",
			data: []byte(`{"a":[{"b":"x"},{"c":"y"}]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Array containing Array
		{
			name: "Object_Array_Array",
			data: []byte(`{"a":[["x","y"],["z"]]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Array containing Sparse Array
		{
			name: "Object_Array_SparseArray",
			data: []byte(`{"a":[{"0":"x","1":"y"}]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Sparse Array containing Object
		{
			name: "Object_SparseArray_Object",
			data: []byte(`{"a":{"0":{"b":"x"},"1":{"c":"y"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Sparse Array containing Array
		{
			name: "Object_SparseArray_Array",
			data: []byte(`{"a":{"0":["x","y"],"1":["z"]}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Object containing Sparse Array containing Sparse Array
		{
			name: "Object_SparseArray_SparseArray",
			data: []byte(`{"a":{"0":{"0":"x","1":"y"},"1":{"0":"z"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Array containing Object containing Object
		{
			name: "Array_Object_Object",
			data: []byte(`[{"a":{"b":"x"}}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Object containing Array
		{
			name: "Array_Object_Array",
			data: []byte(`[{"a":["x","y"]}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Object containing Sparse Array
		{
			name: "Array_Object_SparseArray",
			data: []byte(`[{"a":{"0":"x","1":"y"}}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Array containing Object
		{
			name: "Array_Array_Object",
			data: []byte(`[[{"a":"x"},{"b":"y"}]]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Array containing Array
		{
			name: "Array_Array_Array",
			data: []byte(`[[["x","y"],["z"]]]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Array containing Sparse Array
		{
			name: "Array_Array_SparseArray",
			data: []byte(`[[{"0":"x","1":"y"}]]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Sparse Array containing Object
		{
			name: "Array_SparseArray_Object",
			data: []byte(`[{"0":{"a":"x"},"1":{"b":"y"}}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Sparse Array containing Array
		{
			name: "Array_SparseArray_Array",
			data: []byte(`[{"0":["x","y"],"1":["z"]}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Array containing Sparse Array containing Sparse Array
		{
			name: "Array_SparseArray_SparseArray",
			data: []byte(`[{"0":{"0":"x","1":"y"},"1":{"0":"z"}}]`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
			},
		},
		// Sparse Array containing Object containing Object
		{
			name: "SparseArray_Object_Object",
			data: []byte(`{"0":{"a":{"b":"x"}},"1":{"c":{"d":"y"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "d", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Object containing Array
		{
			name: "SparseArray_Object_Array",
			data: []byte(`{"0":{"a":["x","y"]},"1":{"b":["z"]}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Object containing Sparse Array
		{
			name: "SparseArray_Object_SparseArray",
			data: []byte(`{"0":{"a":{"0":"x","1":"y"}},"1":{"b":{"0":"z"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Array containing Object
		{
			name: "SparseArray_Array_Object",
			data: []byte(`{"0":[{"a":"x"},{"b":"y"}],"1":[{"c":"z"}]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Array containing Array
		{
			name: "SparseArray_Array_Array",
			data: []byte(`{"0":[["x","y"]],"1":[["z"]]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Array containing Sparse Array
		{
			name: "SparseArray_Array_SparseArray",
			data: []byte(`{"0":[{"0":"x","1":"y"}],"1":[{"0":"z"}]}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginArray, "", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Sparse Array containing Object
		{
			name: "SparseArray_SparseArray_Object",
			data: []byte(`{"0":{"0":{"a":"x"},"1":{"b":"y"}},"1":{"0":{"c":"z"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "a", "", 0},
				{EventString, "", "x", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "b", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0},
				{EventKey, "c", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Sparse Array containing Array
		{
			name: "SparseArray_SparseArray_Array",
			data: []byte(`{"0":{"0":["x","y"]},"1":{"0":["z"]}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "x", 0},
				{EventString, "", "y", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginArray, "", "", 0},
				{EventString, "", "z", 0},
				{EventEndArray, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
		// Sparse Array containing Sparse Array containing Sparse Array
		{
			name: "SparseArray_SparseArray_SparseArray",
			data: []byte(`{"0":{"0":{"0":"x","1":"y"},"1":{"0":"z"}},"1":{"0":{"0":"w"}}}`),
			expected: []struct {
				eventType EventType
				key       string
				stringVal string
				intVal    int64
			}{
				{EventBeginObject, "", "", 0}, // sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // deeply nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "x", 0},
				{EventKey, "1", "", 0},
				{EventString, "", "y", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // deeply nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "z", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventKey, "1", "", 0},
				{EventBeginObject, "", "", 0}, // nested sparse array
				{EventKey, "0", "", 0},
				{EventBeginObject, "", "", 0}, // deeply nested sparse array
				{EventKey, "0", "", 0},
				{EventString, "", "w", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
				{EventEndObject, "", "", 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := NewDecoder(bytes.NewReader(tt.data), WithBrackets())
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
					t.Fatalf("unexpected error: %v", err)
				}
				events = append(events, event)
			}

			// Verify event sequence
			if len(events) != len(tt.expected) {
				t.Fatalf("expected %d events, got %d", len(tt.expected), len(events))
			}

			for i, exp := range tt.expected {
				if events[i].Type != exp.eventType {
					t.Errorf("event %d: expected type %v, got %v", i, exp.eventType, events[i].Type)
				}
				if exp.key != "" && events[i].Key != exp.key {
					t.Errorf("event %d: expected key %q, got %q", i, exp.key, events[i].Key)
				}
				if exp.stringVal != "" && events[i].String != exp.stringVal {
					t.Errorf("event %d: expected string %q, got %q", i, exp.stringVal, events[i].String)
				}
				if exp.intVal != 0 && events[i].Int != exp.intVal {
					t.Errorf("event %d: expected int %d, got %d", i, exp.intVal, events[i].Int)
				}
			}

			// Verify that decoding completes successfully and depth returns to 0
			dec2, err := NewDecoder(bytes.NewReader(tt.data), WithBrackets())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			maxDepth := 0
			for {
				_, err := dec2.ReadEvent()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if dec2.Depth() > maxDepth {
					maxDepth = dec2.Depth()
				}
			}

			// Verify we're back at depth 0
			if dec2.Depth() != 0 {
				t.Errorf("expected final depth 0, got %d", dec2.Depth())
			}

			// Verify we reached depth 3 (for depth-3 combinations)
			if maxDepth < 3 {
				t.Errorf("expected max depth >= 3, got %d", maxDepth)
			}
		})
	}
}
