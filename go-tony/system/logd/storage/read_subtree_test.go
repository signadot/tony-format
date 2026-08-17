package storage

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
)

// subtreeStore builds a store with a broad tree: many entities under one path, and a
// small node elsewhere, which is the shape a narrow read exists for.
func subtreeStore(t *testing.T, entities int, snapshotAfter bool) *Storage {
	t.Helper()
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("open: %s", err)
	}
	t.Cleanup(func() { s.Close() })

	blob := strings.Repeat("x", 200)
	for i := 0; i < entities; i++ {
		id := "e" + strconv.Itoa(i)
		subtreeWrite(t, s, "verse.entities."+id, "{id: "+id+", blob: "+blob+"}")
	}
	subtreeWrite(t, s, "verse.meta.rev", "{n: 1}")
	if snapshotAfter {
		if err := s.SwitchDLog(); err != nil {
			t.Fatalf("snapshot: %s", err)
		}
		// and writes after the snapshot, so both sides of the read are exercised
		subtreeWrite(t, s, "verse.entities.late", "{id: late}")
		subtreeWrite(t, s, "verse.meta.rev", "{n: 2}")
	}
	return s
}

func subtreeWrite(t *testing.T, s *Storage, path, body string) {
	t.Helper()
	tx, err := s.NewTx(1, nil)
	if err != nil {
		t.Fatalf("newtx: %s", err)
	}
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %s", body, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		t.Fatalf("patcher %s: %s", path, err)
	}
	if r := p.Commit(); !r.Committed {
		t.Fatalf("commit %s: %v", path, r.Error)
	}
}

// A narrow read has to answer exactly what the wide read holds at that path -- that
// is the whole contract, and everything else about it is cost.
func TestReadSubtreeMatchesTheWideRead(t *testing.T) {
	for _, snapshotAfter := range []bool{false, true} {
		name := "no snapshot"
		if snapshotAfter {
			name = "across a snapshot"
		}
		t.Run(name, func(t *testing.T) {
			s := subtreeStore(t, 20, snapshotAfter)
			commit, err := s.GetCurrentCommit()
			if err != nil {
				t.Fatal(err)
			}
			full, err := s.ReadStateAt("", commit, nil)
			if err != nil {
				t.Fatalf("wide read: %s", err)
			}

			for _, kp := range []string{
				"verse", "verse.meta", "verse.meta.rev", "verse.entities",
				"verse.entities.e7", "verse.entities.e7.id", "nope", "verse.nope.deeper",
			} {
				want, err := full.GetKPath(kp)
				if err != nil {
					t.Fatalf("navigate %q: %s", kp, err)
				}
				got, narrowed, err := s.ReadSubtreeAt(kp, commit, nil)
				if err != nil {
					t.Fatalf("narrow read %q: %s", kp, err)
				}
				switch {
				case want == nil && got == nil:
				case want == nil || got == nil:
					t.Errorf("%q: narrow=%v gave %v, wide gave %v", kp, narrowed, got, want)
				case !got.DeepEqual(want):
					t.Errorf("%q: narrow=%v differs from the wide read\n got %s\nwant %s",
						kp, narrowed, mustEncode(t, got), mustEncode(t, want))
				}
			}
		})
	}
}

// The point of it: reading one entity does not read the tree.
func TestReadSubtreeIsNarrowerThanTheDocument(t *testing.T) {
	s := subtreeStore(t, 200, true)
	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}

	full, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatal(err)
	}
	wide := len(mustEncode(t, full))

	got, narrowed, err := s.ReadSubtreeAt("verse.meta.rev", commit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !narrowed {
		t.Fatalf("the read fell back to the whole document")
	}
	narrow := len(mustEncode(t, got))
	if narrow*10 > wide {
		t.Errorf("narrow read answered %d bytes of a %d byte document, which is not narrow", narrow, wide)
	}
	t.Logf("wide %d bytes, narrow %d bytes", wide, narrow)
}

// An operator above the path says something the subtree cannot say for itself, so the
// read says so by falling back rather than guessing.
func TestReadSubtreeFallsBackUnderAnOperator(t *testing.T) {
	s := subtreeStore(t, 5, true)
	// !all at verse.entities: what it does to each element is not a statement about
	// any one of them that the element can carry alone.
	subtreeWrite(t, s, "verse.entities", "!all {touched: true}")

	commit, err := s.GetCurrentCommit()
	if err != nil {
		t.Fatal(err)
	}
	full, err := s.ReadStateAt("", commit, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := full.GetKPath("verse.entities.e1")
	if err != nil {
		t.Fatal(err)
	}
	got, narrowed, err := s.ReadSubtreeAt("verse.entities.e1", commit, nil)
	if err != nil {
		t.Fatalf("narrow read: %s", err)
	}
	if narrowed {
		t.Error("the read narrowed through an operator above the path")
	}
	if got == nil || !got.DeepEqual(want) {
		t.Errorf("the fallback answered %v, want %v", got, want)
	}
}

func mustEncode(t *testing.T, n *ir.Node) string {
	t.Helper()
	if n == nil {
		return "<nil>"
	}
	var b bytes.Buffer
	if err := encode.Encode(n, &b, encode.EncodeWire(true)); err != nil {
		t.Fatalf("encode: %s", err)
	}
	return b.String()
}

// What the issue is about is the time, not the bytes: a wide read is O(state), so a
// caller after one node pays for every other one.  Measured on this store the narrow
// read is ~40x faster (5.6ms against 131us); the bound below is a tenth of that, so a
// busy machine cannot fail it while a regression to whole-document reads still does.
func TestReadSubtreeCostsLessThanTheDocument(t *testing.T) {
	s := subtreeStore(t, 400, true)
	commit, _ := s.GetCurrentCommit()

	best := func(f func()) time.Duration {
		f()
		var b time.Duration
		for i := 0; i < 5; i++ {
			t0 := time.Now()
			f()
			if d := time.Since(t0); b == 0 || d < b {
				b = d
			}
		}
		return b
	}

	wide := best(func() {
		if _, err := s.ReadStateAt("verse.meta.rev", commit, nil); err != nil {
			t.Fatal(err)
		}
	})
	narrow := best(func() {
		n, ok, err := s.ReadSubtreeAt("verse.meta.rev", commit, nil)
		if err != nil || !ok || n == nil {
			t.Fatalf("narrow read: ok=%v err=%v", ok, err)
		}
	})
	entity := best(func() {
		n, ok, err := s.ReadSubtreeAt("verse.entities.e7", commit, nil)
		if err != nil || !ok || n == nil {
			t.Fatalf("narrow read: ok=%v err=%v", ok, err)
		}
	})
	t.Logf("wide read (ReadStateAt, any path) %10s", wide.Round(time.Microsecond))
	t.Logf("narrow read verse.meta.rev        %10s", narrow.Round(time.Microsecond))
	t.Logf("narrow read verse.entities.e7     %10s", entity.Round(time.Microsecond))
	for _, c := range []struct {
		name string
		took time.Duration
	}{{"verse.meta.rev", narrow}, {"verse.entities.e7", entity}} {
		if took := c.took * 4; took > wide {
			t.Errorf("narrow read of %s took %s against a wide read of %s: not narrow",
				c.name, c.took.Round(time.Microsecond), wide.Round(time.Microsecond))
		}
	}
}
