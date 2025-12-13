package stream

import (
	"strings"
	"testing"
)

func TestStateDepth(t *testing.T) {
	state := NewState()
	if state.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", state.Depth())
	}

	// Open object
	state.ProcessEvent(&Event{Type: EventBeginObject})
	if state.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", state.Depth())
	}

	// Open array inside object
	state.ProcessEvent(&Event{Type: EventBeginArray})
	if state.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", state.Depth())
	}

	// Close array
	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.Depth() != 1 {
		t.Errorf("expected depth 1, got %d", state.Depth())
	}

	// Close object
	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", state.Depth())
	}
}

func TestStateCurrentPath_Empty(t *testing.T) {
	state := NewState()
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateCurrentPath_ObjectKey(t *testing.T) {
	state := NewState()

	// { "key": "value" }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "key"})
	
	if state.CurrentPath() != "key" {
		t.Errorf("expected path 'key', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value"})
	if state.CurrentPath() != "key" {
		t.Errorf("expected path 'key', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateCurrentPath_NestedObject(t *testing.T) {
	state := NewState()

	// { "a": { "b": "value" } }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "a"})
	if state.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "b"})
	if state.CurrentPath() != "a.b" {
		t.Errorf("expected path 'a.b', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value"})
	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateCurrentPath_Array(t *testing.T) {
	state := NewState()

	// [ "value0", "value1" ]
	state.ProcessEvent(&Event{Type: EventBeginArray})
	state.ProcessEvent(&Event{Type: EventString, String: "value0"})
	if state.CurrentPath() != "[0]" {
		t.Errorf("expected path '[0]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value1"})
	if state.CurrentPath() != "[1]" {
		t.Errorf("expected path '[1]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateCurrentPath_NestedArray(t *testing.T) {
	state := NewState()

	// { "arr": [ "value0", "value1" ] }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "arr"})
	if state.CurrentPath() != "arr" {
		t.Errorf("expected path 'arr', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventBeginArray})
	state.ProcessEvent(&Event{Type: EventString, String: "value0"})
	if state.CurrentPath() != "arr[0]" {
		t.Errorf("expected path 'arr[0]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value1"})
	if state.CurrentPath() != "arr[1]" {
		t.Errorf("expected path 'arr[1]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.CurrentPath() != "arr" {
		t.Errorf("expected path 'arr', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateCurrentPath_SparseArray(t *testing.T) {
	state := NewState()

	// { "0": "value0", "1": "value1" }  (sparse array)
	// Note: Sparse arrays use EventKey with integer keys
	// Numeric keys are quoted (KPathQuoteField returns true for keys starting with digits)
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "0"})
	// Key "0" will be quoted because it starts with a digit
	path := state.CurrentPath()
	if path == "" {
		t.Error("expected non-empty path")
	}
	// Should contain "0" (possibly quoted)
	if !strings.Contains(path, "0") {
		t.Errorf("path %q should contain '0'", path)
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value0"})
	state.ProcessEvent(&Event{Type: EventKey, Key: "1"})
	path = state.CurrentPath()
	if path == "" {
		t.Error("expected non-empty path")
	}
	// Should contain "1" (possibly quoted)
	if !strings.Contains(path, "1") {
		t.Errorf("path %q should contain '1'", path)
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value1"})
	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}

func TestStateParentPath(t *testing.T) {
	state := NewState()

	// { "a": { "b": "value" } }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "a"})
	if state.ParentPath() != "" {
		t.Errorf("expected parent path '', got %q", state.ParentPath())
	}

	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "b"})
	if state.ParentPath() != "a" {
		t.Errorf("expected parent path 'a', got %q", state.ParentPath())
	}
	if state.CurrentPath() != "a.b" {
		t.Errorf("expected current path 'a.b', got %q", state.CurrentPath())
	}
}

func TestStateIsInObject(t *testing.T) {
	state := NewState()
	if state.IsInObject() {
		t.Error("should not be in object at start")
	}

	state.ProcessEvent(&Event{Type: EventBeginObject})
	if !state.IsInObject() {
		t.Error("should be in object")
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.IsInObject() {
		t.Error("should not be in object after closing")
	}
}

func TestStateIsInArray(t *testing.T) {
	state := NewState()
	if state.IsInArray() {
		t.Error("should not be in array at start")
	}

	state.ProcessEvent(&Event{Type: EventBeginArray})
	if !state.IsInArray() {
		t.Error("should be in array")
	}

	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.IsInArray() {
		t.Error("should not be in array after closing")
	}
}

func TestStateCurrentKey(t *testing.T) {
	state := NewState()

	// { "key": "value" }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "key"})
	if state.CurrentKey() != "key" {
		t.Errorf("expected key 'key', got %q", state.CurrentKey())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value"})
	if state.CurrentKey() != "key" {
		t.Errorf("expected key 'key', got %q", state.CurrentKey())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentKey() != "" {
		t.Errorf("expected empty key, got %q", state.CurrentKey())
	}
}

func TestStateCurrentIndex(t *testing.T) {
	state := NewState()

	// [ "value0", "value1", "value2" ]
	state.ProcessEvent(&Event{Type: EventBeginArray})
	if state.CurrentIndex() != 0 {
		t.Errorf("expected index 0, got %d", state.CurrentIndex())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value0"})
	if state.CurrentIndex() != 1 {
		t.Errorf("expected index 1, got %d", state.CurrentIndex())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "value1"})
	if state.CurrentIndex() != 2 {
		t.Errorf("expected index 2, got %d", state.CurrentIndex())
	}

	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.CurrentIndex() != 0 {
		t.Errorf("expected index 0 after closing, got %d", state.CurrentIndex())
	}
}


func TestStateLiteralKey(t *testing.T) {
	state := NewState()

	// { key: "value" }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "key"})
	if state.CurrentPath() != "key" {
		t.Errorf("expected path 'key', got %q", state.CurrentPath())
	}
	if state.CurrentKey() != "key" {
		t.Errorf("expected key 'key', got %q", state.CurrentKey())
	}
}

func TestStateQuotedKey(t *testing.T) {
	state := NewState()

	// { "key with spaces": "value" }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "key with spaces"})
	path := state.CurrentPath()
	// Path should be quoted (contains spaces)
	if path == "" {
		t.Error("expected non-empty path")
	}
	// Should contain the key (possibly quoted)
	if !strings.Contains(path, "key") {
		t.Errorf("path %q should contain 'key'", path)
	}
}

func TestStateMultipleKeys(t *testing.T) {
	state := NewState()

	// { "a": 1, "b": 2 }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "a"})
	state.ProcessEvent(&Event{Type: EventInt, Int: 1})
	if state.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventKey, Key: "b"})
	if state.CurrentPath() != "b" {
		t.Errorf("expected path 'b', got %q", state.CurrentPath())
	}
}

func TestStateArrayWithoutCommas(t *testing.T) {
	state := NewState()

	// [ "a" "b" ]  (no commas - events don't include commas)
	state.ProcessEvent(&Event{Type: EventBeginArray})
	state.ProcessEvent(&Event{Type: EventString, String: "a"})
	if state.CurrentPath() != "[0]" {
		t.Errorf("expected path '[0]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "b"})
	if state.CurrentPath() != "[1]" {
		t.Errorf("expected path '[1]', got %q", state.CurrentPath())
	}
}

func TestStateComplexNested(t *testing.T) {
	state := NewState()

	// { "a": { "b": [ "c", "d" ] } }
	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "a"})
	if state.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventBeginObject})
	state.ProcessEvent(&Event{Type: EventKey, Key: "b"})
	if state.CurrentPath() != "a.b" {
		t.Errorf("expected path 'a.b', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventBeginArray})
	state.ProcessEvent(&Event{Type: EventString, String: "c"})
	if state.CurrentPath() != "a.b[0]" {
		t.Errorf("expected path 'a.b[0]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventString, String: "d"})
	if state.CurrentPath() != "a.b[1]" {
		t.Errorf("expected path 'a.b[1]', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndArray})
	if state.CurrentPath() != "a.b" {
		t.Errorf("expected path 'a.b', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "a" {
		t.Errorf("expected path 'a', got %q", state.CurrentPath())
	}

	state.ProcessEvent(&Event{Type: EventEndObject})
	if state.CurrentPath() != "" {
		t.Errorf("expected empty path, got %q", state.CurrentPath())
	}
}
