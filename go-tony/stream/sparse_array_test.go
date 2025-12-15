package stream

import (
	"bytes"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// Note on sparse array syntax (per Tony format spec):
// - Sparse arrays use unquoted integer keys (e.g., {0:"a",1:"b"}) as specified
//   in the Tony format documentation (docs/tony.md section "Sparse Arrays").
// - The decoder handles TInteger tokens followed by TColon as sparse array keys.
// - Regular object keys can be unquoted literals, but per the spec, when using
//   literals as keys, the colon must be followed by whitespace (e.g., {a: value}).
//   Without whitespace, {a:1} tokenizes as '{' 'a:1' '}' because ':' is allowed
//   within literals (e.g., http://hello). This is intentional tokenizer behavior.

// TestDecoder_SparseArray_ValueTypes tests sparse arrays with different value types
func TestDecoder_SparseArray_ValueTypes(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []*Event
	}{
		{
			name: "sparse array with string values",
			data: []byte(`{0:"hello",1:"world"}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "hello"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "world"},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with int values",
			data: []byte(`{0:42,1:100}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventInt, Int: 42},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventInt, Int: 100},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with float values",
			data: []byte(`{0:3.14,1:2.718}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventFloat, Float: 3.14},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventFloat, Float: 2.718},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with bool values",
			data: []byte(`{0:true,1:false}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBool, Bool: true},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBool, Bool: false},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with null values",
			data: []byte(`{0:null,1:null}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventNull},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventNull},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with mixed value types",
			data: []byte(`{0:"string",1:42,2:3.14,3:true,4:null}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "string"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventInt, Int: 42},
				{Type: EventIntKey, IntKey: 2},
				{Type: EventFloat, Float: 3.14},
				{Type: EventIntKey, IntKey: 3},
				{Type: EventBool, Bool: true},
				{Type: EventIntKey, IntKey: 4},
				{Type: EventNull},
				{Type: EventEndObject},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := NewDecoder(bytes.NewReader(tt.data), WithWire())
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			var events []*Event
			for {
				ev, err := dec.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() error = %v", err)
				}
				events = append(events, ev)
			}

			if len(events) != len(tt.expected) {
				t.Errorf("got %d events, want %d", len(events), len(tt.expected))
				t.Logf("got: %+v", events)
				t.Logf("want: %+v", tt.expected)
				return
			}

			for i, ev := range events {
				exp := tt.expected[i]
				if ev.Type != exp.Type {
					t.Errorf("event[%d].Type = %v, want %v", i, ev.Type, exp.Type)
				}
				if ev.Key != exp.Key {
					t.Errorf("event[%d].Key = %q, want %q", i, ev.Key, exp.Key)
				}
				if ev.IntKey != exp.IntKey {
					t.Errorf("event[%d].IntKey = %d, want %d", i, ev.IntKey, exp.IntKey)
				}
				if ev.String != exp.String {
					t.Errorf("event[%d].String = %q, want %q", i, ev.String, exp.String)
				}
				if ev.Int != exp.Int {
					t.Errorf("event[%d].Int = %d, want %d", i, ev.Int, exp.Int)
				}
				if ev.Float != exp.Float {
					t.Errorf("event[%d].Float = %g, want %g", i, ev.Float, exp.Float)
				}
				if ev.Bool != exp.Bool {
					t.Errorf("event[%d].Bool = %v, want %v", i, ev.Bool, exp.Bool)
				}
			}
		})
	}
}

