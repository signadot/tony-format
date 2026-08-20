package ir

import (
	"reflect"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

func TestNode_KPath(t *testing.T) {
	tests := []struct {
		name string
		node *Node
		want string
	}{
		{
			name: "root node",
			node: FromMap(map[string]*Node{}),
			want: "",
		},
		{
			name: "simple object field",
			node: FromMap(map[string]*Node{
				"a": FromString("value"),
			}).Values[0],
			want: "a",
		},
		{
			name: "nested object field",
			node: FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"b": FromString("value"),
				}),
			}).Values[0].Values[0],
			want: "a.b",
		},
		{
			name: "array element",
			node: FromSlice([]*Node{
				FromString("first"),
				FromString("second"),
			}).Values[1],
			want: "[1]",
		},
		{
			name: "nested array element",
			node: FromMap(map[string]*Node{
				"arr": FromSlice([]*Node{
					FromString("first"),
					FromString("second"),
				}),
			}).Values[0].Values[1],
			want: "arr[1]",
		},
		{
			name: "mixed object and array",
			node: FromMap(map[string]*Node{
				"a": FromSlice([]*Node{
					FromMap(map[string]*Node{
						"b": FromString("value"),
					}),
				}),
			}).Values[0].Values[0].Values[0],
			want: "a[0].b",
		},
		{
			name: "field with spaces",
			node: FromMap(map[string]*Node{
				"field name": FromString("value"),
			}).Values[0],
			want: `"field name"`,
		},
		{
			name: "nested field with spaces",
			node: FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"field name": FromString("value"),
				}),
			}).Values[0].Values[0],
			want: `a."field name"`,
		},
		{
			name: "field with dots",
			node: FromMap(map[string]*Node{
				"field.with.dots": FromString("value"),
			}).Values[0],
			want: `"field.with.dots"`,
		},
		{
			name: "nested field with dots",
			node: FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"field.with.dots": FromString("value"),
				}),
			}).Values[0].Values[0],
			want: `a."field.with.dots"`,
		},
		{
			name: "field with brackets",
			node: FromMap(map[string]*Node{
				"field[with]brackets": FromString("value"),
			}).Values[0],
			want: `"field[with]brackets"`,
		},
		{
			name: "field with braces",
			node: FromMap(map[string]*Node{
				"field{with}braces": FromString("value"),
			}).Values[0],
			want: `"field{with}braces"`,
		},
		{
			name: "field with escaped quote",
			node: FromMap(map[string]*Node{
				"field's value": FromString("value"),
			}).Values[0],
			want: `"field's value"`,
		},
		{
			name: "simple field does not need quoting",
			node: FromMap(map[string]*Node{
				"simple": FromString("value"),
			}).Values[0],
			want: "simple",
		},
		{
			name: "nested simple fields",
			node: FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"b": FromString("value"),
				}),
			}).Values[0].Values[0],
			want: "a.b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.node.KPath()
			if got != tt.want {
				t.Errorf("Node.KPath() = %v, want %v", got, tt.want)
			}
			// Verify that KPath() produces a parseable path
			if got != "" {
				parsed, err := kpath.Parse(got)
				if err != nil {
					t.Errorf("ParseKPath(%q) error = %v (KPath() should produce parseable path)", got, err)
					return
				}
				// Verify that parsed path can be converted back to string (round-trip)
				parsedStr := parsed.String()
				reparsed, err := kpath.Parse(parsedStr)
				if err != nil {
					t.Errorf("ParseKPath(%q) error = %v (round-trip string should be parseable)", parsedStr, err)
					return
				}
				// Check that the field names match (for object fields)
				if parsed.Field != nil && reparsed.Field != nil {
					if *parsed.Field != *reparsed.Field {
						t.Errorf("Round-trip failed: ParseKPath(%q).Field = %q, ParseKPath(%q).Field = %q", got, *parsed.Field, parsedStr, *reparsed.Field)
					}
				}
			}
		})
	}
}

