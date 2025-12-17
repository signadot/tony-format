package kpath

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
			got, err := Parse(tt.input)
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
		name  string
		kpath *KPath
		want  string
	}{
		{
			name:  "simple object",
			kpath: &KPath{Field: stringPtr("a")},
			want:  "a",
		},
		{
			name: "nested object",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next:  &KPath{Field: stringPtr("b")},
			},
			want: "a.b",
		},
		{
			name: "with array index",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next:  &KPath{Index: intPtr(0)},
			},
			want: "a[0]",
		},
		{
			name: "with sparse index",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next:  &KPath{SparseIndex: intPtr(42)},
			},
			want: "a{42}",
		},
		{
			name: "with wildcard",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next:  &KPath{IndexAll: true},
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
						Next:  &KPath{SparseIndex: intPtr(1)},
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
					Next:     &KPath{Field: stringPtr("b")},
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
					Next:     &KPath{Field: stringPtr("b")},
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
				Next:     &KPath{Index: intPtr(0)},
			},
			want: "*[0]",
		},
		{
			name: "top-level field wildcard with nested field",
			kpath: &KPath{
				FieldAll: true,
				Next:     &KPath{Field: stringPtr("b")},
			},
			want: "*.b",
		},
		{
			name: "sparse index wildcard",
			kpath: &KPath{
				Field: stringPtr("a"),
				Next: &KPath{
					SparseIndexAll: true,
					Next:           &KPath{Field: stringPtr("b")},
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

func TestKPath_String_QuotedFields(t *testing.T) {
	tests := []struct {
		name       string
		kpath      *KPath
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
			parsed, err := Parse(got)
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
			parsed, err := Parse(input)
			if err != nil {
				t.Fatalf("ParseKPath(%q) error = %v", input, err)
			}
			output := parsed.String()
			// Parse again to verify round-trip
			reparsed, err := Parse(output)
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

func TestSplit(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFirst string
		wantRest  string
		wantErr   bool
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
				firstKp, _ := Parse(first)
				wantKp, _ := Parse(tt.wantFirst)
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

func TestRSplit(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantParent string
		wantLast   string
		wantErr    bool
	}{
		{
			name:       "empty path",
			input:      "",
			wantParent: "",
			wantLast:   "",
		},
		{
			name:       "single field",
			input:      "a",
			wantParent: "",
			wantLast:   "a",
		},
		{
			name:       "two fields",
			input:      "a.b",
			wantParent: "a",
			wantLast:   "b",
		},
		{
			name:       "three fields",
			input:      "a.b.c",
			wantParent: "a.b",
			wantLast:   "c",
		},
		{
			name:       "field then array",
			input:      "a[0]",
			wantParent: "a",
			wantLast:   "[0]",
		},
		{
			name:       "array index first",
			input:      "[0].b",
			wantParent: "[0]",
			wantLast:   "b",
		},
		{
			name:       "sparse index first",
			input:      "{13}.c",
			wantParent: "{13}",
			wantLast:   "c",
		},
		{
			name:       "nested arrays",
			input:      "a[0][1]",
			wantParent: "a[0]",
			wantLast:   "[1]",
		},
		{
			name:       "mixed path",
			input:      "a.b[0].c",
			wantParent: "a.b[0]",
			wantLast:   "c",
		},
		{
			name:       "quoted field",
			input:      "a.'field name'",
			wantParent: "a",
			wantLast:   "\"field name\"", // SegmentString normalizes to double quotes
		},
		{
			name:       "quoted field at end",
			input:      "a.b.'field name'",
			wantParent: "a.b",
			wantLast:   "\"field name\"", // SegmentString normalizes to double quotes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotParent, gotLast := RSplit(tt.input)
			if gotParent != tt.wantParent {
				t.Errorf("RSplit(%q) parent = %q, want %q", tt.input, gotParent, tt.wantParent)
			}
			if gotLast != tt.wantLast {
				t.Errorf("RSplit(%q) last = %q, want %q", tt.input, gotLast, tt.wantLast)
			}
		})
	}
}

func TestSplitAll(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    []string
		checkEq bool // If true, parse and compare KPath structures instead of exact strings (for quoted fields)
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
			name:    "quoted field",
			input:   "'field name'.b",
			want:    []string{"'field name'", "b"},
			checkEq: true, // Quote style may differ
		},
		{
			name:    "quoted field with special chars",
			input:   "\"a.b\".c",
			want:    []string{"\"a.b\"", "c"},
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
					gotKp, err1 := Parse(got[i])
					wantKp, err2 := Parse(tt.want[i])
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
				_, err := Parse(seg)
				if err != nil {
					t.Errorf("SplitAll()[%d] = %q is not a valid top-level kpath: %v", i, seg, err)
				}
			}
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		suffix  string
		want    string
		wantErr bool
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
				gotKp, err1 := Parse(got)
				wantKp, err2 := Parse(tt.want)
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
			originalKp, err1 := Parse(input)
			joinedKp, err2 := Parse(joined)
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
