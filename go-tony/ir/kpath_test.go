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
