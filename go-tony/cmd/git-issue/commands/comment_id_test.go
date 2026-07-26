package commands

import (
	"sort"
	"testing"
	"time"
)

func TestCommentFileName_ShapeAndDeterminism(t *testing.T) {
	ts := time.Date(2026, 7, 26, 16, 5, 26, 0, time.FixedZone("CEST", 2*3600))
	a := commentFileName(ts, "<!-- x -->\n\nhello\n")
	b := commentFileName(ts, "<!-- x -->\n\nhello\n")
	if a != b {
		t.Fatalf("not deterministic: %q != %q", a, b)
	}
	if !isMigratedCommentName(a) {
		t.Fatalf("generated name %q does not match the migrated pattern", a)
	}
	// UTC-normalized timestamp prefix (16:05:26 CEST == 14:05:26 UTC).
	if got, want := a, "discussion/20260726T140526Z-"; got[:len(want)] != want {
		t.Fatalf("name %q does not start with %q", got, want)
	}
	// Different content -> different name (no collision on merge).
	if c := commentFileName(ts, "<!-- x -->\n\nworld\n"); c == a {
		t.Fatalf("distinct content produced the same name %q", c)
	}
}

func TestParseCommentTime_BothHeaderForms(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"legacy", "<!-- Comment 003 - 2026-07-26T02:34:17+02:00 -->\n\nbody\n", "2026-07-26T02:34:17+02:00"},
		{"new", "<!-- 2026-07-26T16:05:26+02:00 -->\n\nbody\n", "2026-07-26T16:05:26+02:00"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts, ok := parseCommentTime(tc.content)
			if !ok {
				t.Fatalf("failed to parse timestamp from %q", tc.content)
			}
			if got := ts.Format(time.RFC3339); got != tc.want {
				t.Fatalf("parsed %s, want %s", got, tc.want)
			}
		})
	}
	if _, ok := parseCommentTime("no header here"); ok {
		t.Fatalf("parsed a timestamp from content with no header")
	}
}

// TestCommentOrdering_ByTimestamp guards the show fix: comments sort
// chronologically by embedded timestamp regardless of filename order.
func TestCommentOrdering_ByTimestamp(t *testing.T) {
	type c struct {
		path, content string
	}
	// Deliberately out of chronological order, and content-addressed names that do
	// NOT sort chronologically by filename.
	items := []c{
		{"discussion/20260726T143259Z-ffff0000.md", "<!-- 2026-07-26T16:32:59+02:00 -->\n\nthird\n"},
		{"discussion/20260726T113324Z-00001111.md", "<!-- 2026-07-26T01:33:24+02:00 -->\n\nfirst\n"},
		{"discussion/20260726T003417Z-aaaa2222.md", "<!-- 2026-07-26T02:34:17+02:00 -->\n\nsecond\n"},
	}
	sort.SliceStable(items, func(i, j int) bool {
		ti, oki := parseCommentTime(items[i].content)
		tj, okj := parseCommentTime(items[j].content)
		if oki && okj && !ti.Equal(tj) {
			return ti.Before(tj)
		}
		return items[i].path < items[j].path
	})
	order := []string{}
	for _, it := range items {
		body := it.content
		order = append(order, body[len(body)-6:len(body)-1]) // the word before trailing \n
	}
	want := []string{"first", "econd", "third"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("chronological order wrong: got %v", order)
		}
	}
}
