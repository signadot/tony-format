package ir

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseKPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *KPath
		wantErr bool
	}{
		{
			name:  "empty path",
			input: "",
			want:  nil,
		},
		{
			name:  "simple object path",
			input: "a",
			want: &KPath{
				Field: stringPtr("a"),
			},
		},
		{
			name:  "nested object path",
			input: "a.b.c",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					Field: stringPtr("b"),
					Next: &KPath{
						Field: stringPtr("c"),
					},
				},
			},
		},
		{
			name:  "array index",
			input: "a[0]",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					Index: intPtr(0),
				},
			},
		},
		{
			name:  "sparse array index",
			input: "a{42}",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					SparseIndex: intPtr(42),
				},
			},
		},
		{
			name:  "array wildcard",
			input: "a[*]",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					IndexAll: true,
				},
			},
		},
		{
			name:  "mixed path",
			input: "a[0].b{1}.c",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					Index: intPtr(0),
					Next: &KPath{
						Field: stringPtr("b"),
						Next: &KPath{
							SparseIndex: intPtr(1),
							Next: &KPath{
								Field: stringPtr("c"),
							},
						},
					},
				},
			},
		},
		{
			name:  "wildcard in nested path",
			input: "a[*].b",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					IndexAll: true,
					Next: &KPath{
						Field: stringPtr("b"),
					},
				},
			},
		},
		{
			name:  "quoted field with spaces",
			input: "'field name'.value",
			want: &KPath{
				Field: stringPtr("field name"),
				Next: &KPath{
					Field: stringPtr("value"),
				},
			},
		},
		{
			name:  "double quoted field",
			input: "\"field name\".value",
			want: &KPath{
				Field: stringPtr("field name"),
				Next: &KPath{
					Field: stringPtr("value"),
				},
			},
		},
		{
			name:  "quoted field with dots",
			input: "'field.with.dots'",
			want: &KPath{
				Field: stringPtr("field.with.dots"),
			},
		},
		{
			name:  "quoted field with escaped quote",
			input: "'field\\'s value'",
			want: &KPath{
				Field: stringPtr("field's value"),
			},
		},
		{
			name:  "field wildcard",
			input: "a.*.b",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					FieldAll: true,
					Next: &KPath{
						Field: stringPtr("b"),
					},
				},
			},
		},
		{
			name:  "sparse index wildcard",
			input: "a{*}.b",
			want: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					SparseIndexAll: true,
					Next: &KPath{
						Field: stringPtr("b"),
					},
				},
			},
		},
		{
			name:  "quoted literal asterisk field",
			input: "'*'.value",
			want: &KPath{
				Field: stringPtr("*"),
				Next: &KPath{
					Field: stringPtr("value"),
				},
			},
		},
		{
			name:  "multiple wildcards - array then field",
			input: "arr[*].*.key",
			want: &KPath{
				Field: stringPtr("arr"),
				Next: &KPath{
					IndexAll: true,
					Next: &KPath{
						FieldAll: true,
						Next: &KPath{
							Field: stringPtr("key"),
						},
					},
				},
			},
		},
		{
			name:  "multiple wildcards - field then array",
			input: "obj.*[*].value",
			want: &KPath{
				Field: stringPtr("obj"),
				Next: &KPath{
					FieldAll: true,
					Next: &KPath{
						IndexAll: true,
						Next: &KPath{
							Field: stringPtr("value"),
						},
					},
				},
			},
		},
		{
			name:  "top-level field wildcard",
			input: "*",
			want: &KPath{
				FieldAll: true,
			},
		},
		{
			name:  "top-level field wildcard with array",
			input: "*[0]",
			want: &KPath{
				FieldAll: true,
				Next: &KPath{
					Index: intPtr(0),
				},
			},
		},
		{
			name:  "top-level field wildcard with nested field",
			input: "*.b",
			want: &KPath{
				FieldAll: true,
				Next: &KPath{
					Field: stringPtr("b"),
				},
			},
		},
		{
			name:  "top-level field wildcard with sparse index",
			input: "*{13}",
			want: &KPath{
				FieldAll: true,
				Next: &KPath{
					SparseIndex: intPtr(13),
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKPath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseKPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !kpathEqual(got, tt.want) {
				t.Errorf("ParseKPath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKPath_String(t *testing.T) {
	tests := []struct {
		name string
		kpath *KPath
		want string
	}{
		{
			name: "simple object",
			kpath: &KPath{Field: stringPtr("a")},
			want: "a",
		},
		{
			name: "nested object",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{Field: stringPtr("b")},
			},
			want: "a.b",
		},
		{
			name: "with array index",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{Index: intPtr(0)},
			},
			want: "a[0]",
		},
		{
			name: "with sparse index",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{SparseIndex: intPtr(42)},
			},
			want: "a{42}",
		},
		{
			name: "with wildcard",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{IndexAll: true},
			},
			want: "a[*]",
		},
		{
			name: "mixed",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					Index: intPtr(0),
					Next: &KPath{
						Field: stringPtr("b"),
						Next: &KPath{SparseIndex: intPtr(1)},
					},
				},
			},
			want: "a[0].b{1}",
		},
		{
			name: "wildcard nested",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					IndexAll: true,
					Next: &KPath{Field: stringPtr("b")},
				},
			},
			want: "a[*].b",
		},
		{
			name: "field wildcard",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					FieldAll: true,
					Next: &KPath{Field: stringPtr("b")},
				},
			},
			want: "a.*.b",
		},
		{
			name: "top-level field wildcard",
			kpath: &KPath{
				FieldAll: true,
			},
			want: "*",
		},
		{
			name: "top-level field wildcard with array",
			kpath: &KPath{
				FieldAll: true,
				Next: &KPath{Index: intPtr(0)},
			},
			want: "*[0]",
		},
		{
			name: "top-level field wildcard with nested field",
			kpath: &KPath{
				FieldAll: true,
				Next: &KPath{Field: stringPtr("b")},
			},
			want: "*.b",
		},
		{
			name: "sparse index wildcard",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					SparseIndexAll: true,
					Next: &KPath{Field: stringPtr("b")},
				},
			},
			want: "a{*}.b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.kpath.String(); got != tt.want {
				t.Errorf("KPath.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

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
				parsed, err := ParseKPath(got)
				if err != nil {
					t.Errorf("ParseKPath(%q) error = %v (KPath() should produce parseable path)", got, err)
					return
				}
				// Verify that parsed path can be converted back to string (round-trip)
				parsedStr := parsed.String()
				reparsed, err := ParseKPath(parsedStr)
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

func TestKPath_String_QuotedFields(t *testing.T) {
	tests := []struct {
		name      string
		kpath     *KPath
		wantQuoted bool // Whether the field should be quoted (exact format may vary)
	}{
		{
			name: "quoted field with spaces",
			kpath: &KPath{
				Field: stringPtr("field name"),
			},
			wantQuoted: true,
		},
		{
			name: "quoted field with dots",
			kpath: &KPath{
				Field: stringPtr("field.with.dots"),
			},
			wantQuoted: true,
		},
		{
			name: "quoted nested fields",
			kpath: &KPath{
				Field: stringPtr("field name"),
				Next: &KPath{
					Field: stringPtr("another field"),
				},
			},
			wantQuoted: true,
		},
		{
			name: "unquoted simple field",
			kpath: &KPath{
				Field: stringPtr("simple"),
			},
			wantQuoted: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.kpath.String()
			// Check if field is quoted (starts and ends with ' or ")
			isQuoted := len(got) > 0 && (got[0] == '\'' || got[0] == '"') && 
				(got[len(got)-1] == '\'' || got[len(got)-1] == '"')
			if isQuoted != tt.wantQuoted {
				t.Errorf("KPath.String() = %v, wantQuoted = %v, got quoted = %v", got, tt.wantQuoted, isQuoted)
			}
			// Verify round-trip: parse and compare
			parsed, err := ParseKPath(got)
			if err != nil {
				t.Errorf("ParseKPath(%q) error = %v", got, err)
				return
			}
			if parsed.Field == nil || *parsed.Field != *tt.kpath.Field {
				t.Errorf("Round-trip failed: ParseKPath(%q).Field = %v, want %v", got, parsed.Field, tt.kpath.Field)
			}
		})
	}
}

