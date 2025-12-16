package kpath

import "testing"

func TestKPath_Type(t *testing.T) {
	tests := []struct {
		path      string
		wantKind  EntryKind
		wantWild  bool
	}{
		{"a", FieldEntry, false},
		{"*", FieldEntry, true},
		{"[0]", ArrayEntry, false},
		{"[*]", ArrayEntry, true},
		{"{5}", SparseArrayEntry, false},
		{"{*}", SparseArrayEntry, true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			kp, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.path, err)
			}

			typ := kp.Type()
			if typ.EntryKind != tt.wantKind {
				t.Errorf("EntryKind = %v, want %v", typ.EntryKind, tt.wantKind)
			}
			if typ.Wild != tt.wantWild {
				t.Errorf("Wild = %v, want %v", typ.Wild, tt.wantWild)
			}

			// Also test individual methods
			if got := kp.EntryKind(); got != tt.wantKind {
				t.Errorf("EntryKind() = %v, want %v", got, tt.wantKind)
			}
			if got := kp.Wild(); got != tt.wantWild {
				t.Errorf("Wild() = %v, want %v", got, tt.wantWild)
			}
		})
	}
}

func TestKPath_LastSegment(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"a", "a"},
		{"a.b.c", "c"},
		{"a[0]", "[0]"},
		{"a.b[5]", "[5]"},
		{"a{3}", "{3}"},
		{"a.b{7}.c", "c"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			kp, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.path, err)
			}

			last := kp.LastSegment()
			if last == nil {
				t.Fatal("LastSegment() returned nil")
			}

			got := last.String()
			if got != tt.want {
				t.Errorf("LastSegment().String() = %q, want %q", got, tt.want)
			}

			// Verify it's a single segment (Next is nil)
			if last.Next != nil {
				t.Error("LastSegment().Next should be nil")
			}
		})
	}
}

func TestKPath_Parent(t *testing.T) {
	tests := []struct {
		path       string
		wantParent string
		wantNil    bool
	}{
		{"a", "", true},
		{"a.b", "a", false},
		{"a.b.c", "a.b", false},
		{"a[0]", "a", false},
		{"a.b[5]", "a.b", false},
		{"a{3}", "a", false},
		{"[0]", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			kp, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.path, err)
			}

			parent := kp.Parent()

			if tt.wantNil {
				if parent != nil {
					t.Errorf("Parent() = %q, want nil", parent.String())
				}
				return
			}

			if parent == nil {
				t.Fatal("Parent() returned nil, want non-nil")
			}

			got := parent.String()
			if got != tt.wantParent {
				t.Errorf("Parent().String() = %q, want %q", got, tt.wantParent)
			}
		})
	}
}

func TestKPath_AncestorOrEqual(t *testing.T) {
	tests := []struct {
		path      string
		other     string
		wantAnc   bool
		wantEq    bool
	}{
		{"a", "a.b", true, false},
		{"a", "a.b.c", true, false},
		{"a.b", "a.b.c", true, false},
		{"a", "a", true, true},
		{"a.b", "a.b", true, true},
		{"a.b", "a", false, false},
		{"a.c", "a.b", false, false},
		{"", "a", true, false},
		{"", "", true, true},
		{"a[0]", "a[0].b", true, false},
		{"a", "a[0]", true, false},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_vs_"+tt.other, func(t *testing.T) {
			kp, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.path, err)
			}

			other, err := Parse(tt.other)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.other, err)
			}

			gotAnc, gotEq := kp.AncestorOrEqual(other)

			if gotAnc != tt.wantAnc {
				t.Errorf("AncestorOrEqual() anc = %v, want %v", gotAnc, tt.wantAnc)
			}
			if gotEq != tt.wantEq {
				t.Errorf("AncestorOrEqual() eq = %v, want %v", gotEq, tt.wantEq)
			}
		})
	}
}

func TestKPath_IsPrefix(t *testing.T) {
	tests := []struct {
		path   string
		other  string
		want   bool
	}{
		{"a", "a.b", true},
		{"a", "a.b.c", true},
		{"a.b", "a.b.c", true},
		{"a", "a", true},  // Equal is also a prefix
		{"a.b", "a", false},
		{"a.c", "a.b", false},
		{"", "a", true},
	}

	for _, tt := range tests {
		t.Run(tt.path+"_prefix_"+tt.other, func(t *testing.T) {
			kp, err := Parse(tt.path)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.path, err)
			}

			other, err := Parse(tt.other)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tt.other, err)
			}

			got := kp.IsPrefix(other)
			if got != tt.want {
				t.Errorf("IsPrefix() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKPath_Parent_NoSharing(t *testing.T) {
	// Verify that Parent() returns a deep copy, not sharing pointers
	kp, _ := Parse("a.b.c")
	parent := kp.Parent()

	// Modify the original's field
	if kp.Field != nil {
		*kp.Field = "modified"
	}

	// Parent should be unchanged
	if parent.String() != "a.b" {
		t.Errorf("Parent was affected by modification of original: got %q", parent.String())
	}
}

func TestKPath_LastSegment_NoSharing(t *testing.T) {
	// Verify that LastSegment() returns a deep copy
	kp, _ := Parse("a.b")
	last := kp.LastSegment()

	// Find and modify the last segment of original
	curr := kp
	for curr.Next != nil {
		curr = curr.Next
	}
	if curr.Field != nil {
		*curr.Field = "modified"
	}

	// LastSegment should be unchanged
	if last.String() != "b" {
		t.Errorf("LastSegment was affected by modification: got %q", last.String())
	}
}
