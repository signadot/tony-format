package server

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"
	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// writeAt commits body at path, in the scope given (nil for baseline), and answers the
// commit and the stored delta as a watcher receives it.
func writeAt(t *testing.T, store *storage.Storage, scope *string, path, body string) (int64, *ir.Node) {
	t.Helper()
	tx, err := store.NewTx(1, scope)
	if err != nil {
		t.Fatalf("newtx: %s", err)
	}
	data, err := parse.Parse([]byte(body))
	if err != nil {
		t.Fatalf("parse %q: %s", body, err)
	}
	p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
	if err != nil {
		t.Fatalf("patcher: %s", err)
	}
	r := p.Commit()
	if !r.Committed {
		t.Fatalf("commit %s: %v", path, r.Error)
	}
	cur, err := store.Deltas(r.Commit, r.Commit, scope, "")
	if err != nil {
		t.Fatalf("Deltas: %v", err)
	}
	defer cur.Close()
	n, err := cur.Next()
	if err != nil {
		t.Fatalf("the delta of %d: %v", r.Commit, err)
	}
	return r.Commit, n.Patch
}

// scopedWatchHarness is a scoped watch on path, driven by hand: writes go to the store and
// their notifications to the watcher, and what the watch sent is read off the connection.
type scopedWatchHarness struct {
	t       *testing.T
	store   *storage.Storage
	scope   string
	hub     *WatchHub
	conn    *mockConn
	session *Session
	watcher *Watcher
	seen    int
}

func newScopedWatch(t *testing.T, store *storage.Storage, scope, path string) *scopedWatchHarness {
	t.Helper()
	h := &scopedWatchHarness{t: t, store: store, scope: scope, hub: NewWatchHub(), conn: newMockConn()}
	h.session = NewSession("test", h.conn, &SessionConfig{Storage: store, Hub: h.hub})
	h.session.scope.Store(&h.scope)
	h.watcher = NewWatcher(path, &h.scope, nil, 16)
	id := "w1"
	h.watcher.ID = &id
	go h.session.writer()
	head, _ := store.GetCurrentCommit()
	go h.session.forwardEvents(h.watcher, nil, true, head)
	return h
}

// commit writes and notifies, then waits for the watch to account for the commit.
func (h *scopedWatchHarness) commit(scope *string, path, body string) int64 {
	h.t.Helper()
	c, patch := writeAt(h.t, h.store, scope, path, body)
	h.watcher.Events <- &storage.CommitNotification{Commit: c, KPaths: []string{"verse"}, Patch: patch, ScopeID: scope}
	time.Sleep(60 * time.Millisecond)
	return c
}

// events answers the patch events sent since the last call.
func (h *scopedWatchHarness) events() []*api.WatchEvent {
	h.t.Helper()
	var out []*api.WatchEvent
	all := decodeResponses(h.t, h.conn.GetResponses())
	for _, resp := range all[h.seen:] {
		if resp.Event != nil && resp.Event.Patch != nil {
			out = append(out, resp.Event)
		}
	}
	h.seen = len(all)
	return out
}

func (h *scopedWatchHarness) counts() (step, drop, reread int64) {
	return h.hub.stats.scopeStep.Load(), h.hub.stats.scopeDrop.Load(), h.hub.stats.scopeReread.Load()
}

// A scoped watch on a path the scope has claimed hears nothing of baseline's writes beneath
// it and one event per write of its own, without a read for either.
func TestScopedWatchUnderAClaimDropsBaselineAndStepsItsOwn(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sc := "sandbox"
	writeAt(t, store, nil, "verse.e1", `{n: 1}`)
	writeAt(t, store, &sc, "verse.e1", `!insert.raw {n: 5}`)
	h := newScopedWatch(t, store, sc, "verse.e1")
	defer h.session.Close()

	h.commit(nil, "verse.e1.n", `2`)
	if evs := h.events(); len(evs) != 0 {
		t.Fatalf("a baseline write under the claim produced %d events: %s", len(evs), encode.MustString(evs[0].Patch))
	}
	h.commit(&sc, "verse.e1.m", `7`)
	evs := h.events()
	if len(evs) != 1 || !api.SameState(evs[0].Patch, mustParseNode(t, `{m: 7}`)) {
		t.Fatalf("the scope's own write produced %d events, first %v", len(evs), evs)
	}
	if step, drop, reread := h.counts(); step != 1 || drop != 1 || reread != 0 {
		t.Errorf("step %d drop %d reread %d, want 1 1 0", step, drop, reread)
	}
}

// A scoped watch on a path the scope has not touched steps baseline's deltas as a
// baseline watch would, and never re-reads.
func TestScopedWatchOnAnUntouchedPathStepsBaseline(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sc := "sandbox"
	writeAt(t, store, nil, "verse.e1", `{n: 1}`)
	writeAt(t, store, &sc, "verse.e2", `{n: 9}`)
	h := newScopedWatch(t, store, sc, "verse.e1")
	defer h.session.Close()

	h.commit(nil, "verse.e1.n", `2`)
	h.commit(nil, "verse.e1", `{k: {deep: true}}`)
	evs := h.events()
	if len(evs) != 2 || !api.SameState(evs[0].Patch, mustParseNode(t, `{n: 2}`)) || !api.SameState(evs[1].Patch, mustParseNode(t, `{k: {deep: true}}`)) {
		t.Fatalf("baseline writes beside the scope produced %d events: %v", len(evs), evs)
	}
	if step, drop, reread := h.counts(); step != 2 || drop != 0 || reread != 0 {
		t.Errorf("step %d drop %d reread %d, want 2 0 0", step, drop, reread)
	}
}

