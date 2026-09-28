package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/signadot/tony-format/go-tony/system/logd/api"
	"github.com/signadot/tony-format/go-tony/system/logd/storage"
)

// progressEvents answers the watch's progress events in the transcript, by index.
func progressEvents(rs []*api.SessionResponse, watchID string) map[int]int64 {
	out := map[int]int64{}
	for i, r := range rs {
		if r.ID != nil && *r.ID == watchID && r.Event != nil && r.Event.Progress {
			out[i] = r.Event.Commit
		}
	}
	return out
}

// A progress event comes after every event its watch sends for a commit at or below the
// commit it carries, and the stream's commits stay in order around it. The writes go
// straight to the store while the request is on its way, and the dispatcher lags the
// watermark as it does under load, so the watch's events for them are racing the answer
// (7v4azhtjh12krv76q9n0): a ping sent the same way overtakes them.
func TestProgressFollowsTheWatchsEvents(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "a", "{n: 0}")
	ls := newLiveSession(t, store)
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
		ls.send(fmt.Sprintf(`{id: "p%d", progress: w}`, i))
	}
	got := ls.until("every progress event", func(m map[string][]*api.SessionResponse) bool {
		n := 0
		for _, r := range m["w"] {
			if r.Event != nil && r.Event.Progress {
				n++
			}
		}
		return n == rounds
	})

	// Each request is acknowledged with the head, and the round's writes -- a at 2i, b at
	// 2i+1 -- are at or below it.
	var maxAck int64
	for i := 1; i <= rounds; i++ {
		rs := got[fmt.Sprintf("p%d", i)]
		if len(rs) != 1 || rs[0].Result == nil || rs[0].Result.Progress == nil {
			t.Fatalf("p%d answered %+v", i, rs)
		}
		c := rs[0].Result.Progress.Commit
		if c < int64(2*i+1) {
			t.Errorf("p%d acknowledged commit %d, below the round's writes (%d)", i, c, 2*i+1)
		}
		maxAck = max(maxAck, c)
	}

	rs := decodeResponses(t, ls.conn.GetResponses())
	var last, maxEvent int64
	for at, commit := range progressEvents(rs, "w") {
		maxEvent = max(maxEvent, commit)
		// The writes to a after the watch began are the even commits from 2: each at or
		// below the event's commit has its event ahead of it.
		ahead := 0
		for _, r := range rs[:at] {
			if r.ID != nil && *r.ID == "w" && r.Event != nil && r.Event.Patch != nil && r.Event.Commit <= commit {
				ahead++
			}
		}
		if want := int(commit / 2); ahead != want {
			t.Errorf("the progress event for commit %d has %d of the watch's events ahead of it, want %d", commit, ahead, want)
		}
	}
	// Every request's event carries its acknowledged commit or a later one.
	if maxEvent < maxAck {
		t.Errorf("the progress events reach commit %d, below the last acknowledged, %d", maxEvent, maxAck)
	}
	for _, r := range rs {
		if r.ID == nil || *r.ID != "w" || r.Event == nil || r.Event.Commit == 0 {
			continue
		}
		if r.Event.Commit < last {
			t.Errorf("the watch sent commit %d after commit %d", r.Event.Commit, last)
		}
		last = r.Event.Commit
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
	ls.send(`{id: "p", progress: w}`)
	got := ls.until("the progress event", func(m map[string][]*api.SessionResponse) bool {
		for _, r := range m["w"] {
			if r.Event != nil && r.Event.Progress {
				return true
			}
		}
		return false
	})
	if r := got["p"]; len(r) != 1 || r[0].Result == nil || r[0].Result.Progress == nil || r[0].Result.Progress.Commit != head {
		t.Errorf("progress acknowledged %+v, want commit %d", r, head)
	}
	for _, r := range got["w"] {
		if ev := r.Event; ev != nil && ev.Progress && (ev.Commit != head || ev.Path != "a") {
			t.Errorf("progress event %+v, want path a at commit %d", ev, head)
		}
	}
}

// A progress request names a watch by its id. One the session does not hold is refused.
func TestProgressWithoutAWatch(t *testing.T) {
	store := openStore(t)
	narrowWrite(t, store, "a", "{n: 0}")
	ls := newLiveSession(t, store)
	ls.send(`{id: "w", watch: {path: "a"}}`)
	ls.send(`{id: "p", progress: other}`)
	got := ls.until("the refusal", func(m map[string][]*api.SessionResponse) bool { return len(m["p"]) > 0 })
	if e := got["p"][0].Error; e == nil || e.Code != api.ErrCodeNotWatching {
		t.Errorf("p answered %+v, want not_watching", got["p"][0])
	}
}