func TestKPath_RoundTrip_QuotedFields(t *testing.T) {
	tests := []string{
		"'field name'",
		"\"field name\"",
		"'field.with.dots'",
		"\"field.with.dots\"",
		"'field\\'s value'",
		"\"field\\\"s value\"",
		"'field name'.value",
		"a.'field name'",
		"'field name'[0]",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			parsed, err := ParseKPath(input)
			if err != nil {
				t.Fatalf("ParseKPath(%q) error = %v", input, err)
			}
			output := parsed.String()
			// Parse again to verify round-trip
			reparsed, err := ParseKPath(output)
			if err != nil {
				t.Fatalf("ParseKPath(%q) error = %v", output, err)
			}
			// Compare field values (not string representation, since quoting style may differ)
			if !kpathEqual(parsed, reparsed) {
				t.Errorf("Round-trip failed: ParseKPath(%q) = %v, String() = %q, ParseKPath(%q) = %v",
					input, parsed, output, output, reparsed)
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func intPtr(i int) *int {
	return &i
}

func kpathEqual(a, b *KPath) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.FieldAll != b.FieldAll {
		return false
	}
	if !reflect.DeepEqual(a.Field, b.Field) {
		return false
	}
	if a.IndexAll != b.IndexAll {
		return false
	}
	if !reflect.DeepEqual(a.Index, b.Index) {
		return false
	}
	if a.SparseIndexAll != b.SparseIndexAll {
		return false
	}
	if !reflect.DeepEqual(a.SparseIndex, b.SparseIndex) {
		return false
	}
	return kpathEqual(a.Next, b.Next)
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantFirst    string
		wantRest     string
		wantErr      bool
	}{
		{
			name:      "empty path",
			input:     "",
			wantFirst: "",
			wantRest:  "",
		},
		{
			name:      "single field",
			input:     "a",
			wantFirst: "a",
			wantRest:  "",
		},
		{
			name:      "two fields",
			input:     "a.b",
			wantFirst: "a",
			wantRest:  "b",
		},
		{
			name:      "three fields",
			input:     "a.b.c",
			wantFirst: "a",
			wantRest:  "b.c",
		},
		{
			name:      "field then array",
			input:     "a[0]",
			wantFirst: "a",
			wantRest:  "[0]",
		},
		{
			name:      "array index first",
			input:     "[0].b",
			wantFirst: "[0]",
			wantRest:  "b",
		},
		{
			name:      "sparse index first",
			input:     "{13}.c",
			wantFirst: "{13}",
			wantRest:  "c",
		},
		{
			name:      "field then sparse",
			input:     "a{42}",
			wantFirst: "a",
			wantRest:  "{42}",
		},
		{
			name:      "nested arrays",
			input:     "[0][1]",
			wantFirst: "[0]",
			wantRest:  "[1]",
		},
		{
			name:      "complex path",
			input:     "a[0].b{13}.c",
			wantFirst: "a",
			wantRest:  "[0].b{13}.c",
		},
		{
			name:      "quoted field",
			input:     "'field name'.b",
			wantFirst: "'field name'", // May be converted to double quotes by token.Quote
			wantRest:  "b",
		},
		{
			name:      "quoted field with special chars",
			input:     "\"a.b\".c",
			wantFirst: "\"a.b\"",
			wantRest:  "c",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			first, rest := Split(tt.input)
			// For quoted fields, accept either single or double quotes (token.Quote may change style)
			if tt.name == "quoted field" {
				// Parse both to check they represent the same field
				firstKp, _ := ParseKPath(first)
				wantKp, _ := ParseKPath(tt.wantFirst)
				if !kpathEqual(firstKp, wantKp) {
					t.Errorf("Split() first = %q (parsed differently), want %q", first, tt.wantFirst)
				}
			} else {
				if first != tt.wantFirst {
					t.Errorf("Split() first = %q, want %q", first, tt.wantFirst)
				}
			}
			if rest != tt.wantRest {
				t.Errorf("Split() rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

func TestSplitAll(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     []string
		checkEq  bool // If true, parse and compare KPath structures instead of exact strings (for quoted fields)
	}{
		{
			name:  "empty path",
			input: "",
			want:  []string{},
		},
		{
			name:  "single field",
			input: "a",
			want:  []string{"a"},
		},
		{
			name:  "two fields",
			input: "a.b",
			want:  []string{"a", "b"},
		},
		{
			name:  "three fields",
			input: "a.b.c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "field then array",
			input: "a[0]",
			want:  []string{"a", "[0]"},
		},
		{
			name:  "array index first",
			input: "[0].b",
			want:  []string{"[0]", "b"},
		},
		{
			name:  "sparse index first",
			input: "{13}.c",
			want:  []string{"{13}", "c"},
		},
		{
			name:  "field then sparse",
			input: "a{42}",
			want:  []string{"a", "{42}"},
		},
		{
			name:  "nested arrays",
			input: "[0][1]",
			want:  []string{"[0]", "[1]"},
		},
		{
			name:  "complex path",
			input: "a[0].b{13}.c",
			want:  []string{"a", "[0]", "b", "{13}", "c"},
		},
		{
			name:   "quoted field",
			input:  "'field name'.b",
			want:   []string{"'field name'", "b"},
			checkEq: true, // Quote style may differ
		},
		{
			name:   "quoted field with special chars",
			input:  "\"a.b\".c",
			want:   []string{"\"a.b\"", "c"},
			checkEq: true, // Quote style may differ
		},
		{
			name:  "wildcard dense array",
			input: "a[*].b",
			want:  []string{"a", "[*]", "b"},
		},
		{
			name:  "wildcard sparse array",
			input: "a{*}.b",
			want:  []string{"a", "{*}", "b"},
		},
		{
			name:  "wildcard field",
			input: "a.*.b",
			want:  []string{"a", "*", "b"}, // FieldAll segment outputs "*" as top-level kpath
		},
		{
			name:  "top-level field wildcard",
			input: "*",
			want:  []string{"*"},
		},
		{
			name:  "top-level field wildcard with array",
			input: "*[0]",
			want:  []string{"*", "[0]"},
		},
		{
			name:  "top-level field wildcard with nested field",
			input: "*.b",
			want:  []string{"*", "b"},
		},
		{
			name:  "multiple wildcards",
			input: "a[*].*{*}.b",
			want:  []string{"a", "[*]", "*", "{*}", "b"}, // FieldAll segment outputs "*" as top-level kpath
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SplitAll(tt.input)
			
			if tt.checkEq {
				// For quoted fields, parse and compare KPath structures
				if len(got) != len(tt.want) {
					t.Errorf("SplitAll() length = %d, want %d", len(got), len(tt.want))
					return
				}
				for i := range got {
					gotKp, err1 := ParseKPath(got[i])
					wantKp, err2 := ParseKPath(tt.want[i])
					if err1 != nil || err2 != nil {
						t.Errorf("SplitAll()[%d] = %q, want %q (parse errors: %v, %v)", i, got[i], tt.want[i], err1, err2)
						continue
					}
					if !kpathEqual(gotKp, wantKp) {
						t.Errorf("SplitAll()[%d] = %q (parsed differently), want %q", i, got[i], tt.want[i])
					}
				}
			} else {
				if !equalStringSlice(got, tt.want) {
					t.Errorf("SplitAll() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

// equalStringSlice compares two string slices for equality.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestSplitAll_EachSegmentIsValidTopLevelKPath(t *testing.T) {
	// Verify that each segment returned by SplitAll is a valid top-level kpath
	testPaths := []string{
		"a.b.c",
		"[0].b",
		"{13}.c",
		"a[0].b{13}.c",
		"'field name'.b",
		"a[*].b",
		"a{*}.b",
		"a.*.b",
		"a[*].*{*}.b",
	}
	
	for _, input := range testPaths {
		t.Run(input, func(t *testing.T) {
			segments := SplitAll(input)
			for i, seg := range segments {
				_, err := ParseKPath(seg)
				if err != nil {
					t.Errorf("SplitAll()[%d] = %q is not a valid top-level kpath: %v", i, seg, err)
				}
			}
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		suffix   string
		want     string
		wantErr  bool
	}{
		{
			name:   "empty prefix",
			prefix: "",
			suffix: "a.b",
			want:   "a.b",
		},
		{
			name:   "empty suffix",
			prefix: "a",
			suffix: "",
			want:   "a",
		},
		{
			name:   "both empty",
			prefix: "",
			suffix: "",
			want:   "",
		},
		{
			name:   "field + field",
			prefix: "a",
			suffix: "b",
			want:   "a.b",
		},
		{
			name:   "field + nested",
			prefix: "a",
			suffix: "b.c",
			want:   "a.b.c",
		},
		{
			name:   "field + array",
			prefix: "a",
			suffix: "[0]",
			want:   "a[0]",
		},
		{
			name:   "array + field",
			prefix: "[0]",
			suffix: "b",
			want:   "[0].b",
		},
		{
			name:   "sparse + field",
			prefix: "{13}",
			suffix: "c",
			want:   "{13}.c",
		},
		{
			name:   "field + sparse",
			prefix: "a",
			suffix: "{42}",
			want:   "a{42}",
		},
		{
			name:   "array + nested",
			prefix: "[0]",
			suffix: "b.c",
			want:   "[0].b.c",
		},
		{
			name:   "complex join",
			prefix: "a",
			suffix: "[0].b{13}.c",
			want:   "a[0].b{13}.c",
		},
		{
			name:   "quoted field + field",
			prefix: "'field name'",
			suffix: "b",
			want:   "'field name'.b",
		},
		{
			name:   "field + quoted field",
			prefix: "a",
			suffix: "'b.c'",
			want:   "a.'b.c'",
		},
		{
			name:   "nested arrays",
			prefix: "[0]",
			suffix: "[1]",
			want:   "[0][1]",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Join(tt.prefix, tt.suffix)
			// For quoted fields, compare parsed structures (quote style may differ)
			if strings.Contains(tt.name, "quoted") {
				gotKp, err1 := ParseKPath(got)
				wantKp, err2 := ParseKPath(tt.want)
				if err1 != nil {
					t.Errorf("Join(%q, %q) = %q, ParseKPath error = %v", tt.prefix, tt.suffix, got, err1)
					return
				}
				if err2 != nil {
					t.Errorf("Join(%q, %q), ParseKPath(%q) error = %v", tt.prefix, tt.suffix, tt.want, err2)
					return
				}
				if !kpathEqual(gotKp, wantKp) {
					t.Errorf("Join(%q, %q) = %q (parsed differently), want %q\nGot:  %+v\nWant: %+v",
						tt.prefix, tt.suffix, got, tt.want, gotKp, wantKp)
				}
			} else {
				if got != tt.want {
					t.Errorf("Join(%q, %q) = %q, want %q", tt.prefix, tt.suffix, got, tt.want)
				}
			}
		})
	}
}

func TestSplitJoin_RoundTrip(t *testing.T) {
	tests := []string{
		"a",
		"a.b",
		"a.b.c",
		"a[0]",
		"[0].b",
		"a[0].b",
		"a{13}",
		"{13}.c",
		"a{13}.c",
		"a[0].b{13}.c",
		"'field name'.b",
		"a.'field name'",
	}
	
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			first, rest := Split(input)
			joined := Join(first, rest)
			
			// Parse both to compare structures (quote style may differ)
			originalKp, err1 := ParseKPath(input)
			joinedKp, err2 := ParseKPath(joined)
			if err1 != nil {
				t.Fatalf("ParseKPath(%q) error = %v", input, err1)
			}
			if err2 != nil {
				t.Fatalf("ParseKPath(%q) error = %v", joined, err2)
			}
			
			if !kpathEqual(originalKp, joinedKp) {
				t.Errorf("Round trip failed: Split(%q) = (%q, %q), Join(%q, %q) = %q\nOriginal: %+v\nJoined:   %+v",
					input, first, rest, first, rest, joined, originalKp, joinedKp)
			}
		})
	}
}
