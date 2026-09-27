package commands

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// serve -watch is `git issue watch` behind the web view. The same watch runs
// (watchStore): it looks at this clone's refs every -poll and pulls the
// remote every -fetch. What it finds goes to the open pages rather than to a
// terminal: a page asks what changed since it last asked (/changes), every
// couple of seconds, and reloads when its issue did -- the index when any
// did. What a pull left standing, an issue refused or a remote not reached,
// is shown on the pages, and every page reloads when it changes.
//
// A page asks rather than holding a stream open, because a browser keeps six
// connections to a server and no more: six tabs each holding one would leave
// none to load a seventh page.

// liveView is what the watch tells the open pages: the changes it found,
// numbered in order, the last liveLogCap kept.
type liveView struct {
	p *puller // nil when the watch pulls nothing

	mu    sync.Mutex
	epoch string // this server's: numbers start again with each
	seq   uint64
	log   []liveChange
	told  string // what stands (alarmsHash), as the pages were last told
}

// liveChange is one thing a page may reload for: an issue's id, or anyPage.
type liveChange struct {
	seq  uint64
	what string
}

// liveLogCap is how many changes are kept. A page that asks from before
// them reloads, which is what it would have done for one of them.
const liveLogCap = 1024

func newLiveView(p *puller) *liveView {
	return &liveView{p: p, epoch: strconv.FormatInt(time.Now().UnixNano(), 36)}
}

// everyIssue is the scope of what stands: all of it.
func everyIssue(string) bool { return true }

// stands is what the last pull left standing.
func (l *liveView) stands() []pullNote {
	if l.p == nil {
		return nil
	}
	return l.p.stands(everyIssue)
}

// changed records that an issue changed.
func (l *liveView) changed(ch watchChange) { l.record(ch.ID) }

// anyPage is the change every page reloads for.
const anyPage = "*"

// pulled records a change for every page when what stands is not what they
// were told.
func (l *liveView) pulled() {
	hash := alarmsHash(l.stands())
	l.mu.Lock()
	differs := hash != l.told
	l.told = hash
	l.mu.Unlock()
	if differs {
		l.record(anyPage)
	}
}

func (l *liveView) record(what string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	l.log = append(l.log, liveChange{l.seq, what})
	if over := len(l.log) - liveLogCap; over > 0 {
		l.log = append([]liveChange(nil), l.log[over:]...)
	}
}

// changesOut is what /changes answers.
type changesOut struct {
	Cursor  string   `json:"cursor"`  // what to ask from next
	Changed []string `json:"changed"` // the issues changed since, anyPage among them for every page
	Reload  bool     `json:"reload"`  // the cursor is not this server's, or is from before what it keeps
}

// since answers what changed after a cursor. With none it answers the cursor
// to ask from, and no change: where a page begins.
func (l *liveView) since(cursor string) changesOut {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := changesOut{Cursor: l.epoch + "." + strconv.FormatUint(l.seq, 10), Changed: []string{}}
	if cursor == "" {
		return out
	}
	epoch, n, _ := strings.Cut(cursor, ".")
	seq, err := strconv.ParseUint(n, 10, 64)
	if err != nil || epoch != l.epoch || seq > l.seq || (len(l.log) > 0 && seq+1 < l.log[0].seq) {
		out.Reload = true
		return out
	}
	for _, c := range l.log {
		if c.seq > seq {
			out.Changed = append(out.Changed, c.what)
		}
	}
	return out
}

// livePage is what a page says of the watch. The zero value is a page of a
// server that does not watch.
type livePage struct {
	Watching bool
	Self     string   // the issue whose change reloads the page; empty for any
	Stands   []string // what the last pull left standing, as watch says it
}

// live answers what a page says of the watch, and what that adds to the
// page's cache token. self is the page's issue, or empty for a page of them
// all; an issue's page shows what stands of that issue, and every failure.
func (s *issueServer) live(self string) (livePage, string) {
	if s.watch == nil {
		return livePage{}, ""
	}
	page := livePage{Watching: true, Self: self}
	var shown []pullNote
	for _, n := range s.watch.stands() {
		if self == "" || n.ID == "" || n.ID == self {
			shown = append(shown, n)
			page.Stands = append(page.Stands, n.line())
		}
	}
	return page, ".live." + alarmsHash(shown)
}

// watching makes the server one that watches: its pages carry the script, and
// it serves the script and the changes.
func (s *issueServer) watching(l *liveView) {
	s.watch = l
	s.mux.HandleFunc("GET /watch.js", s.handleWatchJS)
	s.mux.HandleFunc("GET /changes", s.handleChanges)
}

// watchJS asks what changed every two seconds, and reloads the page for a
// change that names its issue, or any issue when the page names none, or
// every page. It asks at once when the page loads, for where to begin. A
// server that does not answer is asked again: when it is back its cursor is
// another, and the page reloads.
//
// It is a file of its own, not inline, so that no page holds a script
// element with anything in it: what an issue's text could smuggle in stays
// tellable from what the server wrote.
const watchJS = `(function () {
  var self = document.body.getAttribute("data-watch");
  var cursor = "";
  function again() { setTimeout(ask, 2000); }
  function ask() {
    fetch("/changes?since=" + encodeURIComponent(cursor), { cache: "no-store" })
      .then(function (res) { return res.json(); })
      .then(function (c) {
        var mine = c.changed.some(function (id) {
          return !self || id === "*" || id === self;
        });
        if (c.reload || mine) { location.reload(); return; }
        cursor = c.cursor;
        again();
      })
      .catch(again);
  }
  ask();
})();
`

func (s *issueServer) handleWatchJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(watchJS))
}

// handleChanges answers what the watch found since the cursor a page gives.
// It reads what the watch recorded and nothing of the repository.
func (s *issueServer) handleChanges(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s.watch.since(r.URL.Query().Get("since")))
}

// watchPuller answers the puller of a watch on a repository: of the remote
// named, or of origin when there is one and local is not asked for, or none.
// repo is the repository's name in what is said, when more than one is
// watched.
func watchPuller(cc *cli.Context, st issuelib.Store, repo, remote string, local bool) (*puller, error) {
	switch {
	case local && remote != "":
		return nil, fmt.Errorf("%w: --local and --remote: one or the other", cli.ErrUsage)
	case remote != "":
		if err := st.VerifyRemote(remote); err != nil {
			if repo != "" {
				return nil, fmt.Errorf("%s: %w", repo, err)
			}
			return nil, err
		}
		return newPuller(st, "", repo, remote, pullTimeout), nil
	case !local:
		if st.VerifyRemote("origin") == nil {
			return newPuller(st, "", repo, "origin", pullTimeout), nil
		}
		if repo != "" {
			fmt.Fprintf(cc.Err, "%s: ", repo)
		}
		fmt.Fprintln(cc.Err, "no origin: watching this clone alone")
	}
	return nil, nil
}
