package issuelib

import (
	"encoding/binary"
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

	// The counter mix is a bijection on 24 bits, so it cannot merge two counters.
	var hit [1 << 24 / 8]byte
	for c := uint32(0); c < 1<<24; c++ {
		m := mixCounter(c)
		if m > 0xFFFFFF || hit[m/8]&(1<<(m%8)) != 0 {
			t.Fatalf("mixCounter(%#x) = %#x collides or overflows", c, m)
		}
		hit[m/8] |= 1 << (m % 8)
	}

	// Characters of the XIDR that come from the counter's upper bytes and
	// from the pid; without mixing both would be fixed across a batch.
	counterChars := make(map[string]bool)
	pidChars := make(map[string]bool)

	// Generate many XIDs at the same timestamp
	for i := 0; i < 1000; i++ {
		x := NewXID(now)
		s := x.String()
		if seen[s] {
			t.Fatalf("duplicate XID generated: %s", s)
		}
		seen[s] = true

		// The mask is a function of fields the id keeps, so the pid comes back out.
		ts := binary.BigEndian.Uint32(x[0:4])
		c := uint32(x[9])<<16 | uint32(x[10])<<8 | uint32(x[11])
		if got := binary.BigEndian.Uint16(x[7:9]) ^ pidMask(ts, c); got != pid {
			t.Fatalf("%s: unmasked pid %d, want %d", s, got, pid)
		}
		counterChars[x.XIDR()[2:4]] = true
		pidChars[x.XIDR()[5:7]] = true
	}
	// One process's ids should not share a stem: these characters vary.
	if len(counterChars) < 500 {
		t.Errorf("1000 ids carry only %d distinct counter characters", len(counterChars))
	}
	if len(pidChars) < 500 {
		t.Errorf("1000 ids carry only %d distinct pid characters", len(pidChars))
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
