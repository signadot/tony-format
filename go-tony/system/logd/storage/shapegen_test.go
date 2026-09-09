package storage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

// A store shaped like the staging forensics, at a chosen scale: many paths under a few
// shallow ancestors, a root snapshot every so often, writes spread over the paths so that
// two commits in three land under one ancestor. The bar the issue sets is read against it
// (rebuild_plan.md, "Measuring against the bar").
type shape struct {
	paths         int // distinct leaf paths
	writesPerPath int // commits touching each path, spread over the run
	snapshotEvery int // commits between root snapshots
	ancestors     []string
}

func shapedStore(t *testing.T, sh shape) (*Storage, []string) {
	t.Helper()
	s := openTestStorage(t)
	paths := make([]string, sh.paths)
	for i := range paths {
		paths[i] = fmt.Sprintf("%s.n%d", sh.ancestors[i%len(sh.ancestors)], i)
	}
	commits := 0
	for round := 0; round < sh.writesPerPath; round++ {
		for i, p := range paths {
			body := fmt.Sprintf("{v: %d, blob: %s}", round, strings.Repeat("x", 64))
			if err := scopedCommit(t, s, nil, p, body); err != nil {
				t.Fatalf("write %s: %v", p, err)
			}
			commits++
			if sh.snapshotEvery > 0 && commits%sh.snapshotEvery == 0 {
				if err := s.SwitchDLog(); err != nil {
					t.Fatalf("snapshot: %v", err)
				}
			}
			_ = i
		}
	}
	return s, paths
}

