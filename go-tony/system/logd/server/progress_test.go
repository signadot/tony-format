package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// progressAt answers the commit a progress request with id reported, and its index in the
// transcript, or -1 when it has not been answered.
func progressAt(rs []*api.SessionResponse, id string) (int, int64) {
	for i, r := range rs {
		if r.ID != nil && *r.ID == id && r.Result != nil && r.Result.Progress != nil {
			return i, r.Result.Progress.Commit
		}
	}
	return -1, 0
}

// A progress answer comes after every event its watch sends for a commit at or below the
// commit it reports. The writes go straight to the store while the request is on its way,
// so the watch's events for them are racing the answer (7v4azhtjh12krv76q9n0): a ping
// sent the same way can overtake them.
func TestProgressFollowsTheWatchsEvents(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "a", "{n: 0}")
	ls := newLiveSession(t, store)
	// The dispatcher lags the watermark, as it does under load: a commit is readable, and
	// reported, before its watchers have been handed it.
	store.SetCommitNotifier(func(n *storage.CommitNotification) {
		time.Sleep(2 * time.Millisecond)
		ls.hub.Broadcast(n)
	})
	ls.send(`{id: "w", watch: {path: "a"}}`)
	ls.until("the watch's state", func(m map[string][]*api.SessionResponse) bool {
		for _, r := range m["w"] {
			if r.Event != nil && r.Event.State != nil {
				return true
			}
		}
		return false
	})

	const rounds = 50
	for i := 1; i <= rounds; i++ {
		narrowWrite(t, store, "a", fmt.Sprintf("{n: %d}", i))
		narrowWrite(t, store, "b", fmt.Sprintf("{n: %d}", i))
		ls.send(fmt.Sprintf(`{id: "p%d", progress: {path: "a", watchId: "w"}}`, i))
	}
	ls.until("every progress answer", func(m map[string][]*api.SessionResponse) bool {
		return len(m[fmt.Sprintf("p%d", rounds)]) > 0
	})

	rs := decodeResponses(t, ls.conn.GetResponses())
	for i := 1; i <= rounds; i++ {
		id := fmt.Sprintf("p%d", i)
		at, commit := progressAt(rs, id)
		if at < 0 {
			t.Fatalf("%s: no progress answer in %s", id, ls.conn.GetResponses())
		}
		// Every write to a at or below the commit reported has its event ahead of the
		// answer.
		ahead := 0
		for _, r := range rs[:at] {
			if r.ID != nil && *r.ID == "w" && r.Event != nil && r.Event.Patch != nil && r.Event.Commit <= commit {
				ahead++
			}
		}
		for _, r := range rs[at:] {
			if r.ID != nil && *r.ID == "w" && r.Event != nil && r.Event.Commit <= commit && r.Event.Commit > 1 {
				t.Errorf("%s reported commit %d, and the event for commit %d came after it", id, commit, r.Event.Commit)
			}
		}
		// Round i wrote a at commit 2i and b at 2i+1, and the request followed both.
		if commit < int64(2*i+1) {
			t.Errorf("%s reported commit %d, below the round's writes (%d)", id, commit, 2*i+1)
		}
		// The writes to a after the watch began are the even commits from 2.
		if want := int(commit / 2); ahead != want {
			t.Errorf("%s reported commit %d with %d of the watch's events ahead of it, want %d", id, commit, ahead, want)
		}
	}
}

// A watch that nothing reaches is current through the head all the same: the commits it
// was not sent are ones it accounted for by not being reached.
func TestProgressOnAQuietWatch(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "a", "{n: 0}")
	ls := newLiveSession(t, store)
	ls.send(`{id: "w", watch: {path: "a"}}`)
	for i := 1; i <= 20; i++ {
		narrowWrite(t, store, "b", fmt.Sprintf("{n: %d}", i))
	}
	head, _ := store.GetCurrentCommit()
	ls.send(`{id: "p", progress: {path: "a", watchId: "w"}}`)
	got := ls.until("the progress answer", func(m map[string][]*api.SessionResponse) bool { return len(m["p"]) > 0 })
	r := got["p"][0]
	if r.Result == nil || r.Result.Progress == nil {
		t.Fatalf("progress answered %+v", r)
	}
	if p := r.Result.Progress; p.Commit != head || p.Path != "a" {
		t.Errorf("progress answered %+v, want path a at commit %d", p, head)
	}
}

// A progress request names a watch the session holds, as an unwatch does.
func TestProgressWithoutAWatch(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "a", "{n: 0}")
	ls := newLiveSession(t, store)
	ls.send(`{id: "w", watch: {path: "a"}}`)
	ls.send(`{id: "p1", progress: {path: "a", watchId: "other"}}`)
	ls.send(`{id: "p2", progress: {path: "b"}}`)
	got := ls.until("both refusals", func(m map[string][]*api.SessionResponse) bool {
		return len(m["p1"]) > 0 && len(m["p2"]) > 0
	})
	for _, id := range []string{"p1", "p2"} {
		if e := got[id][0].Error; e == nil || e.Code != api.ErrCodeNotWatching {
			t.Errorf("%s answered %+v, want not_watching", id, got[id][0])
		}
	}
}