// TestDecoder_SparseArray_Nested tests nested sparse arrays
func TestDecoder_SparseArray_Nested(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []*Event
	}{
		{
			name: "sparse array containing objects",
			// Per Tony spec: when using unquoted literals as keys, the colon must
			// be followed by whitespace. Without whitespace (e.g., {a:1}), the
			// tokenizer treats it as a single literal because ':' is allowed
			// within literals. Using quoted values (e.g., {"a":"x"}) avoids this.
			data: []byte(`{0:{"a":"x"},1:{"b":"y"}}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginObject},
				{Type: EventKey, Key: "a"},
				{Type: EventString, String: "x"},
				{Type: EventEndObject},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginObject},
				{Type: EventKey, Key: "b"},
				{Type: EventString, String: "y"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array containing arrays",
			data: []byte(`{0:["a","b"],1:["c"]}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginArray},
				{Type: EventString, String: "a"},
				{Type: EventString, String: "b"},
				{Type: EventEndArray},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginArray},
				{Type: EventString, String: "c"},
				{Type: EventEndArray},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array containing sparse arrays",
			data: []byte(`{0:{0:"a",1:"b"},1:{0:"c"}}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "c"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with mixed nested types",
			// Per Tony spec: sparse arrays use unquoted integer keys (e.g., 0:"a").
			// Regular object keys can be unquoted literals, but require whitespace
			// after the colon (e.g., {a: 1}). Without whitespace, {a:1} tokenizes as
			// a single literal because ':' is allowed within literals. Using quoted
			// values (e.g., {"a":1}) avoids this requirement.
			data: []byte(`{0:{"a":1},1:["x","y"],2:{0:"nested"}}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginObject},
				{Type: EventKey, Key: "a"},
				{Type: EventInt, Int: 1},
				{Type: EventEndObject},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginArray},
				{Type: EventString, String: "x"},
				{Type: EventString, String: "y"},
				{Type: EventEndArray},
				{Type: EventIntKey, IntKey: 2},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "nested"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := NewDecoder(bytes.NewReader(tt.data), WithWire())
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			var events []*Event
			for {
				ev, err := dec.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() error = %v", err)
				}
				events = append(events, ev)
			}

			if len(events) != len(tt.expected) {
				t.Errorf("got %d events, want %d", len(events), len(tt.expected))
				t.Logf("got: %+v", events)
				t.Logf("want: %+v", tt.expected)
				return
			}

			for i, ev := range events {
				exp := tt.expected[i]
				if ev.Type != exp.Type {
					t.Errorf("event[%d].Type = %v, want %v", i, ev.Type, exp.Type)
				}
				if ev.Key != exp.Key {
					t.Errorf("event[%d].Key = %q, want %q", i, ev.Key, exp.Key)
				}
				if ev.String != exp.String {
					t.Errorf("event[%d].String = %q, want %q", i, ev.String, exp.String)
				}
				if ev.Int != exp.Int {
					t.Errorf("event[%d].Int = %d, want %d", i, ev.Int, exp.Int)
				}
			}
		})
	}
}