// A baseline delta that meets a statement of the scope is answered by a read, and the
// event is the difference the scope actually sees: the scope's leaf stays, baseline's
// other leaf arrives.
func TestScopedWatchOverlappingBaselineRereads(t *testing.T) {
	store, err := storage.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sc := "sandbox"
	writeAt(t, store, nil, "verse.e1", `{n: 1}`)
	writeAt(t, store, &sc, "verse.e1.m", `7`)
	h := newScopedWatch(t, store, sc, "verse.e1")
	defer h.session.Close()

	h.commit(nil, "verse.e1", `{n: 2, m: 9}`)
	evs := h.events()
	if len(evs) != 1 {
		t.Fatalf("the overlapping write produced %d events", len(evs))
	}
	// The delta is what the re-read's diff spells (a !replace for a changed scalar); the
	// contract is that a client applies it to what it holds and lands on the scope's view.
	held, err := applyAt(mustParseNode(t, `{n: 1, m: 7}`), evs[0].Patch)
	if err != nil || !mergeop.RawEqual(held, mustParseNode(t, `{n: 2, m: 7}`)) {
		t.Fatalf("applying the event %s to the held value gives %s (%v); want {n: 2, m: 7}",
			encode.MustString(evs[0].Patch), encode.MustString(held), err)
	}
	if step, drop, reread := h.counts(); step != 0 || drop != 0 || reread != 1 {
		t.Errorf("step %d drop %d reread %d, want 0 0 1", step, drop, reread)
	}
}

// The three-way rule agrees with the recompute at every commit: a value stepped, dropped
// or re-read per the rule is the value a scoped read at the path answers. Mixed baseline
// and scoped writes over overlapping paths, claims among them, watched at four paths.
func TestScopedStepAgreesWithTheRecompute(t *testing.T) {
	sc := "s1"
	watched := []string{"", "a", "a.b", "d"}
	writePaths := []string{"", "a", "a.b", "d", "d.e"}
	shapes := []string{`{k%d: %d}`, `{k%d: {n: %d}}`, `!insert.raw {k%d: %d, op: !glob "x"}`, `%d`, `!delete`}
	for seed := 1; seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(int64(seed)))
		store, err := storage.Open(t.TempDir(), nil)
		if err != nil {
			t.Fatal(err)
		}
		prev := map[string]*ir.Node{}
		for _, kp := range watched {
			prev[kp] = nil
		}
		for i := 0; i < 50; i++ {
			var scope *string
			if rng.Intn(2) == 0 {
				scope = &sc
			}
			path := writePaths[rng.Intn(len(writePaths))]
			shape := shapes[rng.Intn(len(shapes))]
			body := shape
			switch shape {
			case `%d`:
				body = fmt.Sprintf(shape, i)
			case `!delete`:
			default:
				body = fmt.Sprintf(shape, rng.Intn(3), i)
			}
			if path == "" && (shape == `%d` || shape == `!delete`) {
				continue // a scalar or nothing at the root ends the document
			}
			tx, err := store.NewTx(1, scope)
			if err != nil {
				t.Fatal(err)
			}
			data, err := parse.Parse([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			p, err := tx.NewPatcher(&api.Patch{PathData: api.PathData{Path: path, Data: data}})
			if err != nil {
				t.Fatal(err)
			}
			r := p.Commit()
			if !r.Committed {
				continue // a write the store refused is not a commit anybody sees
			}
			cur, err := store.Deltas(r.Commit, r.Commit, &sc, "")
			if err != nil {
				t.Fatal(err)
			}
			n, err := cur.Next()
			cur.Close()
			if err != nil {
				t.Fatal(err)
			}
			for _, kp := range watched {
				want, err := readScoped(store, r.Commit, sc, kp)
				if err != nil {
					t.Fatalf("seed %d commit %d: read %q: %v", seed, r.Commit, kp, err)
				}
				at, _, ok := api.ProjectDelta(n.Patch, kp)
				var next *ir.Node
				verdict := "own"
				switch {
				case scope != nil:
					if ok && at == nil {
						next = prev[kp]
					} else if ok {
						next, err = applyAt(prev[kp], at.DeepCopy())
					} else {
						next = want
					}
				case !ok:
					next, verdict = want, "blocked"
				case at == nil:
					next, verdict = prev[kp], "misses"
				default:
					switch v := store.BaselineDeltaInScope(sc, kp, at); v {
					case storage.BaselineHidden:
						next, verdict = prev[kp], v.String()
					case storage.BaselineSteps:
						next, err = applyAt(prev[kp], at.DeepCopy())
						verdict = v.String()
					default:
						next, verdict = want, v.String()
					}
				}
				if err != nil {
					t.Fatalf("seed %d commit %d: step %q: %v", seed, r.Commit, kp, err)
				}
				if !api.SameState(next, want) {
					t.Fatalf("seed %d commit %d (%s %s <- %s), watch at %q, verdict %s:\n stepped %s\n read    %s",
						seed, r.Commit, who(scope), path, body, kp, verdict, encode.MustString(next), encode.MustString(want))
				}
				prev[kp] = next
			}
		}
		store.Close()
	}
}

func who(scope *string) string {
	if scope == nil {
		return "baseline"
	}
	return "scope"
}

// readScoped is the value at kp in the scope as of commit, nil where it holds nothing.
func readScoped(store *storage.Storage, commit int64, scope, kp string) (*ir.Node, error) {
	c, err := store.Read(commit, &scope, kp)
	if err != nil {
		return nil, err
	}
	return storage.Collect(c, 1<<20)
}
