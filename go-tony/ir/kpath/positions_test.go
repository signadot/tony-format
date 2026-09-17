package kpath

import (
	"strings"
	"testing"
)

// names spells a position set for a test: the rest of the pattern at each position,
// "$" for the spent pattern.
func (ps Positions) names() string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		if p == nil {
			out = append(out, "$")
			continue
		}
		out = append(out, p.String())
	}
	return strings.Join(out, " ")
}

// takesField is a walker's answer for a child that is the field named.
func takesField(name string) func(*KPath) bool {
	return func(seg *KPath) bool {
		return seg.FieldAll || (seg.Field != nil && *seg.Field == name)
	}
}

// A position set is closed over `..`: at a descent it also holds what follows, since
// a descent may take nothing, and a run of descents closes through all of them.
func TestPositions_StartClosesOverDescents(t *testing.T) {
	for _, tc := range []struct{ pattern, want string }{
		{"a.b", "a.b"},
		{"..b", "..b b"},
		{"..", ".. $"},
		{"....b", "....b ..b b"},
		{"a..", "a.."},
	} {
		kp, err := Parse(tc.pattern)
		if err != nil {
			t.Fatal(err)
		}
		if got := Start(kp).names(); got != tc.want {
			t.Errorf("Start(%q) = %q, want %q", tc.pattern, got, tc.want)
		}
	}
}

// A step keeps every descent (it absorbs the child) and advances every segment that
// takes the child, closing over what it advances to; what does not take the child
// is dropped.
func TestPositions_Step(t *testing.T) {
	kp, err := Parse("..b..c")
	if err != nil {
		t.Fatal(err)
	}
	ps := Start(kp)
	if got, want := ps.names(), "..b..c b..c"; got != want {
		t.Fatalf("start = %q, want %q", got, want)
	}
	if ps.Done() {
		t.Error("done before any b")
	}
	// Under x: the descent absorbs it; b does not take it.
	ps = ps.Step(takesField("x"))
	if got, want := ps.names(), "..b..c b..c"; got != want {
		t.Errorf("after x = %q, want %q", got, want)
	}
	// Under b: the descent absorbs it, AND b takes it, opening the second descent.
	ps = ps.Step(takesField("b"))
	if got, want := ps.names(), "..b..c b..c ..c c"; got != want {
		t.Errorf("after b = %q, want %q", got, want)
	}
	if ps.Done() {
		t.Error("done at b: the pattern wants a c")
	}
	// Under c: matched, and every descent still open.
	ps = ps.Step(takesField("c"))
	if got, want := ps.names(), "..b..c b..c ..c c $"; got != want {
		t.Errorf("after c = %q, want %q", got, want)
	}
	if !ps.Done() || !ps.Live() {
		t.Errorf("after c: done %v live %v, want both", ps.Done(), ps.Live())
	}
}

// Without a descent the set is one position, and a step that nothing takes empties it:
// the child is not on any path the pattern names.
func TestPositions_SpentAndDropped(t *testing.T) {
	kp, err := Parse("a.b")
	if err != nil {
		t.Fatal(err)
	}
	ps := Start(kp).Step(takesField("a"))
	if got, want := ps.names(), "b"; got != want {
		t.Fatalf("after a = %q, want %q", got, want)
	}
	if dropped := ps.Step(takesField("x")); len(dropped) != 0 {
		t.Errorf("x kept positions %q", dropped.names())
	}
	ps = ps.Step(takesField("b"))
	if got, want := ps.names(), "$"; got != want {
		t.Fatalf("after b = %q, want %q", got, want)
	}
	if !ps.Done() || ps.Live() {
		t.Errorf("spent: done %v live %v", ps.Done(), ps.Live())
	}
	// A spent pattern takes nothing, and takes is not asked.
	asked := false
	if next := ps.Step(func(*KPath) bool { asked = true; return true }); len(next) != 0 || asked {
		t.Errorf("a spent pattern stepped: %q, asked %v", next.names(), asked)
	}
}
