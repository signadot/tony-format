package kpath

import "testing"

func TestSegmentFieldName(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantField   string
		wantIsField bool
	}{
		{
			name:        "empty segment",
			input:       "",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "simple field",
			input:       "a",
			wantField:   "a",
			wantIsField: true,
		},
		{
			name:        "field with spaces (single quoted)",
			input:       "'field name'",
			wantField:   "field name",
			wantIsField: true,
		},
		{
			name:        "field with spaces (double quoted)",
			input:       "\"field name\"",
			wantField:   "field name",
			wantIsField: true,
		},
		{
			name:        "field with dots",
			input:       "'field.with.dots'",
			wantField:   "field.with.dots",
			wantIsField: true,
		},
		{
			name:        "field with brackets",
			input:       "'field[with]brackets'",
			wantField:   "field[with]brackets",
			wantIsField: true,
		},
		{
			name:        "array index",
			input:       "[0]",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "sparse array index",
			input:       "{42}",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "array wildcard",
			input:       "[*]",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "sparse array wildcard",
			input:       "{*}",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "field wildcard",
			input:       "*",
			wantField:   "",
			wantIsField: false,
		},
		{
			name:        "quoted asterisk (field)",
			input:       "'*'",
			wantField:   "*",
			wantIsField: true,
		},
		{
			name:        "field with escaped quote",
			input:       "'field\\'s value'",
			wantField:   "field's value",
			wantIsField: true,
		},
		{
			name:        "numeric field (quoted)",
			input:       "'123'",
			wantField:   "123",
			wantIsField: true,
		},
		{
			name:        "boolean-like field (quoted)",
			input:       "'true'",
			wantField:   "true",
			wantIsField: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotField, gotIsField := SegmentFieldName(tt.input)
			if gotField != tt.wantField {
				t.Errorf("SegmentFieldName(%q) field = %q, want %q", tt.input, gotField, tt.wantField)
			}
			if gotIsField != tt.wantIsField {
				t.Errorf("SegmentFieldName(%q) isField = %v, want %v", tt.input, gotIsField, tt.wantIsField)
			}
		})
	}
}
