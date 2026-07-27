package storage

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// Differential test for the read path.
//
// THE ORACLE. Two storages get the identical write stream, so identical commit
// numbers, and differ in one thing:
//
//	A (reference) never snapshots. Every read therefore finds no snapshot base, and
//	  ApplyPatches takes its empty-base branch (processor.go): apply every entry from
//	  commit 0 in order via tony.Patch. That branch is the semantics of record — it is
//	  what reads below the root did for the life of the code before the snapshot lookup
//	  was fixed, and it is O(history) per read, which is why it is a reference and not a
//	  read path.
//	B (subject) snapshots at generated points, so its reads go through
//	  findSnapshotBaseReader + the streaming processor's patch-root matching.
//
// Any A/B divergence is a bug in the snapshot read path. Neither instance compacts:
// compaction is deliberately lossy beyond its cutoff, so it cannot be judged by
// equality and gets its own comparison (A vs C, recent reads only) elsewhere.
//
// Reads are compared at every commit and every path, including paths never written,
// because the failures found by hand all needed a specific SHAPE rather than a specific
// value: an ancestor-rooted write and a descendant-rooted write in the same replay
// range (mixed patch-root depth), and an op at an ancestor of the read path. Uniform
// depth — every write at "", or every write at one deep path — hides both, which is
// what the pre-existing snapshot tests do.
// seedCount defaults to a quick run; LOGD_SEEDS=500 for a soak.
func seedCount() int {
	if v := os.Getenv("LOGD_SEEDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 25
}

// writePaths are deliberately overlapping: ancestors, descendants and siblings of one
// another, so a generated stream mixes patch-root depths within a replay range.
var writePaths = []string{"", "a", "a.b", "a.b.c", "a.x", "d", "d.e"}

// readPaths includes a path nothing ever writes: a read there must still agree.
var readPaths = []string{"", "a", "a.b", "a.b.c", "a.x", "d", "d.e", "never.written"}

type genOp struct {
	path     string
	src      string
	snapshot bool // take a snapshot AFTER this op (subject only)
}

func (o genOp) String() string {
	if o.snapshot {
		return fmt.Sprintf("%s <- %s [snapshot]", quotePath(o.path), o.src)
	}
	return fmt.Sprintf("%s <- %s", quotePath(o.path), o.src)
}

func quotePath(p string) string {
	if p == "" {
		return `""`
	}
	return p
}

func genOps(rng *rand.Rand, n int) []genOp {
	ops := make([]genOp, 0, n)
	for i := 0; i < n; i++ {
		path := writePaths[rng.Intn(len(writePaths))]
		var src string
		switch rng.Intn(10) {
		case 0:
			// Delete at this path. Deleting the whole document is legal but makes for a
			// dull stream, so skip it at the root.
			if path == "" {
				src = fmt.Sprintf(`{k%d: %d}`, rng.Intn(3), i)
			} else {
				src = `!delete`
			}
		case 1, 2:
			src = fmt.Sprintf(`{k%d: {nested: %d}}`, rng.Intn(3), i)
			// NOTE: !replace is absent on purpose — it takes {from:, to:} and the from:
			// must match the current value, so generating it needs a state-tracking
			// generator. Worth adding once the op-reduction path exists: an op at an
			// ancestor of the read path is exactly the case step 4 has to handle.
		default:
			src = fmt.Sprintf(`{k%d: %d}`, rng.Intn(3), i)
		}
		ops = append(ops, genOp{path: path, src: src, snapshot: rng.Intn(8) == 0})
	}
	return ops
}

// applyOp commits one generated op and returns the commit number.
func applyOp(t *testing.T, s *Storage, o genOp) (int64, error) {
	t.Helper()
	n, err := parse.Parse([]byte(o.src))
	if err != nil {
		t.Fatalf("parse %q: %v", o.src, err)
	}
	txn, err := s.NewTx(1, nil)
	if err != nil {
		return 0, err
	}
	p, err := txn.NewPatcher(&api.Patch{PathData: api.PathData{Path: o.path, Data: n}})
	if err != nil {
		return 0, err
	}
	res := p.Commit()
	if !res.Committed {
		return 0, res.Error
	}
	return res.Commit, nil
}

func nodeText(n *ir.Node) string {
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b); err != nil {
		return "<encode error: " + err.Error() + ">"
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// readResult is a read outcome, so an error on one side and a value on the other
// counts as a divergence rather than a panic.
type readResult struct {
	node *ir.Node
	err  error
}

func read(s *Storage, kp string, commit int64) readResult {
	n, err := s.ReadStateAt(kp, commit, nil)
	return readResult{node: n, err: err}
}

func (r readResult) equal(o readResult) bool {
	if (r.err == nil) != (o.err == nil) {
		return false
	}
	if r.err != nil {
		return r.err.Error() == o.err.Error()
	}
	if r.node == nil || o.node == nil {
		return r.node == nil && o.node == nil
	}
	return r.node.DeepEqual(o.node)
}

// orderOnly reports whether the two results hold the same content and differ only in
// object field order. Tony preserves field order, so this is still a divergence — a read
// should not depend on whether a snapshot happens to exist — but it is a different
// finding from losing or reverting a value, and worth naming as such.
func (r readResult) orderOnly(o readResult) bool {
	if r.err != nil || o.err != nil || r.node == nil || o.node == nil {
		return false
	}
	return canonicalNode(r.node).DeepEqual(canonicalNode(o.node))
}

// canonicalNode returns a copy with every object's fields sorted by key.
func canonicalNode(n *ir.Node) *ir.Node {
	c := n.Clone()
	sortFieldsRecursive(c)
	return c
}

func sortFieldsRecursive(n *ir.Node) {
	if n == nil {
		return
	}
	for _, v := range n.Values {
		sortFieldsRecursive(v)
	}
	if n.Type != ir.ObjectType || len(n.Fields) != len(n.Values) {
		return
	}
	idx := make([]int, len(n.Fields))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		return fieldKey(n.Fields[idx[a]]) < fieldKey(n.Fields[idx[b]])
	})
	fields := make([]*ir.Node, len(idx))
	values := make([]*ir.Node, len(idx))
	for i, j := range idx {
		fields[i], values[i] = n.Fields[j], n.Values[j]
	}
	n.Fields, n.Values = fields, values
}

