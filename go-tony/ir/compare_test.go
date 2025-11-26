package ir

import (
	"testing"
)

func TestCompare(t *testing.T) {
	tests := []struct {
		name     string
		a, b     *Node
		expected int
	}{
		// Type Ranking: Comment < Null < Bool < Number < String < Array < Object
		{"Comment < Null", &Node{Type: CommentType}, &Node{Type: NullType}, -1},
		{"Null < Bool", &Node{Type: NullType}, FromBool(false), -1},
		{"Bool < Number", FromBool(true), FromInt(1), -1},
		{"Number < String", FromInt(1), FromString("a"), -1},
		{"String < Array", FromString("a"), FromSlice(nil), -1},
		{"Array < Object", FromSlice(nil), FromKeyVals(nil), -1},

		// Bool Comparison
		{"false < true", FromBool(false), FromBool(true), -1},
		{"true > false", FromBool(true), FromBool(false), 1},
		{"true == true", FromBool(true), FromBool(true), 0},

		// Number Comparison: Int < Float < String
		{"Int < Float", FromInt(1), FromFloat(1.0), -1},
		{"Float < StringNum", FromFloat(1.0), &Node{Type: NumberType, Number: "1"}, -1},
		{"Int < Int", FromInt(1), FromInt(2), -1},
		{"Float < Float", FromFloat(1.0), FromFloat(2.0), -1},
		{"StringNum < StringNum", &Node{Type: NumberType, Number: "1"}, &Node{Type: NumberType, Number: "2"}, -1},

		// String Comparison: MergeKey < StringKey
		{"MergeKey < String", FromString("<<"), FromString("a"), -1},
		{"String > MergeKey", FromString("a"), FromString("<<"), 1},
		{"String < String", FromString("a"), FromString("b"), -1},

		// Array Comparison
		{"Empty Array == Empty Array", FromSlice(nil), FromSlice(nil), 0},
		{"Short Array < Long Array", FromSlice([]*Node{FromInt(1)}), FromSlice([]*Node{FromInt(1), FromInt(2)}), -1},
		{"Array Element Comparison", FromSlice([]*Node{FromInt(1)}), FromSlice([]*Node{FromInt(2)}), -1},

		// Object Comparison
		{"Empty Object == Empty Object", FromKeyVals(nil), FromKeyVals(nil), 0},
		{"Short Object < Long Object",
			FromKeyVals([]KeyVal{{Key: FromString("a"), Val: FromInt(1)}}),
			FromKeyVals([]KeyVal{{Key: FromString("a"), Val: FromInt(1)}, {Key: FromString("b"), Val: FromInt(2)}}),
			-1},
		{"Object Key Comparison",
			FromKeyVals([]KeyVal{{Key: FromString("a"), Val: FromInt(1)}}),
			FromKeyVals([]KeyVal{{Key: FromString("b"), Val: FromInt(1)}}),
			-1},
		{"Object Value Comparison",
			FromKeyVals([]KeyVal{{Key: FromString("a"), Val: FromInt(1)}}),
			FromKeyVals([]KeyVal{{Key: FromString("a"), Val: FromInt(2)}}),
			-1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compare(tt.a, tt.b); got != tt.expected {
				t.Errorf("Compare() = %v, want %v", got, tt.expected)
			}
			// Test symmetry
			if got := Compare(tt.b, tt.a); got != -tt.expected {
				t.Errorf("Compare(b, a) = %v, want %v", got, -tt.expected)
			}
		})
	}
}