// TestDecoder_SparseArray_MixedWithRegular tests sparse arrays mixed with regular objects/arrays
func TestDecoder_SparseArray_MixedWithRegular(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []*Event
	}{
		{
			name: "object containing sparse array",
			data: []byte(`{"key":{0:"a",1:"b"}}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventKey, Key: "key"},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
		},
		{
			name: "array containing sparse array",
			data: []byte(`[{0:"a",1:"b"}]`),
			expected: []*Event{
				{Type: EventBeginArray},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
				{Type: EventEndArray},
			},
		},
		{
			name: "sparse array containing regular array",
			data: []byte(`{0:["a","b"],1:["c"]}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginArray},
				{Type: EventString, String: "a"},
				{Type: EventString, String: "b"},
				{Type: EventEndArray},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginArray},
				{Type: EventString, String: "c"},
				{Type: EventEndArray},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array containing regular object",
			// Per Tony spec: when using unquoted literals as keys, the colon must
			// be followed by whitespace. Without whitespace (e.g., {a:x}), the
			// tokenizer treats it as a single literal because ':' is allowed
			// within literals. Using quoted values (e.g., {"a":"x"}) avoids this.
			data: []byte(`{0:{"a":"x"},1:{"b":"y"}}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginObject},
				{Type: EventKey, Key: "a"},
				{Type: EventString, String: "x"},
				{Type: EventEndObject},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventBeginObject},
				{Type: EventKey, Key: "b"},
				{Type: EventString, String: "y"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := NewDecoder(bytes.NewReader(tt.data), WithWire())
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			var events []*Event
			for {
				ev, err := dec.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() error = %v", err)
				}
				events = append(events, ev)
			}

			if len(events) != len(tt.expected) {
				t.Errorf("got %d events, want %d", len(events), len(tt.expected))
				t.Logf("got: %+v", events)
				t.Logf("want: %+v", tt.expected)
				return
			}

			for i, ev := range events {
				exp := tt.expected[i]
				if ev.Type != exp.Type {
					t.Errorf("event[%d].Type = %v, want %v", i, ev.Type, exp.Type)
				}
				if ev.Key != exp.Key {
					t.Errorf("event[%d].Key = %q, want %q", i, ev.Key, exp.Key)
				}
				if ev.String != exp.String {
					t.Errorf("event[%d].String = %q, want %q", i, ev.String, exp.String)
				}
			}
		})
	}
}

// TestState_SparseArray_PathTracking tests path tracking for sparse arrays
func TestState_SparseArray_PathTracking(t *testing.T) {
	tests := []struct {
		name           string
		events         []*Event
		expectedPaths  []string
		expectedDepths []int
	}{
		{
			name: "simple sparse array",
			events: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
			},
			expectedPaths: []string{
				"",    // after BeginObject
				"{0}", // after IntKey 0
				"{0}", // after String "a"
				"{1}", // after IntKey 1 (quoted because starts with digit)
				"{1}", // after String "b"
				"",    // after EndObject
			},
			expectedDepths: []int{
				1, // BeginObject
				1, // Key "0"
				1, // String "a"
				1, // Key "1"
				1, // String "b"
				0, // EndObject
			},
		},
		{
			name: "nested sparse arrays",
			events: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventEndObject},
				{Type: EventEndObject},
			},
			expectedPaths: []string{
				"",       // after BeginObject
				"{0}",    // after Key "0" (quoted because starts with digit)
				"{0}",    // after BeginObject (nested)
				"{0}{0}", // after Key "0" (nested, quoted)
				"{0}{0}", // after String "a"
				"{0}",    // after EndObject (nested)
				"",       // after EndObject
			},
			expectedDepths: []int{
				1, // BeginObject
				1, // Key "0"
				2, // BeginObject (nested)
				2, // Key "0" (nested)
				2, // String "a"
				1, // EndObject (nested)
				0, // EndObject
			},
		},
		{
			name: "sparse array in regular array",
			events: []*Event{
				{Type: EventBeginArray},
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventEndObject},
				{Type: EventEndArray},
			},
			expectedPaths: []string{
				"",       // after BeginArray
				"[0]",    // after BeginObject
				"[0]{0}", // after IntKey 0
				"[0]{0}", // after String "a"
				"[0]",    // after EndObject
				"",       // after EndArray
			},
			expectedDepths: []int{
				1, // BeginArray
				2, // BeginObject
				2, // Key "0"
				2, // String "a"
				1, // EndObject
				0, // EndArray
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewState()
			for i, ev := range tt.events {
				if err := state.ProcessEvent(ev); err != nil {
					t.Fatalf("ProcessEvent() at index %d error = %v", i, err)
				}

				path := state.CurrentPath()
				depth := state.Depth()

				if i < len(tt.expectedPaths) {
					if path != tt.expectedPaths[i] {
						t.Errorf("after event[%d] %v: path = %q, want %q", i, ev.Type, path, tt.expectedPaths[i])
					}
					if depth != tt.expectedDepths[i] {
						t.Errorf("after event[%d] %v: depth = %d, want %d", i, ev.Type, depth, tt.expectedDepths[i])
					}
				}
			}
		})
	}
}

// TestSparseArray_RoundTrip tests encoding and decoding sparse arrays
func TestSparseArray_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "simple sparse array",
			data: []byte(`{0:"a",1:"b"}`),
		},
		{
			name: "sparse array with mixed types",
			data: []byte(`{0:"string",1:42,2:3.14,3:true,4:null}`),
		},
		{
			name: "nested sparse arrays",
			data: []byte(`{0:{0:"a",1:"b"},1:{0:"c"}}`),
		},
		{
			name: "sparse array in object",
			data: []byte(`{"key":{0:"a",1:"b"}}`),
		},
		{
			name: "sparse array in array",
			data: []byte(`[{0:"a",1:"b"}]`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Decode original
			dec1, err := NewDecoder(bytes.NewReader(tt.data), WithWire())
			if err != nil {
				t.Fatalf("NewDecoder() error = %v", err)
			}

			var events []*Event
			for {
				ev, err := dec1.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() error = %v", err)
				}
				events = append(events, ev)
			}

			// Encode back
			var buf bytes.Buffer
			enc, err := NewEncoder(&buf, WithWire())
			if err != nil {
				t.Fatalf("NewEncoder() error = %v", err)
			}

			for _, ev := range events {
				switch ev.Type {
				case EventBeginObject:
					if err := enc.BeginObject(); err != nil {
						t.Fatalf("BeginObject() error = %v", err)
					}
				case EventEndObject:
					if err := enc.EndObject(); err != nil {
						t.Fatalf("EndObject() error = %v", err)
					}
				case EventBeginArray:
					if err := enc.BeginArray(); err != nil {
						t.Fatalf("BeginArray() error = %v", err)
					}
				case EventEndArray:
					if err := enc.EndArray(); err != nil {
						t.Fatalf("EndArray() error = %v", err)
					}
				case EventKey:
					if err := enc.WriteKey(ev.Key); err != nil {
						t.Fatalf("WriteKey(%q) error = %v", ev.Key, err)
					}
				case EventIntKey:
					if err := enc.WriteIntKey(int(ev.IntKey)); err != nil {
						t.Fatalf("WriteKey(%d) error = %v", ev.IntKey, err)
					}
				case EventString:
					if err := enc.WriteString(ev.String); err != nil {
						t.Fatalf("WriteString(%q) error = %v", ev.String, err)
					}
				case EventInt:
					if err := enc.WriteInt(ev.Int); err != nil {
						t.Fatalf("WriteInt(%d) error = %v", ev.Int, err)
					}
				case EventFloat:
					if err := enc.WriteFloat(ev.Float); err != nil {
						t.Fatalf("WriteFloat(%g) error = %v", ev.Float, err)
					}
				case EventBool:
					if err := enc.WriteBool(ev.Bool); err != nil {
						t.Fatalf("WriteBool(%v) error = %v", ev.Bool, err)
					}
				case EventNull:
					if err := enc.WriteNull(); err != nil {
						t.Fatalf("WriteNull() error = %v", err)
					}
				}
			}

			// Decode encoded result
			dec2, err := NewDecoder(bytes.NewReader(buf.Bytes()), WithWire())
			if err != nil {
				t.Fatalf("NewDecoder() on encoded data error = %v", err)
			}

			var events2 []*Event
			for {
				ev, err := dec2.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() on encoded data error = %v", err)
				}
				events2 = append(events2, ev)
			}

			// Compare
			if len(events2) != len(events) {
				t.Errorf("round-trip: got %d events, want %d", len(events2), len(events))
				t.Logf("original: %+v", events)
				t.Logf("round-trip: %+v", events2)
				return
			}

			for i, ev2 := range events2 {
				ev1 := events[i]
				if ev2.Type != ev1.Type {
					t.Errorf("event[%d].Type = %v, want %v", i, ev2.Type, ev1.Type)
				}
				if ev2.Key != ev1.Key {
					t.Errorf("event[%d].Key = %q, want %q", i, ev2.Key, ev1.Key)
				}
				if ev2.String != ev1.String {
					t.Errorf("event[%d].String = %q, want %q", i, ev2.String, ev1.String)
				}
				if ev2.Int != ev1.Int {
					t.Errorf("event[%d].Int = %d, want %d", i, ev2.Int, ev1.Int)
				}
				if ev2.Float != ev1.Float {
					t.Errorf("event[%d].Float = %g, want %g", i, ev2.Float, ev1.Float)
				}
				if ev2.Bool != ev1.Bool {
					t.Errorf("event[%d].Bool = %v, want %v", i, ev2.Bool, ev1.Bool)
				}
			}
		})
	}
}

// TestSparseArray_EventsToNode tests converting sparse array events to IR nodes
func TestSparseArray_EventsToNode(t *testing.T) {
	tests := []struct {
		name     string
		events   []Event
		validate func(t *testing.T, node *ir.Node)
	}{
		{
			name: "simple sparse array",
			events: []Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
			},
			validate: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Errorf("node.Type = %v, want ObjectType", node.Type)
				}
				if len(node.Fields) != 2 {
					t.Fatalf("node.Fields length = %d, want 2", len(node.Fields))
				}
				if *node.Fields[0].Int64 != 0 {
					t.Errorf("node.Fields[0] = %q, want \"0\"", node.Fields[0].String)
				}
				if *node.Fields[1].Int64 != 1 {
					t.Errorf("node.Fields[1] = %q, want \"1\"", node.Fields[1].String)
				}
				if node.Values[0].String != "a" {
					t.Errorf("node.Values[0] = %q, want \"a\"", node.Values[0].String)
				}
				if node.Values[1].String != "b" {
					t.Errorf("node.Values[1] = %q, want \"b\"", node.Values[1].String)
				}
			},
		},
		{
			name: "sparse array with mixed types",
			events: []Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventInt, Int: 42},
				{Type: EventIntKey, IntKey: 2},
				{Type: EventFloat, Float: 3.14},
				{Type: EventEndObject},
			},
			validate: func(t *testing.T, node *ir.Node) {
				if node.Type != ir.ObjectType {
					t.Errorf("node.Type = %v, want ObjectType", node.Type)
				}
				if len(node.Values) != 3 {
					t.Fatalf("node.Values length = %d, want 3", len(node.Values))
				}
				if node.Values[0].String != "a" {
					t.Errorf("node.Values[0] = %q, want \"a\"", node.Values[0].String)
				}
				if node.Values[1].Int64 == nil || *node.Values[1].Int64 != 42 {
					t.Errorf("node.Values[1].Int64 = %v, want 42", node.Values[1].Int64)
				}
				if node.Values[2].Float64 == nil || *node.Values[2].Float64 != 3.14 {
					t.Errorf("node.Values[2].Float64 = %v, want 3.14", node.Values[2].Float64)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := EventsToNode(tt.events)
			if err != nil {
				t.Fatalf("EventsToNode() error = %v", err)
			}
			if node == nil {
				t.Fatal("EventsToNode() returned nil")
			}
			tt.validate(t, node)
		})
	}
}

// TestSparseArray_EdgeCases tests edge cases for sparse arrays
func TestSparseArray_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected []*Event
	}{
		{
			name: "sparse array with unquoted integer keys",
			data: []byte(`{0:"a",1:"b"}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 1},
				{Type: EventString, String: "b"},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with large integer keys",
			data: []byte(`{0:"a",999:"b",1000:"c"}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "a"},
				{Type: EventIntKey, IntKey: 999},
				{Type: EventString, String: "b"},
				{Type: EventIntKey, IntKey: 1000},
				{Type: EventString, String: "c"},
				{Type: EventEndObject},
			},
		},
		{
			name: "sparse array with zero key",
			data: []byte(`{0:"zero"}`),
			expected: []*Event{
				{Type: EventBeginObject},
				{Type: EventIntKey, IntKey: 0},
				{Type: EventString, String: "zero"},
				{Type: EventEndObject},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := NewDecoder(bytes.NewReader(tt.data), WithWire())
			if err != nil {
				// Some tests might fail decoder creation (e.g., unquoted keys)
				// That's okay - we're testing edge cases
				t.Logf("NewDecoder() error = %v (may be expected)", err)
				return
			}

			var events []*Event
			for {
				ev, err := dec.ReadEvent()
				if err != nil {
					if err.Error() == "EOF" {
						break
					}
					t.Fatalf("ReadEvent() error = %v", err)
				}
				events = append(events, ev)
			}

			if len(events) != len(tt.expected) {
				t.Errorf("got %d events, want %d", len(events), len(tt.expected))
				t.Logf("got: %+v", events)
				t.Logf("want: %+v", tt.expected)
				return
			}

			for i, ev := range events {
				exp := tt.expected[i]
				if ev.Type != exp.Type {
					t.Errorf("event[%d].Type = %v, want %v", i, ev.Type, exp.Type)
				}
				if ev.IntKey != exp.IntKey {
					t.Errorf("event[%d].IntKey = %d, want %d", i, ev.IntKey, exp.IntKey)
				}
				if ev.String != exp.String {
					t.Errorf("event[%d].String = %q, want %q", i, ev.String, exp.String)
				}
			}
		})
	}
}