func fieldKey(f *ir.Node) string {
	if f == nil {
		return ""
	}
	if f.Type == ir.StringType {
		return f.String
	}
	return nodeText(f)
}

func (r readResult) String() string {
	if r.err != nil {
		return "ERROR " + r.err.Error()
	}
	return nodeText(r.node)
}

func TestReadEquivalence_SnapshotVsReference(t *testing.T) {
	for seed := 1; seed <= seedCount(); seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			runEquivalenceSeed(t, int64(seed), 40)
		})
	}
}

func runEquivalenceSeed(t *testing.T, seed int64, nOps int) {
	rng := rand.New(rand.NewSource(seed))
	ops := genOps(rng, nOps)

	ref := openTestStorage(t)  // A: never snapshots
	subj := openTestStorage(t) // B: snapshots at the generated points

	var commits []int64
	for i, o := range ops {
		refCommit, refErr := applyOp(t, ref, o)
		subjCommit, subjErr := applyOp(t, subj, o)
		if (refErr == nil) != (subjErr == nil) {
			t.Fatalf("seed %d op %d %s: commit disagreed: reference %v, subject %v\n%s",
				seed, i, o, refErr, subjErr, dumpOps(ops[:i+1]))
		}
		if refErr != nil {
			continue // both refused it; the stream stays in lockstep
		}
		if refCommit != subjCommit {
			t.Fatalf("seed %d op %d %s: commit numbers diverged: reference %d, subject %d",
				seed, i, o, refCommit, subjCommit)
		}
		commits = append(commits, refCommit)
		if o.snapshot {
			if err := subj.SwitchDLog(); err != nil {
				t.Fatalf("seed %d op %d: SwitchDLog: %v", seed, i, err)
			}
		}
	}

	for _, commit := range commits {
		for _, kp := range readPaths {
			want := read(ref, kp, commit)
			got := read(subj, kp, commit)
			if want.equal(got) {
				continue
			}
			kind := "diverged"
			if want.orderOnly(got) {
				kind = "diverged (field ORDER only, same content)"
			}
			t.Fatalf("seed %d: read(%q, commit=%d) %s\n  reference: %s\n  subject:   %s\n%s",
				seed, kp, commit, kind, want, got, dumpOps(ops))
		}
	}
}

// dumpOps renders the stream so a failure is replayable by eye, not just by seed.
func dumpOps(ops []genOp) string {
	var b strings.Builder
	b.WriteString("  write stream:\n")
	for i, o := range ops {
		fmt.Fprintf(&b, "    %2d: %s\n", i+1, o)
	}
	return b.String()
}
