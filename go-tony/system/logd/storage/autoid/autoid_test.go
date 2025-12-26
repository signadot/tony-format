package autoid

import (
	"testing"
)

func TestGenerate(t *testing.T) {
	tests := []struct {
		commit int64
		index  int
		want   string
	}{
		{1, 0, "a1a0"},
		{1, 1, "a1a1"},
		{1, 2, "a1a2"},
		{2, 0, "a2a0"},
		{15, 0, "afa0"},   // 0xf
		{16, 0, "b10a0"},  // 0x10
		{255, 0, "bffa0"}, // 0xff
		{256, 0, "c100a0"}, // 0x100
		{256, 5, "c100a5"},
		{1000000, 0, "ef4240a0"},   // 0xf4240 = 5 hex digits
		{1000000, 5, "ef4240a5"},
		{1000000, 99, "ef4240b63"}, // 99 = 0x63
	}

	var prev string
	for _, tt := range tests {
		id := Generate(tt.commit, tt.index)
		t.Logf("commit=%d, index=%d -> %q", tt.commit, tt.index, id)

		if id != tt.want {
			t.Errorf("Generate(%d, %d) = %q, want %q", tt.commit, tt.index, id, tt.want)
		}

		// Verify monotonicity: each ID should be > previous
		if prev != "" && id <= prev {
			t.Errorf("IDs not monotonic: %q <= %q", id, prev)
		}
		prev = id
	}
}

func TestGenerateUniqueness(t *testing.T) {
	seen := make(map[string]bool)

	// Generate many IDs and verify uniqueness
	for commit := int64(1); commit <= 100; commit++ {
		for index := 0; index < 10; index++ {
			id := Generate(commit, index)
			if seen[id] {
				t.Errorf("Duplicate ID: %q (commit=%d, index=%d)", id, commit, index)
			}
			seen[id] = true
		}
	}
}

func TestGenerateMonotonicity(t *testing.T) {
	// Verify that IDs sort correctly
	ids := make([]string, 0, 1000)

	for commit := int64(1); commit <= 100; commit++ {
		for index := 0; index < 10; index++ {
			ids = append(ids, Generate(commit, index))
		}
	}

	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("IDs not monotonic at position %d: %q <= %q", i, ids[i], ids[i-1])
		}
	}
}

func TestGenerateValidTonyLiteral(t *testing.T) {
	// Verify IDs are valid Tony literals (start with letter)
	for commit := int64(0); commit < 1000; commit += 100 {
		id := Generate(commit, 0)
		if len(id) == 0 {
			t.Errorf("Generate(%d, 0) returned empty string", commit)
			continue
		}
		first := id[0]
		if first < 'a' || first > 'z' {
			t.Errorf("ID %q doesn't start with lowercase letter", id)
		}
	}
}

func TestFormatLex(t *testing.T) {
	tests := []struct {
		n    int64
		want string
	}{
		{0, "a0"},
		{1, "a1"},
		{15, "af"},    // 0xf
		{16, "b10"},   // 0x10
		{255, "bff"},  // 0xff
		{256, "c100"}, // 0x100
		{4095, "cfff"}, // 0xfff
		{4096, "d1000"}, // 0x1000
		{0xffffffff, "hffffffff"},  // 8 hex digits
		{0x7fffffffffffffff, "p7fffffffffffffff"}, // int64 max (16 hex digits)
	}

	for _, tt := range tests {
		got := formatLex(tt.n)
		if got != tt.want {
			t.Errorf("formatLex(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}

	// Verify lexicographic ordering
	var prev string
	for _, tt := range tests {
		got := formatLex(tt.n)
		if prev != "" && got <= prev {
			t.Errorf("formatLex not monotonic: formatLex(%d)=%q <= prev=%q", tt.n, got, prev)
		}
		prev = got
	}
}
