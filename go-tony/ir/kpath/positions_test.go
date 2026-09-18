package kpath

import (
	"fmt"
	"strings"
	"testing"
)

// names spells a position set for a test: the rest of the pattern at each position,
// "$" for the spent pattern.
func (ps Positions) names() string {
	out := make([]string, 0, len(ps.at))
	for _, x := range ps.at {
		if x.p == nil {
			out = append(out, "$")
			continue
		}
		name := x.p.String()
		if x.taken > 0 {
			name += fmt.Sprintf("@%d", x.taken)
		}
		out = append(out, name)
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
		if got := Start(kp, Unbounded).names(); got != tc.want {
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
	ps := Start(kp, Unbounded)
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
	// Unbounded, nothing is counted, so a descent reopened at every level is one
	// position and not one per level: the set is the pattern's size however deep the
	// walk goes.
	deep := Start(kp, Unbounded)
	for range 5 {
		deep = deep.Step(takesField("b"))
	}
	if got, want := deep.names(), "..b..c b..c ..c c"; got != want {
		t.Errorf("after five b = %q, want %q", got, want)
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
	ps := Start(kp, Unbounded).Step(takesField("a"))
	if got, want := ps.names(), "b"; got != want {
		t.Fatalf("after a = %q, want %q", got, want)
	}
	if dropped := ps.Step(takesField("x")); !dropped.Empty() {
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
	if next := ps.Step(func(*KPath) bool { asked = true; return true }); !next.Empty() || asked {
		t.Errorf("a spent pattern stepped: %q, asked %v", next.names(), asked)
	}
}

// A depth bounds every descent: a position at a `..` that has taken its depth is
// dropped, and what it had opened stays. So `..b` at depth 1 names a b at the root or
// under one child, and the walk has nothing to do beneath a child, which is what makes
// a bounded descent cost its levels rather than the subtree.
func TestPositions_Depth(t *testing.T) {
	kp, err := Parse("..b")
	if err != nil {
		t.Fatal(err)
	}
	ps := Start(kp, 1)
	if got, want := ps.names(), "..b b"; got != want {
		t.Fatalf("start = %q, want %q", got, want)
	}
	// Under x: the descent takes it, and has taken all it may; b is still live, and
	// carries the count, since the budget is the path's.
	ps = ps.Step(takesField("x"))
	if got, want := ps.names(), "..b@1 b@1"; got != want {
		t.Errorf("after x = %q, want %q", got, want)
	}
	if !ps.Live() {
		t.Error("x: b could still take a child, and the set is not live")
	}
	// Under x.b: b takes it; the descent is spent and dropped. Done, and nothing live.
	under := ps.Step(takesField("b"))
	if got, want := under.names(), "$"; got != want {
		t.Errorf("after x.b = %q, want %q", got, want)
	}
	if !under.Done() || under.Live() {
		t.Errorf("x.b: done %v live %v", under.Done(), under.Live())
	}
	// Under x.y: nothing takes it, and the descent may take no more: empty.
	if beside := ps.Step(takesField("y")); !beside.Empty() {
		t.Errorf("x.y kept positions %q", beside.names())
	}
	// Depth 0: the descent takes nothing, so `..b` is b at the root.
	zero := Start(kp, 0)
	if got, want := zero.names(), "..b b"; got != want {
		t.Fatalf("depth 0 start = %q, want %q", got, want)
	}
	if got := zero.Step(takesField("x")); !got.Empty() {
		t.Errorf("depth 0 under x kept %q", got.names())
	}
	// The budget is the path's, shared by its descents: `..b..c` at depth 1 under b
	// holds the second descent at nothing taken (the first took nothing, b took b)
	// and, through the first descent taking b, b..c at one taken.
	two, _ := Parse("..b..c")
	ps = Start(two, 1).Step(takesField("b"))
	if got, want := ps.names(), "..b..c@1 b..c@1 ..c c"; got != want {
		t.Errorf("..b..c at depth 1 after b = %q, want %q", got, want)
	}
	// Under a second b: the first descent is spent; b..c@1 takes b and opens the second
	// descent at one taken; the second descent at nothing taken takes b and is at one
	// too. One position for `..c`, at one taken.
	ps = ps.Step(takesField("b"))
	if got, want := ps.names(), "..c@1 c@1"; got != want {
		t.Errorf("..b..c at depth 1 after b, b = %q, want %q", got, want)
	}
	// And c under that is named, with the budget spent: a c any deeper is not.
	if under := ps.Step(takesField("c")); !under.Done() || under.Live() {
		t.Errorf("..b..c at depth 1 after b, b, c: done %v live %v, want done and not live", under.Done(), under.Live())
	}
	if beside := ps.Step(takesField("x")); !beside.Empty() {
		t.Errorf("..b..c at depth 1 after b, b, x kept %q: the budget was spent", beside.names())
	}
}

// One depth for the whole path: `..c..d` at depth 1 names X.c.d and c.Y.d but not
// X.c.Y.d, whose two descents took two segments between them.
func TestPositions_DepthIsOneBudget(t *testing.T) {
	kp, _ := Parse("..c..d")
	walk := func(fields ...string) Positions {
		ps := Start(kp, 1)
		for _, f := range fields {
			ps = ps.Step(takesField(f))
		}
		return ps
	}
	if !walk("c", "d").Done() || !walk("X", "c", "d").Done() || !walk("c", "Y", "d").Done() {
		t.Error("a d one segment off the path is not named")
	}
	if walk("X", "c", "Y", "d").Done() {
		t.Error("X.c.Y.d is named at depth 1: two descents took two segments between them")
	}
	if !walk("X", "c", "Y", "d").Empty() {
		t.Errorf("X.c.Y: still walking with the budget spent: %q", walk("X", "c", "Y").names())
	}
}

// A descent that has taken its depth is not live, so a walk has no reason to list
// beneath a node where nothing else is: `a..` at depth 1 lists a once and stops at
// its children, rather than listing each child to find nothing (gqk8t2h5h12ksse3ndn0).
func TestPositions_NotLiveAtTheBound(t *testing.T) {
	kp, _ := Parse("..")
	ps := Start(kp, 1)
	if !ps.Live() || !ps.Done() {
		t.Fatalf("start: live %v done %v", ps.Live(), ps.Done())
	}
	ps = ps.Step(takesField("x"))
	if !ps.Done() {
		t.Error("x is not named")
	}
	if ps.Live() {
		t.Errorf("x: the descent has taken its depth, and the set is live: %q", ps.names())
	}
	if !ps.Step(takesField("y")).Empty() {
		t.Error("x.y is on a path the pattern names")
	}
}

// CheckDepth is the one statement of what a depth may be.
func TestCheckDepth(t *testing.T) {
	for _, tc := range []struct {
		path  string
		depth int
		ok    bool
	}{
		{"a..", 0, true}, {"a..", 3, true},
		{"a.*", 1, false}, {"a.b", 0, false}, {"", 2, false},
		// A given -1 is a negative like any other, not the unbounded sentinel.
		{"a..", -1, false}, {"a..", -2, false}, {"a.b", -2, false},
	} {
		kp, err := Parse(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := CheckDepth(kp, tc.depth) == nil; got != tc.ok {
			t.Errorf("CheckDepth(%q, %d) ok = %v, want %v", tc.path, tc.depth, got, tc.ok)
		}
	}
}
