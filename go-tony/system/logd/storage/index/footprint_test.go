package index

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/storage/internal/dlog"
)

// footWriter indexes whole-document scope entries at successive commits, so a test can
// say a stream and read the footprint after it.
type footWriter struct {
	t      *testing.T
	idx    *Index
	scope  string
	commit int64
}

func newFootWriter(t *testing.T, scope string) *footWriter {
	return &footWriter{t: t, idx: NewIndex(""), scope: scope}
}

func (w *footWriter) write(src string) int64 {
	w.t.Helper()
	n, err := parse.Parse([]byte(src), parse.ParseComments(true))
	if err != nil {
		w.t.Fatalf("parse %q: %v", src, err)
	}
	w.commit++
	last := w.commit - 1
	e := &dlog.Entry{Commit: w.commit, LastCommit: &last, Patch: n, ScopeID: &w.scope}
	sc := w.scope
	IndexPatch(w.idx, e, "A", w.commit*100, w.commit, 1, n, &sc)
	return w.commit
}

// live renders the footprint's live statements for a read at kp as "path@commit".
func (w *footWriter) live(kp string) string {
	var parts []string
	for _, st := range w.idx.Footprint().Live(w.scope, kp) {
		parts = append(parts, fmt.Sprintf("%s@%d", st.Path, st.Commit))
	}
	return strings.Join(parts, " ")
}

// The cover rules, as what is live after each write (the rows of the compaction test,
// decided at the write instead of at the compaction).
func TestFootprintKeepsWhatNoLaterStatementRetires(t *testing.T) {
	w := newFootWriter(t, "s1")
	for i := 1; i <= 100; i++ {
		w.write(fmt.Sprintf(`{a: {x: %d}}`, i))
	}
	if got := w.live("a.x"); got != "a.x@100" {
		t.Errorf("a hundred writes at a.x leave %q live, want the last", got)
	}
	w.write(`{a: {y: !delete null}}`) // 101, total at a.y
	w.write("keep:\n  # why\n  7")    // 102, commented: needs total
	w.write(`{keep: 8}`)              // 103, whole at keep: does not retire the comment
	w.write(`{arr: [3, 4, 5]}`)       // 104, plain array: whole
	w.write(`{arr: [6]}`)             // 105, retires 104
	if got := w.live(""); got != "a.x@100 a.y@101 keep@102 keep@103 arr@105" {
		t.Errorf("live at the root: %q", got)
	}
	w.write(`{a: !insert.raw {x: 0, z: !glob "*"}}`) // 106, total at a: retires a.x and a.y
	if got := w.live("a"); got != "a@106" {
		t.Errorf("after a claim at a, live under a: %q, want the claim alone", got)
	}
	if got := w.live("a.x"); got != "a@106" {
		t.Errorf("a read at a.x under the claim sees %q", got)
	}
	if got := w.live("keep"); got != "keep@102 keep@103" {
		t.Errorf("the claim at a touched keep: %q", got)
	}
	if !w.idx.Footprint().TotallyCovered("s1", "a.anything") {
		t.Errorf("a.anything is not totally covered under the claim at a")
	}
	if w.idx.Footprint().TotallyCovered("s1", "keep") {
		t.Errorf("keep reads as totally covered")
	}
	if !w.idx.Footprint().Reaches("s1", "keep") || w.idx.Footprint().Reaches("s1", "nowhere") {
		t.Errorf("Reaches is wrong about keep or nowhere")
	}
	st := w.idx.Footprint().Stats()
	if st.Scopes != 1 || st.Statements != 4 {
		t.Errorf("stats %+v, want 1 scope with 4 live statements", st)
	}
	if st.Statements != 4 {
		t.Errorf("stats %+v", st)
	}
}

// A statement arriving AFTER one that dominates it -- the order a rebuild or a re-index
// may present -- is not added, and nothing dropped comes back.
func TestFootprintDoesNotResurrectADominatedStatement(t *testing.T) {
	idx := NewIndex("")
	sc := "s1"
	older := Statement{Path: "a.x", Commit: 5, LogFile: "A", LogPosition: 500, Offers: CoverWhole, Needs: CoverWhole}
	newer := Statement{Path: "a", Commit: 9, LogFile: "A", LogPosition: 900, Offers: CoverTotal, Needs: CoverWhole}
	idx.foot.state(sc, newer)
	if idx.foot.state(sc, older) {
		t.Fatalf("a statement older than a total cover above it was added")
	}
	if got := len(idx.foot.Live(sc, "")); got != 1 {
		t.Errorf("%d live, want the cover alone", got)
	}
	if idx.foot.EntryLive(sc, "A", 500) || !idx.foot.EntryLive(sc, "A", 900) {
		t.Errorf("EntryLive is wrong")
	}
	// The same statement re-stated, as a re-index does after forgetting it, is one.
	idx.foot.forget(sc, newer)
	if idx.foot.EntryLive(sc, "A", 900) {
		t.Errorf("forgotten and still live")
	}
	idx.foot.state(sc, newer)
	if got := len(idx.foot.Live(sc, "")); got != 1 {
		t.Errorf("%d live after forget and re-state, want 1", got)
	}
}

// A node wearing a head comment is a statement at its path, as the fold reads it, and a
// plain container is not.
func TestACommentedContainerIsAStatement(t *testing.T) {
	n, err := parse.Parse([]byte("a:\n  # note\n  x: 1\nb:\n  y: 2"), parse.ParseComments(true))
	if err != nil {
		t.Fatal(err)
	}
	last := int64(0)
	e := &dlog.Entry{Commit: 1, LastCommit: &last, Patch: n}
	stated := map[string]bool{}
	spine := map[string]bool{}
	EachSegment(e, "A", 0, 1, func(seg *LogSegment) {
		stated[seg.KindedPath] = seg.Statement
		spine[seg.KindedPath] = seg.Spine
	})
	if !stated["a"] || spine["a"] {
		t.Errorf("the commented container at a: statement=%v spine=%v", stated["a"], spine["a"])
	}
	if stated["b"] || !spine["b"] {
		t.Errorf("the plain container at b: statement=%v spine=%v", stated["b"], spine["b"])
	}
	if !stated["a.x"] || !stated["b.y"] {
		t.Errorf("the leaves are not statements: %v", stated)
	}
}
