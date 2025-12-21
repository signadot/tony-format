package ir

import "testing"

func TestParseTag(t *testing.T) {
	tests := []struct {
		tag  string
		want string // round-trip result
	}{
		{"", ""},
		{"!foo", "!foo"},
		{"!foo(a)", "!foo(a)"},
		{"!foo(a,b)", "!foo(a,b)"},
		{"!foo.bar", "!foo.bar"},
		{"!foo(a).bar(b)", "!foo(a).bar(b)"},
		{"!array(int)", "!array(int)"},
		{"!array(array(int))", "!array(array(int))"},
		{"!map(string,array(int))", "!map(string,array(int))"},
		{"!all.t", "!all.t"},
		{"!all.hasPath", "!all.hasPath"},
	}

	for _, tt := range tests {
		tree := ParseTag(tt.tag)
		got := tree.String()
		if got != tt.want {
			t.Errorf("ParseTag(%q).String() = %q, want %q", tt.tag, got, tt.want)
		}
	}
}

func TestTagTreeMap(t *testing.T) {
	tests := []struct {
		tag     string
		subst   map[string]string
		want    string
	}{
		{"!t", map[string]string{"t": "int"}, "!int"},
		{"!all.t", map[string]string{"t": "int"}, "!all.int"},
		{"!array(t)", map[string]string{"t": "int"}, "!array(int)"},
		{"!array(array(t))", map[string]string{"t": "int"}, "!array(array(int))"},
		{"!map(k,v)", map[string]string{"k": "string", "v": "int"}, "!map(string,int)"},
		{"!foo.bar.t", map[string]string{"t": "baz"}, "!foo.bar.baz"},
		{"!all.t", map[string]string{"x": "int"}, "!all.t"}, // no match
	}

	for _, tt := range tests {
		tree := ParseTag(tt.tag)
		mapped := tree.Map(func(name string) string {
			if replacement, ok := tt.subst[name]; ok {
				return replacement
			}
			return name
		})
		got := mapped.String()
		if got != tt.want {
			t.Errorf("ParseTag(%q).Map(%v).String() = %q, want %q", tt.tag, tt.subst, got, tt.want)
		}
	}
}

func TestTagTreeStructure(t *testing.T) {
	// Test !array(array(int)) structure
	tree := ParseTag("!array(array(int))")

	if tree.Name != "array" {
		t.Errorf("Name = %q, want %q", tree.Name, "array")
	}
	if len(tree.Args) != 1 {
		t.Fatalf("len(Args) = %d, want 1", len(tree.Args))
	}

	arg := tree.Args[0]
	if arg.Name != "array" {
		t.Errorf("Args[0].Name = %q, want %q", arg.Name, "array")
	}
	if len(arg.Args) != 1 {
		t.Fatalf("len(Args[0].Args) = %d, want 1", len(arg.Args))
	}

	innerArg := arg.Args[0]
	if innerArg.Name != "int" {
		t.Errorf("Args[0].Args[0].Name = %q, want %q", innerArg.Name, "int")
	}
}

func TestTagTreeLen(t *testing.T) {
	tests := []struct {
		tag  string
		want int
	}{
		{"", 0},
		{"!foo", 1},
		{"!foo.bar", 2},
		{"!foo.bar.baz", 3},
		{"!foo(a).bar", 2},
	}

	for _, tt := range tests {
		tree := ParseTag(tt.tag)
		got := tree.Len()
		if got != tt.want {
			t.Errorf("ParseTag(%q).Len() = %d, want %d", tt.tag, got, tt.want)
		}
	}
}

func TestTagTreeClone(t *testing.T) {
	original := ParseTag("!array(array(int)).foo")
	cloned := original.Clone()

	// Modify the clone
	cloned.Name = "modified"
	cloned.Args[0].Name = "also-modified"

	// Original should be unchanged
	if original.Name != "array" {
		t.Errorf("Original Name changed to %q", original.Name)
	}
	if original.Args[0].Name != "array" {
		t.Errorf("Original Args[0].Name changed to %q", original.Args[0].Name)
	}
}
