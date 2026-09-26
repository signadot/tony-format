package commands

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// serve -watch is `git issue watch` behind the web view. The same watch runs
// (watchStore): it looks at this clone's refs every -poll and pulls the
// remote every -fetch. What it finds goes to the open pages rather than to a
// terminal: a page holds an event stream open (/events), and reloads when its
// issue changes -- the index when any does. What a pull left standing, an
// issue refused or a remote not reached, is shown on the pages, and every
// page reloads when it changes.

// liveView is what the watch tells the open pages.
type liveView struct {
	p *puller // nil when the watch pulls nothing

	mu   sync.Mutex
	subs map[chan string]bool
	told string // what stands (alarmsHash), as the pages were last told
}

func newLiveView(p *puller) *liveView {
	return &liveView{p: p, subs: map[chan string]bool{}}
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

// changed tells the pages an issue changed.
func (l *liveView) changed(ch watchChange) { l.send(ch.ID) }

// anyPage is the event every page reloads on.
const anyPage = "*"

// pulled tells every page when what stands is not what they were told.
func (l *liveView) pulled() {
	hash := alarmsHash(l.stands())
	l.mu.Lock()
	differs := hash != l.told
	l.told = hash
	l.mu.Unlock()
	if differs {
		l.send(anyPage)
	}
}

// send hands an event to every open page. A page that has fallen behind is
// cut off rather than waited for: its stream ends, it connects again, and
// reloads for having lost one.
func (l *liveView) send(what string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.subs {
		select {
		case ch <- what:
		default:
			delete(l.subs, ch)
			close(ch)
		}
	}
}

func (l *liveView) subscribe() chan string {
	ch := make(chan string, 64)
	l.mu.Lock()
	l.subs[ch] = true
	l.mu.Unlock()
	return ch
}

func (l *liveView) unsubscribe(ch chan string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subs[ch] {
		delete(l.subs, ch)
		close(ch)
	}
}

// close ends every stream, for a server shutting down.
func (l *liveView) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for ch := range l.subs {
		delete(l.subs, ch)
		close(ch)
	}
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
// it serves the script and the events.
func (s *issueServer) watching(l *liveView) {
	s.watch = l
	s.mux.HandleFunc("GET /watch.js", s.handleWatchJS)
	s.mux.HandleFunc("GET /events", s.handleEvents)
}

// watchJS reloads the page on an event that names its issue, or any issue
// when the page names none, or every page. Events close together are one
// reload. A stream that was lost may have lost events, so connecting again
// reloads too.
//
// It is a file of its own, not inline, so that no page holds a script
// element with anything in it: what an issue's text could smuggle in stays
// tellable from what the server wrote.
const watchJS = `(function () {
  var self = document.body.getAttribute("data-watch");
  var timer, lost = false;
  function reload() {
    clearTimeout(timer);
    timer = setTimeout(function () { location.reload(); }, 200);
  }
  var events = new EventSource("/events");
  events.onmessage = function (e) {
    if (!self || e.data === "*" || e.data === self) reload();
  };
  events.onerror = function () { lost = true; };
  events.onopen = function () { if (lost) reload(); };
})();
`

func (s *issueServer) handleWatchJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(watchJS))
}

// handleEvents streams what the watch finds, one event an issue's id or
// anyPage, until the page goes or the server does.
func (s *issueServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := s.watch.subscribe()
	defer s.watch.unsubscribe(ch)
	fmt.Fprint(w, ": watching\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case what, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", what)
			flusher.Flush()
		}
	}
}

// watchPuller answers the puller of a watch: of the remote named, or of
// origin when there is one and local is not asked for, or none.
func watchPuller(cc *cli.Context, st issuelib.Store, remote string, local bool) (*puller, error) {
	switch {
	case local && remote != "":
		return nil, fmt.Errorf("%w: --local and --remote: one or the other", cli.ErrUsage)
	case remote != "":
		if err := st.VerifyRemote(remote); err != nil {
			return nil, err
		}
		return newPuller(st, "", "", remote, pullTimeout), nil
	case !local:
		if st.VerifyRemote("origin") == nil {
			return newPuller(st, "", "", "origin", pullTimeout), nil
		}
		fmt.Fprintln(cc.Err, "no origin: watching this clone alone")
	}
	return nil, nil
}
