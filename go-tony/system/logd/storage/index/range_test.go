package index

import (
	"slices"
	"testing"
)

func TestRange(t *testing.T) {
	tr := NewTree[int](func(a, b int) bool { return a < b })

	// Test empty tree
	count := 0
	tr.Range(func(i int) bool {
		count++
		return true
	}, func(i int) int { return 0 })
	if count != 0 {
		t.Errorf("expected 0 items in empty tree, got %d", count)
	}

	// Insert 100 items
	n := 100
	for i := 0; i < n; i++ {
		tr.Insert(i)
	}

	tests := []struct {
		name     string
		min, max int // range [min, max)
		want     []int
	}{
		{
			name: "subset middle",
			min:  20, max: 50,
			want: makeRange(20, 50),
		},
		{
			name: "start",
			min:  0, max: 10,
			want: makeRange(0, 10),
		},
		{
			name: "end",
			min:  90, max: 100,
			want: makeRange(90, 100),
		},
		{
			name: "before start",
			min:  -10, max: 0,
			want: []int{},
		},
		{
			name: "after end",
			min:  100, max: 110,
			want: []int{},
		},
		{
			name: "all",
			min:  0, max: 100,
			want: makeRange(0, 100),
		},
		{
			name: "overlap start",
			min:  -10, max: 10,
			want: makeRange(0, 10),
		},
		{
			name: "overlap end",
			min:  90, max: 110,
			want: makeRange(90, 100),
		},
		{
			name: "single item",
			min:  5, max: 6,
			want: []int{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []int
			tr.Range(func(i int) bool {
				got = append(got, i)
				return true
			}, func(i int) int {
				if i < tt.min {
					return -1
				}
				if i >= tt.max {
					return 1
				}
				return 0
			})

			if !slices.Equal(got, tt.want) {
				t.Errorf("Range() = %v, want %v", got, tt.want)
			}
		})
	}
}

func makeRange(min, max int) []int {
	res := make([]int, 0, max-min)
	for i := min; i < max; i++ {
		res = append(res, i)
	}
	return res
}
