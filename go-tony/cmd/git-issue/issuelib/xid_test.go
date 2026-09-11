package issuelib

import (
	"testing"
	"time"
)

func TestXIDPrefixMatching(t *testing.T) {
	x := NewXID(time.Now())
	xidr := x.XIDR()

	// Full match
	if !MatchesXIDRPrefix(xidr, xidr) {
		t.Error("full string should match itself as prefix")
	}

	// Prefix matches
	for i := 1; i <= len(xidr); i++ {
		prefix := xidr[:i]
		if !MatchesXIDRPrefix(prefix, xidr) {
			t.Errorf("prefix %q should match %q", prefix, xidr)
		}
	}

	// Non-match
	if MatchesXIDRPrefix("zzzzz", xidr) {
		t.Error("non-matching prefix should not match")
	}

	// Case insensitive
	if !MatchesXIDRPrefix("ABC", "abcdef01234567890123") {
		t.Error("prefix matching should be case-insensitive")
	}
}

func TestXIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	now := time.Now()

	// Generate many XIDs at the same timestamp
	for i := 0; i < 1000; i++ {
		x := NewXID(now)
		s := x.String()
		if seen[s] {
			t.Fatalf("duplicate XID generated: %s", s)
		}
		seen[s] = true
	}
}

func TestXIDChronologicalOrder(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	x1 := NewXID(t1)
	x2 := NewXID(t2)
	x3 := NewXID(t3)

	// Normal XIDs should sort chronologically (older first)
	if x1.String() >= x2.String() {
		t.Errorf("x1 should be < x2: %s >= %s", x1.String(), x2.String())
	}
	if x2.String() >= x3.String() {
		t.Errorf("x2 should be < x3: %s >= %s", x2.String(), x3.String())
	}

	// Reversed XIDs sort in reverse chronological order
	// (because counter bytes come first, but for different timestamps,
	// we need to consider that xidr puts timestamp at the end)
}
