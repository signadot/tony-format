package issuelib

import (
	"testing"
	"time"
)

func TestXIDRoundTrip(t *testing.T) {
	now := time.Now()
	x := NewXID(now)

	// Test String() round-trip
	s := x.String()
	if len(s) != 20 {
		t.Errorf("expected 20 chars, got %d: %s", len(s), s)
	}

	parsed, err := ParseXID(s)
	if err != nil {
		t.Fatalf("ParseXID failed: %v", err)
	}
	if parsed != x {
		t.Errorf("round-trip failed: got %v, want %v", parsed, x)
	}

	// Verify timestamp preserved (within 1 second due to precision)
	if parsed.Time().Unix() != now.Unix() {
		t.Errorf("timestamp mismatch: got %v, want %v", parsed.Time().Unix(), now.Unix())
	}
}

func TestXIDRRoundTrip(t *testing.T) {
	now := time.Now()
	x := NewXID(now)

	xidr := x.XIDR()
	if len(xidr) != 20 {
		t.Errorf("expected 20 chars, got %d: %s", len(xidr), xidr)
	}

	// Reversed should be different from normal
	if xidr == x.String() {
		t.Error("xidr should differ from normal encoding")
	}

	// Round-trip through ParseXIDR
	parsed, err := ParseXIDR(xidr)
	if err != nil {
		t.Fatalf("ParseXIDR failed: %v", err)
	}
	if parsed != x {
		t.Errorf("xidr round-trip failed: got %v, want %v", parsed, x)
	}
}

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

func TestValidXIDPrefix(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"abc123", true},
		{"ABC123", true},
		{"0123456789", true},
		{"abcdefghjkmnpqrstvwxyz", true}, // all valid chars
		{"", false},                       // empty
		{"abci", false},                   // 'i' is invalid
		{"abcl", false},                   // 'l' is invalid
		{"abco", false},                   // 'o' is invalid
		{"abcu", false},                   // 'u' is invalid
		{"abc-123", false},                // hyphen invalid
	}

	for _, tt := range tests {
		got := IsValidXIDRPrefix(tt.input)
		if got != tt.valid {
			t.Errorf("IsValidXIDRPrefix(%q) = %v, want %v", tt.input, got, tt.valid)
		}
	}
}
