package stream

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/token"
)

func TestKPathState(t *testing.T) {
	tests := []struct {
		path         string
		wantPath     string
		wantDepth    int
		wantIsObject bool
		wantIsArray  bool
		wantKey      string
		wantIntKey   int
		wantIndex    int
		wantParent   string
		wantStack    []token.TokenType
	}{
		// Empty and simple
		{"", "", 0, false, false, "", -1, 0, "", nil},
		{"a", "a", 1, true, false, "a", -1, 0, "", []token.TokenType{token.TLCurl}},
		{"a.b", "a.b", 2, true, false, "b", -1, 0, "a", []token.TokenType{token.TLCurl, token.TLCurl}},
		{"a.b.c", "a.b.c", 3, true, false, "c", -1, 0, "a.b", []token.TokenType{token.TLCurl, token.TLCurl, token.TLCurl}},

		// Dense arrays
		{"a[0]", "a[0]", 2, false, true, "", -1, 0, "a", []token.TokenType{token.TLCurl, token.TLSquare}},
		{"a[5]", "a[5]", 2, false, true, "", -1, 5, "a", []token.TokenType{token.TLCurl, token.TLSquare}},
		{"a[0][1]", "a[0][1]", 3, false, true, "", -1, 1, "a[0]", []token.TokenType{token.TLCurl, token.TLSquare, token.TLSquare}},

		// Sparse arrays (note: CurrentIntKey() returns the key from currentPath, not from state)
		{"a{0}", "a{0}", 2, false, true, "", -1, 0, "a", []token.TokenType{token.TLCurl, token.TLSquare}},
		{"a{5}", "a{5}", 2, false, true, "", -1, 5, "a", []token.TokenType{token.TLCurl, token.TLSquare}},

		// Mixed
		{"a.b[0]", "a.b[0]", 3, false, true, "", -1, 0, "a.b", []token.TokenType{token.TLCurl, token.TLCurl, token.TLSquare}},
		{"a[0].b", "a[0].b", 3, true, false, "b", -1, 0, "a[0]", []token.TokenType{token.TLCurl, token.TLSquare, token.TLCurl}},
		{"a.b{5}.c", "a.b{5}.c", 4, true, false, "c", -1, 0, "a.b{5}", []token.TokenType{token.TLCurl, token.TLCurl, token.TLSquare, token.TLCurl}},
		{"a[0][1].b", "a[0][1].b", 4, true, false, "b", -1, 0, "a[0][1]", []token.TokenType{token.TLCurl, token.TLSquare, token.TLSquare, token.TLCurl}},

		// Quoted fields (CurrentPath() returns quoted representation)
		{"'field name'", "\"field name\"", 1, true, false, "field name", -1, 0, "", []token.TokenType{token.TLCurl}},
		{"a.'field name'", "a.\"field name\"", 2, true, false, "field name", -1, 0, "a", []token.TokenType{token.TLCurl, token.TLCurl}},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			st, err := KPathState(tt.path)
			if err != nil {
				t.Fatalf("KPathState(%q) error: %v", tt.path, err)
			}

			// Basic state
			if st.CurrentPath() != tt.wantPath {
				t.Errorf("CurrentPath = %q, want %q", st.CurrentPath(), tt.wantPath)
			}
			if st.Depth() != tt.wantDepth {
				t.Errorf("Depth = %d, want %d", st.Depth(), tt.wantDepth)
			}
			if st.IsInObject() != tt.wantIsObject {
				t.Errorf("IsInObject = %v, want %v", st.IsInObject(), tt.wantIsObject)
			}
			if st.IsInArray() != tt.wantIsArray {
				t.Errorf("IsInArray = %v, want %v", st.IsInArray(), tt.wantIsArray)
			}

			// Context-specific checks
			if tt.wantIsObject && tt.wantKey != "" {
				if st.CurrentKey() != tt.wantKey {
					t.Errorf("CurrentKey = %q, want %q", st.CurrentKey(), tt.wantKey)
				}
			}
			if tt.wantIsArray {
				if st.CurrentIndex() != tt.wantIndex {
					t.Errorf("CurrentIndex = %d, want %d", st.CurrentIndex(), tt.wantIndex)
				}
			}

			// Parent path
			if st.ParentPath() != tt.wantParent {
				t.Errorf("ParentPath = %q, want %q", st.ParentPath(), tt.wantParent)
			}

			// Bracket stack
			if tt.wantStack != nil {
				if len(st.bracketStack) != len(tt.wantStack) {
					t.Fatalf("bracketStack length = %d, want %d", len(st.bracketStack), len(tt.wantStack))
				}
				for i, want := range tt.wantStack {
					if st.bracketStack[i] != want {
						t.Errorf("bracketStack[%d] = %v, want %v", i, st.bracketStack[i], want)
					}
				}
			}

			// Consistency checks
			if st.Depth() != len(st.bracketStack) {
				t.Errorf("Depth(%d) != len(bracketStack)(%d)", st.Depth(), len(st.bracketStack))
			}
			if len(st.pathStack) != len(st.bracketStack) {
				t.Errorf("len(pathStack)(%d) != len(bracketStack)(%d)", len(st.pathStack), len(st.bracketStack))
			}
		})
	}
}

func TestKPathState_ProcessNextEvent(t *testing.T) {
	// Verify that a state created by KPathState can process events without error
	tests := []struct {
		initPath  string
		nextEvent *Event
	}{
		{"a", &Event{Type: EventString, String: "value"}},
		{"a[0]", &Event{Type: EventInt, Int: 42}},
		{"a.b", &Event{Type: EventEndObject}},
	}

	for _, tt := range tests {
		t.Run(tt.initPath, func(t *testing.T) {
			st, err := KPathState(tt.initPath)
			if err != nil {
				t.Fatalf("KPathState(%q) error: %v", tt.initPath, err)
			}

			if err := st.ProcessEvent(tt.nextEvent); err != nil {
				t.Errorf("ProcessEvent() error: %v", err)
			}
		})
	}
}

func TestKPathState_InvalidPath(t *testing.T) {
	_, err := KPathState("a[invalid]")
	if err == nil {
		t.Error("Expected error for invalid path, got nil")
	}
}