// A read of one field beneath a path that carries many writes and sits far from the last
// snapshot reads the writes to THAT path since the snapshot and nothing else: the largest
// record it holds is one write to it, and the count is bounded by its own history in the
// interval, not by the store's.
func TestReadOfOneFieldIsBoundedByItsOwnWrites(t *testing.T) {
	sh := shape{paths: 120, writesPerPath: 4, snapshotEvery: 150,
		ancestors: []string{"verse.git.ref", "verse.github.issue", "verse.github.comment"}}
	s, paths := shapedStore(t, sh)
	commit, _ := s.GetCurrentCommit()
	// A path written AFTER the last snapshot, so the read composes a projected write over
	// the snapshot's base rather than answering from the snapshot alone: the last round
	// wrote paths in order, and the snapshot fell at commit 450 of 480.
	target := paths[len(paths)-10] + ".v"

	before := s.ReadStats()
	node, _, err := readSubtreeAt(s, target, commit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if node == nil || node.Int64 == nil || *node.Int64 != int64(sh.writesPerPath-1) {
		t.Fatalf("read %s at %s, want %d", mustEncode(t, node), target, sh.writesPerPath-1)
	}
	after := s.ReadStats()

	if after.SeekHit != before.SeekHit+1 {
		t.Errorf("the seek did not find the root snapshot (hit %d -> %d, miss %d -> %d)",
			before.SeekHit, after.SeekHit, before.SeekMiss, after.SeekMiss)
	}
	if after.Narrow != before.Narrow+1 || after.WideRoot != before.WideRoot {
		t.Errorf("a read at a field was counted wide: narrow %d -> %d, root %d -> %d",
			before.Narrow, after.Narrow, before.WideRoot, after.WideRoot)
	}
	// One write to this path is ~100 bytes of node; the interval holds ~150 commits to
	// other paths. The largest record held is a write to THIS path.
	if largest := after.LargestRecord; largest == 0 || largest > 2048 {
		t.Errorf("the largest record held was %d bytes; want one write to this path, which is a few hundred", largest)
	}
	emitted := after.BytesEmitted - before.BytesEmitted
	if emitted > 256 {
		t.Errorf("a read of one integer emitted %d bytes", emitted)
	}
	t.Logf("commit %d, %d paths, snapshot every %d: emitted %d bytes, largest record %d bytes",
		commit, sh.paths, sh.snapshotEvery, emitted, after.LargestRecord)
}

// The root read is a cursor at the root: it completes, and what it emits is the document.
func TestRootReadIsACursorAtTheRoot(t *testing.T) {
	s, paths := shapedStore(t, shape{paths: 60, writesPerPath: 2, snapshotEvery: 50,
		ancestors: []string{"verse.a", "verse.b"}})
	commit, _ := s.GetCurrentCommit()
	c, err := s.Read(commit, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Presence() != Present {
		t.Fatalf("root presence %s", c.Presence())
	}
	events := 0
	for {
		_, err := c.Next()
		if err != nil {
			break
		}
		events++
	}
	c.Close()
	if events < len(paths)*4 {
		t.Errorf("the root cursor emitted %d events for %d paths", events, len(paths))
	}
	doc, err := readStateAt(s, "", commit, nil)
	if err != nil || doc == nil {
		t.Fatal(err)
	}
	if got, _ := doc.GetKPath(paths[3] + ".v"); got == nil || got.Int64 == nil || *got.Int64 != 1 {
		t.Errorf("%s.v = %v", paths[3], got)
	}
	_ = ir.Null
}

// scopeShape is a sandbox on a shaped store: a scope that rewrites K of the store's paths,
// rounds times, while baseline keeps writing all of them. It is the deployment shape the
// scope plan measures against -- a sandbox writing a few entities while baseline writes
// many (scope_plan.md, phase 0) -- and the watch at the shared ancestor is phase 3's.
type scopeShape struct {
	shape
	scope  string
	k      int // paths the scope writes, the first k of the store's
	rounds int // times the scope rewrites each of them
}

// shapedScopeStore builds the baseline shape, then interleaves the scope's rounds with one
// more baseline round, so the scope's entries sit among baseline's in the log rather than
// after them.
func shapedScopeStore(t *testing.T, sh scopeShape) (*Storage, []string) {
	t.Helper()
	s, paths := shapedStore(t, sh.shape)
	sc := sh.scope
	for round := 0; round < sh.rounds; round++ {
		for i := 0; i < sh.k && i < len(paths); i++ {
			body := fmt.Sprintf("{v: %d, sandbox: %s}", 1000+round, strings.Repeat("y", 64))
			if err := scopedCommit(t, s, &sc, paths[i], body); err != nil {
				t.Fatalf("scope write %s: %v", paths[i], err)
			}
		}
		for _, p := range paths {
			body := fmt.Sprintf("{v: %d, blob: %s}", sh.writesPerPath+round, strings.Repeat("x", 64))
			if err := scopedCommit(t, s, nil, p, body); err != nil {
				t.Fatalf("baseline write %s: %v", p, err)
			}
		}
	}
	return s, paths
}

// A scoped read of one of the sandbox's paths, and of a path the sandbox never wrote,
// counted: what the scope term of each folds today is the scope's history at the path,
// and the footprint counters exist and read zero. Phase 2 changes the first number and
// starts the others; this is where they are read from.
func TestScopedReadsOnAShapedStoreCountTheirScopeTerm(t *testing.T) {
	sh := scopeShape{
		shape: shape{paths: 90, writesPerPath: 2, snapshotEvery: 100,
			ancestors: []string{"verse.git.ref", "verse.github.issue", "verse.github.comment"}},
		scope: "sandbox", k: 5, rounds: 8,
	}
	s, paths := shapedScopeStore(t, sh)
	commit, _ := s.GetCurrentCommit()
	sc := sh.scope

	before := s.ReadStats()
	if _, _, err := readSubtreeAt(s, paths[0], commit, &sc); err != nil {
		t.Fatal(err)
	}
	mid := s.ReadStats()
	if _, _, err := readSubtreeAt(s, paths[len(paths)-1], commit, &sc); err != nil {
		t.Fatal(err)
	}
	after := s.ReadStats()

	if after.Scope != before.Scope+2 {
		t.Errorf("scoped reads counted %d -> %d, want two more", before.Scope, after.Scope)
	}
	sandboxed := mid.ScopeFolded - before.ScopeFolded
	untouched := after.ScopeFolded - mid.ScopeFolded
	if sandboxed < int64(sh.rounds) {
		t.Errorf("a read of a path the sandbox rewrote %d times folded %d scope entries", sh.rounds, sandboxed)
	}
	if untouched != 0 {
		t.Errorf("a read of a path the sandbox never wrote folded %d scope entries", untouched)
	}
	if after.ScopeFootprint != 0 || after.ScopeSkipped != 0 || after.ScopeHistoric != 0 {
		t.Errorf("footprint counters should read zero before phase 2: %+v", after)
	}
	t.Logf("sandbox path folded %d scope entries, untouched path %d; wide %d", sandboxed, untouched, after.ScopeWide)
}
