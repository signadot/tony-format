package kpath

import (
	"testing"
)

func TestKPath_SegmentString(t *testing.T) {
	tests := []struct {
		name     string
		kpath    *KPath
		expected string
	}{
		{
			name:     "nil KPath",
			kpath:    nil,
			expected: "",
		},
		{
			name: "field segment",
			kpath: &KPath{
				Field: stringPtr("a"),
			},
			expected: "a",
		},
		{
			name: "quoted field segment",
			kpath: &KPath{
				Field: stringPtr("field name"),
			},
			expected: `"field name"`, // token.Quote uses double quotes
		},
		{
			name: "dense array index",
			kpath: &KPath{
				Index: intPtr(0),
			},
			expected: "[0]",
		},
		{
			name: "dense array index large",
			kpath: &KPath{
				Index: intPtr(42),
			},
			expected: "[42]",
		},
		{
			name: "sparse array index",
			kpath: &KPath{
				SparseIndex: intPtr(5),
			},
			expected: "{5}",
		},
		{
			name: "sparse array index large",
			kpath: &KPath{
				SparseIndex: intPtr(100),
			},
			expected: "{100}",
		},
		{
			name: "field wildcard",
			kpath: &KPath{
				FieldAll: true,
			},
			expected: "*",
		},
		{
			name: "array wildcard",
			kpath: &KPath{
				IndexAll: true,
			},
			expected: "[*]",
		},
		{
			name: "sparse array wildcard",
			kpath: &KPath{
				SparseIndexAll: true,
			},
			expected: "{*}",
		},
		{
			name: "key",
			kpath: &KPath{
				Key: stringPtr("jane"),
			},
			expected: "(jane)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.kpath.SegmentString()
			if got != tt.expected {
				t.Errorf("SegmentString() = %q, want %q", got, tt.expected)
			}
		})
	}
}
