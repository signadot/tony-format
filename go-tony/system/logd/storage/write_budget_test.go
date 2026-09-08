package storage

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// The write budget is the largest node the write path builds to verify one write: the
// value at a path the write names. An operation whose site is a large subtree -- a
// !replace of it, an !all over it -- asks for that subtree as its intermediate, and past
// the budget it is refused, naming the path and the budget. A plain merge at a leaf reads
// the leaf, whatever the document around it weighs, and is never refused for size.
func TestWriteBudgetRefusesAWideOperationAndAdmitsANarrowOne(t *testing.T) {
	s := openTestStorage(t)
	for i := 0; i < 40; i++ {
		subtreeWrite(t, s, "verse.entities.e"+strconv.Itoa(i), "{id: e"+strconv.Itoa(i)+", blob: "+strings.Repeat("x", 200)+"}")
	}
	s.SetWriteBudget(2048)

	err := scopedCommit(t, s, nil, "verse.entities", `!replace {from: {}, to: {}}`)
	var wb *WriteBudgetError
	if !errors.As(err, &wb) {
		t.Fatalf("a !replace of a subtree past the budget was not refused for size: %v", err)
	}
	if wb.Path != "verse.entities" || wb.Budget != 2048 || !strings.Contains(err.Error(), "!replace") {
		t.Errorf("the refusal does not name the path, the operation and the budget: %v", err)
	}
	if !errors.Is(err, ErrBudget) {
		t.Errorf("the refusal is not an ErrBudget: %v", err)
	}

	// The same kind of change, narrower: one element, well within the budget.
	if err := scopedCommit(t, s, nil, "verse.entities.e7", `{blob: !replace {from: "`+strings.Repeat("x", 200)+`", to: "y"}}`); err != nil {
		t.Fatalf("a narrow relative write was refused: %v", err)
	}
	// A plain merge at a leaf under the large subtree reads only the leaf.
	if err := scopedCommit(t, s, nil, "verse.entities.e8.blob", `"z"`); err != nil {
		t.Fatalf("a plain merge at a leaf was refused: %v", err)
	}
	// A merge at the root naming two leaves reads two leaves, not the document.
	if err := scopedCommit(t, s, nil, "", `{verse: {entities: {e1: {blob: "a"}, e2: {blob: "b"}}}}`); err != nil {
		t.Fatalf("a merge naming two leaves was refused: %v", err)
	}
	c, _ := s.GetCurrentCommit()
	doc, err := readStateAt(s, "", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ kp, want string }{{"verse.entities.e7.blob", "y"}, {"verse.entities.e8.blob", "z"}, {"verse.entities.e1.blob", "a"}} {
		if v, _ := doc.GetKPath(tc.kp); v == nil || v.String != tc.want {
			t.Errorf("%s = %v, want %q", tc.kp, v, tc.want)
		}
	}
}

// A precondition reads the value at its own path, under the same budget.
func TestPreconditionIsABoundedRead(t *testing.T) {
	s := openTestStorage(t)
	for i := 0; i < 40; i++ {
		subtreeWrite(t, s, "verse.entities.e"+strconv.Itoa(i), "{id: e"+strconv.Itoa(i)+", blob: "+strings.Repeat("x", 200)+"}")
	}
	s.SetWriteBudget(2048)
	before := s.ReadStats()
	if err := casWriteAt(t, s, "verse.entities.e3.id", `e3`, "verse.entities.e3.note", `hi`); err != nil {
		t.Fatalf("a precondition at a leaf was refused: %v", err)
	}
	after := s.ReadStats()
	if after.WideRoot != before.WideRoot {
		t.Errorf("a precondition at a leaf read the root (%d -> %d)", before.WideRoot, after.WideRoot)
	}
	err := casWriteAt(t, s, "verse.entities", `{e3: {id: e3}}`, "verse.entities.e3.note", `again`)
	var wb *WriteBudgetError
	if !errors.As(err, &wb) {
		t.Fatalf("a precondition over a subtree past the budget was not refused for size: %v", err)
	}
}

// A write to one leaf reads that leaf to verify itself, and nothing wider: the counters
// show a narrow read per write and no read at the root.
func TestAWriteReadsOnlyWhatItWrites(t *testing.T) {
	s, paths := shapedStore(t, shape{paths: 60, writesPerPath: 2, snapshotEvery: 50,
		ancestors: []string{"verse.git.ref", "verse.github.issue"}})
	before := s.ReadStats()
	for i := 0; i < 10; i++ {
		if err := scopedCommit(t, s, nil, paths[i]+".v", strconv.Itoa(100+i)); err != nil {
			t.Fatal(err)
		}
	}
	after := s.ReadStats()
	if after.WideRoot != before.WideRoot {
		t.Errorf("ten leaf writes read the root %d time(s)", after.WideRoot-before.WideRoot)
	}
	if narrow := after.Narrow - before.Narrow; narrow < 10 || narrow > 20 {
		t.Errorf("ten leaf writes performed %d narrow reads; want one or two each", narrow)
	}
	if after.LargestRecord > 4096 {
		t.Errorf("the largest record held across ten leaf writes was %d bytes", after.LargestRecord)
	}
}