// A wildcard names a SET of values and Get answers one, so it is refused -- not because
// wildcards are unsupported, but because this is the wrong question to put to them. The set
// forms are where they belong: `o list 'items[*].sku'` walks them, and a match applies a
// pattern to every node a wildcard reaches.
//
// Spelled out because a refusal that does not say why reads as a gap, and a later reader
// with a wildcard in hand needs to know which of the two it is.
func TestNode_GetKPath_Wildcard(t *testing.T) {
	tests := []struct {
		name  string
		kpath string
		want  string
	}{
		{
			name:  "array wildcard",
			kpath: "arr[*]",
			want:  "any index [*] in get",
		},
		{
			name:  "field wildcard",
			kpath: "obj.*",
			want:  "any field .* in get",
		},
		{
			name:  "sparse index wildcard",
			kpath: "arr{*}",
			want:  "any sparse index {*} in get",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node *Node
			if strings.Contains(tt.kpath, "[") || strings.Contains(tt.kpath, "{") {
				node = FromMap(map[string]*Node{
					"arr": FromSlice([]*Node{
						FromString("first"),
						FromString("second"),
					}),
				})
			} else {
				node = FromMap(map[string]*Node{
					"obj": FromMap(map[string]*Node{
						"key": FromString("value"),
					}),
				})
			}
			_, err := node.GetKPath(tt.kpath)
			if err == nil {
				t.Errorf("GetKPath() should error on wildcard %q", tt.kpath)
			}
			if err.Error() != tt.want {
				t.Errorf("GetKPath() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNode_ListKPath_Wildcard(t *testing.T) {
	node := FromMap(map[string]*Node{
		"arr": FromSlice([]*Node{
			FromString("first"),
			FromString("second"),
			FromString("third"),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "arr[*]")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("ListKPath() returned %d nodes, want 3", len(result))
	}
	if result[0].String != "first" || result[1].String != "second" || result[2].String != "third" {
		t.Errorf("ListKPath() returned wrong values: %v", result)
	}
}

func TestNode_ListKPath_WildcardNested(t *testing.T) {
	node := FromMap(map[string]*Node{
		"arr": FromSlice([]*Node{
			FromMap(map[string]*Node{
				"key": FromString("value1"),
			}),
			FromMap(map[string]*Node{
				"key": FromString("value2"),
			}),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "arr[*].key")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("ListKPath() returned %d nodes, want 2", len(result))
	}
	if result[0].String != "value1" || result[1].String != "value2" {
		t.Errorf("ListKPath() returned wrong values: %v", result)
	}
}

func TestNode_ListKPath_FieldWildcard(t *testing.T) {
	node := FromMap(map[string]*Node{
		"obj": FromMap(map[string]*Node{
			"a": FromString("value1"),
			"b": FromString("value2"),
			"c": FromString("value3"),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "obj.*")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("ListKPath() returned %d nodes, want 3", len(result))
	}
	values := make(map[string]bool)
	for _, n := range result {
		values[n.String] = true
	}
	if !values["value1"] || !values["value2"] || !values["value3"] {
		t.Errorf("ListKPath() returned wrong values: %v", result)
	}
}

func TestNode_ListKPath_FieldWildcardNested(t *testing.T) {
	node := FromMap(map[string]*Node{
		"obj": FromMap(map[string]*Node{
			"a": FromMap(map[string]*Node{
				"key": FromString("value1"),
			}),
			"b": FromMap(map[string]*Node{
				"key": FromString("value2"),
			}),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "obj.*.key")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("ListKPath() returned %d nodes, want 2", len(result))
	}
	if result[0].String != "value1" || result[1].String != "value2" {
		t.Errorf("ListKPath() returned wrong values: %v", result)
	}
}

func TestNode_ListKPath_SparseIndexWildcard(t *testing.T) {
	node := FromMap(map[string]*Node{
		"arr": FromSlice([]*Node{
			FromString("first"),
			FromString("second"),
			FromString("third"),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "arr{*}")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 3 {
		t.Errorf("ListKPath() returned %d nodes, want 3", len(result))
	}
	if result[0].String != "first" || result[1].String != "second" || result[2].String != "third" {
		t.Errorf("ListKPath() returned wrong values: %v", result)
	}
}

func TestNode_ListKPath_MultipleWildcards(t *testing.T) {
	// Test path with multiple wildcards: arr[*].*.key
	// This should match: arr[0].a.key, arr[0].b.key, arr[1].a.key, arr[1].b.key, etc.
	node := FromMap(map[string]*Node{
		"arr": FromSlice([]*Node{
			FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"key": FromString("value1a"),
				}),
				"b": FromMap(map[string]*Node{
					"key": FromString("value1b"),
				}),
			}),
			FromMap(map[string]*Node{
				"a": FromMap(map[string]*Node{
					"key": FromString("value2a"),
				}),
				"b": FromMap(map[string]*Node{
					"key": FromString("value2b"),
				}),
			}),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "arr[*].*.key")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 4 {
		t.Errorf("ListKPath() returned %d nodes, want 4", len(result))
	}
	values := make(map[string]bool)
	for _, n := range result {
		values[n.String] = true
	}
	expected := map[string]bool{
		"value1a": true,
		"value1b": true,
		"value2a": true,
		"value2b": true,
	}
	if !reflect.DeepEqual(values, expected) {
		t.Errorf("ListKPath() returned wrong values: got %v, want %v", values, expected)
	}
}

func TestNode_ListKPath_MultipleWildcards_FieldThenArray(t *testing.T) {
	// Test path with multiple wildcards: obj.*[*].value
	// This should match: obj.a[0].value, obj.a[1].value, obj.b[0].value, obj.b[1].value, etc.
	node := FromMap(map[string]*Node{
		"obj": FromMap(map[string]*Node{
			"a": FromSlice([]*Node{
				FromMap(map[string]*Node{
					"value": FromString("a0"),
				}),
				FromMap(map[string]*Node{
					"value": FromString("a1"),
				}),
			}),
			"b": FromSlice([]*Node{
				FromMap(map[string]*Node{
					"value": FromString("b0"),
				}),
				FromMap(map[string]*Node{
					"value": FromString("b1"),
				}),
			}),
		}),
	})
	dst := []*Node{}
	result, err := node.ListKPath(dst, "obj.*[*].value")
	if err != nil {
		t.Fatalf("ListKPath() error = %v", err)
	}
	if len(result) != 4 {
		t.Errorf("ListKPath() returned %d nodes, want 4", len(result))
	}
	values := make(map[string]bool)
	for _, n := range result {
		values[n.String] = true
	}
	expected := map[string]bool{
		"a0": true,
		"a1": true,
		"b0": true,
		"b1": true,
	}
	if !reflect.DeepEqual(values, expected) {
		t.Errorf("ListKPath() returned wrong values: got %v, want %v", values, expected)
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

// keyedDoc is a document holding keyed lists of every shape the walk has to
// read: keyed by a field, by a nested field, by a number, by the element
// itself, and an array with no !key tag at all.
func keyedDoc() *Node {
	return FromMap(map[string]*Node{
		"people": FromSlice([]*Node{
			FromMap(map[string]*Node{"name": FromString("joe"), "x": FromInt(1)}),
			FromMap(map[string]*Node{"name": FromString("bob"), "x": FromInt(2)}),
		}).WithTag("!key(name)"),
		"nested": FromSlice([]*Node{
			FromMap(map[string]*Node{
				"meta": FromMap(map[string]*Node{"name": FromString("joe")}),
				"x":    FromInt(3),
			}),
		}).WithTag("!key(meta.name)"),
		"byID": FromSlice([]*Node{
			FromMap(map[string]*Node{"id": FromInt(7), "x": FromInt(4)}),
		}).WithTag("!key(id)"),
		"bare": FromSlice([]*Node{
			FromString("joe"),
			FromString("bob"),
		}).WithTag("!key"),
		"twice": FromSlice([]*Node{
			FromMap(map[string]*Node{"name": FromString("joe"), "x": FromInt(5)}),
			FromMap(map[string]*Node{"name": FromString("joe"), "x": FromInt(6)}),
		}).WithTag("!key(name)"),
		"plain": FromSlice([]*Node{
			FromMap(map[string]*Node{"name": FromString("joe"), "x": FromInt(9)}),
		}),
		"obj": FromMap(map[string]*Node{"joe": FromInt(8)}),
	})
}

func TestNode_GetKPath_Keyed(t *testing.T) {
	doc := keyedDoc()
	tests := []struct {
		kpath string
		want  int64 // the x of the element reached, or -1 for no node
		err   bool
	}{
		{kpath: "people(joe).x", want: 1},
		{kpath: "people(bob).x", want: 2},
		{kpath: "people('joe').x", want: 1}, // a quoted key is the same key
		{kpath: "nested(joe).x", want: 3},
		{kpath: "byID(7).x", want: 4},
		{kpath: "twice(joe).x", want: 5}, // the first, Get taking one node
		// no element carries the key, so the path reaches nothing
		{kpath: "people(nope).x", want: -1},
		// nothing keys a list without the tag, so its elements have no keys
		{kpath: "plain(joe).x", want: -1},
		// the key is there but the field under it is not
		{kpath: "people(joe).zz", want: -1},
		// a key names an element of a list, and obj is not one
		{kpath: "obj(joe)", err: true},
	}
	for _, tt := range tests {
		got, err := doc.GetKPath(tt.kpath)
		if tt.err {
			if err == nil {
				t.Errorf("GetKPath(%q): got %v, want an error", tt.kpath, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("GetKPath(%q): %v", tt.kpath, err)
			continue
		}
		if tt.want == -1 {
			if got != nil {
				t.Errorf("GetKPath(%q): got %v, want nothing", tt.kpath, got)
			}
			continue
		}
		if got == nil {
			t.Errorf("GetKPath(%q): got nothing, want %d", tt.kpath, tt.want)
			continue
		}
		if got.Int64 == nil || *got.Int64 != tt.want {
			t.Errorf("GetKPath(%q): got %v, want %d", tt.kpath, got, tt.want)
		}
	}
}

func TestNode_GetKPath_KeyedBare(t *testing.T) {
	doc := keyedDoc()
	got, err := doc.GetKPath("bare(joe)")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.String != "joe" {
		t.Errorf(`GetKPath("bare(joe)"): got %v, want joe`, got)
	}
}

func TestNode_ListKPath_Keyed(t *testing.T) {
	doc := keyedDoc()
	tests := []struct {
		kpath string
		want  []int64 // the x of each element reached
	}{
		{kpath: "people(joe).x", want: []int64{1}},
		{kpath: "twice(joe).x", want: []int64{5, 6}}, // a key held twice names both
		{kpath: "people(nope).x", want: nil},
		{kpath: "plain(joe).x", want: nil},
		{kpath: "obj(joe)", want: nil},
	}
	for _, tt := range tests {
		nodes, err := doc.ListKPath(nil, tt.kpath)
		if err != nil {
			t.Errorf("ListKPath(%q): %v", tt.kpath, err)
			continue
		}
		var got []int64
		for _, n := range nodes {
			if n.Int64 == nil {
				t.Errorf("ListKPath(%q): reached %v, want a number", tt.kpath, n)
				continue
			}
			got = append(got, *n.Int64)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ListKPath(%q): got %v, want %v", tt.kpath, got, tt.want)
		}
	}
}

func TestNode_KeyField(t *testing.T) {
	tests := []struct {
		tag   string
		field string
		keyed bool
	}{
		{tag: "!key(name)", field: "name", keyed: true},
		{tag: "!key(meta.name)", field: "meta.name", keyed: true},
		{tag: "!key", keyed: true}, // the elements are their own keys
		{tag: "!other.key(name)", field: "name", keyed: true},
		{tag: "!key(name).other", field: "name", keyed: true},
		{tag: ""},
		{tag: "!other"},
	}
	for _, tt := range tests {
		field, keyed := FromSlice(nil).WithTag(tt.tag).KeyField()
		if field != tt.field || keyed != tt.keyed {
			t.Errorf("KeyField() of %q: got (%q, %t), want (%q, %t)",
				tt.tag, field, keyed, tt.field, tt.keyed)
		}
	}
}
