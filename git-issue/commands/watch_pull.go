package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// A watch that pulls. Issues often travel by push and pull, so a watch that
// only read this clone would hear nothing a teammate pushed, and would look
// like a sync that does not work. So a watch can pull a remote every -fetch,
// and says what each pull did.
//
// A pull says two kinds of thing. What it did to an issue -- created, moved
// on, merged -- goes with the change it brought, and is said when the change
// is. What stands until a person acts -- an issue refused, a remote not
// reached -- is news on its own, said when it begins or ends and not on every
// pull while it stands. Which of those a watch has said is the watch's to
// know: `git issue watch` keeps it as it runs (alarmsSaid), issue_watch_remote
// carries it in its cursor.

// defaultFetch is how often a watch pulls unless -fetch says otherwise.
const defaultFetch = 30 * time.Second

// pullTimeout is how long a watch's pull waits on a remote before it gives
// up and says it could not pull. A person running pull waits as long as it
// takes; a watch has no one watching it, and a remote that hangs would stop
// it for good.
const pullTimeout = 2 * time.Minute

// pullNote is one thing a watcher's pull did.
type pullNote struct {
	Repo   string `json:"repo,omitempty" jsonschema:"the repository, when more than one is served"`
	Remote string `json:"remote"`
	ID     string `json:"id,omitempty" jsonschema:"the issue, when the note is about one"`
	What   string `json:"what" jsonschema:"what the pull did, as issue_pull says it: created at <sha>, <old>..<new>, merged <a> and <b>; or what stands: refused: why, for a person to decide; failed: why; could not pull: why; and clear, when nothing stands any more"`

	dir string // the repository's directory: its name, Repo, can change
}

func (n pullNote) line() string {
	var b strings.Builder
	b.WriteString("pull " + n.Remote)
	if n.Repo != "" {
		b.WriteString(" (" + n.Repo + ")")
	}
	b.WriteString(":")
	if n.ID != "" {
		b.WriteString(" " + n.ID + " ")
	}
	b.WriteString(" " + n.What)
	return b.String()
}

// key is what a watch compares to know whether it was told of a note: the
// repository by its directory, which does not change as others are served,
// and a pull that could not be made as one thing, whatever git said -- its
// words can differ from one attempt to the next.
func (n pullNote) key() string {
	what := n.What
	if strings.HasPrefix(what, couldNot) {
		what = couldNot
	}
	return n.dir + " " + n.Remote + " " + n.ID + " " + what
}

// couldNot begins the note of a pull that could not be made at all: the
// remote not reached, or not there.
const couldNot = "could not pull: "

// clear is the note of a watch whose refusals and failures have all gone.
const clear = "clear: nothing refused or failing"

// puller pulls one repository from one remote, and keeps what its last pull
// left standing. One pull runs at a time, and a watch that finds one running
// waits for it rather than pulling again.
//
// A pull holds busy, and so does a look at the repository (the MCP server's),
// which leaves a repository being pulled for the look that follows the pull:
// a change is then found with the pull that brought it known, not halfway
// through it.
type puller struct {
	st           issuelib.Store
	dir          string
	repo, remote string

	pulling sync.Mutex
	busy    sync.Mutex

	mu       sync.Mutex
	last     time.Time
	standing []pullNote            // the refusals and failures the last pull left
	recent   map[string]pulledNote // what the last pull did, by issue, until a look takes it
}

// pulledNote is what a pull did to an issue, and the commit it left the issue
// at: a look takes the note only for a change to that commit, so one that
// finds the change before the pull is done does not hand the note on to a
// later change.
type pulledNote struct {
	note   pullNote
	commit string
}

// newPuller pulls through a store on st's repository that gives up on the
// remote after timeout.
func newPuller(st issuelib.Store, dir, repo, remote string, timeout time.Duration) *puller {
	return &puller{st: st.WithNetTimeout(timeout), dir: dir, repo: repo, remote: remote}
}

// pull pulls, keeps what it left standing, and answers what it did to each
// issue. It waits for a pull that is running, and is not made -- ok is false
// -- when the last one is then less than every old.
func (p *puller) pull(every time.Duration) (did []pullNote, ok bool) {
	p.pulling.Lock()
	defer p.pulling.Unlock()
	if !p.due(every) {
		return nil, false
	}
	p.busy.Lock()
	defer p.busy.Unlock()
	p.mu.Lock()
	repo := p.repo
	p.mu.Unlock()
	note := func(id, what string) pullNote {
		return pullNote{Repo: repo, Remote: p.remote, ID: id, What: what, dir: p.dir}
	}
	var standing []pullNote
	r, err := ops.Pull(p.st, p.remote, false, false)
	if err != nil {
		standing = append(standing, note("", couldNot+err.Error()))
	} else {
		for _, c := range r.Changed {
			did = append(did, note(c.ID, c.What))
		}
		for _, f := range r.Refused {
			standing = append(standing, note(f.ID, "refused: "+f.Reason+" (here "+f.Here+", "+p.remote+" "+f.There+")"))
		}
		for _, e := range r.Failed {
			standing = append(standing, note("", "failed: "+e.Error()))
		}
		srcs := make([]string, 0, len(r.Unreached))
		for src := range r.Unreached {
			srcs = append(srcs, src)
		}
		sort.Strings(srcs)
		for _, src := range srcs {
			standing = append(standing, note("", "source "+src+" not reached, its mirrors are as they were: "+r.Unreached[src]))
		}
	}
	recent := map[string]pulledNote{}
	for _, n := range did {
		if ref, err := p.st.FindRef(n.ID); err == nil {
			if commit, err := p.st.GetRefCommit(ref); err == nil {
				recent[n.ID] = pulledNote{n, commit}
			}
		}
	}
	p.mu.Lock()
	p.last, p.standing, p.recent = time.Now(), standing, recent
	p.mu.Unlock()
	return did, true
}

// took answers what the last pull did to an issue, once, when the issue is
// now at the commit the pull left it at: the look that finds the change the
// pull brought takes it, to log with the change, so every watch reading the
// log has it.
func (p *puller) took(xidr string, now map[string]string) (pullNote, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.recent[xidr]
	if !ok {
		return pullNote{}, false
	}
	delete(p.recent, xidr)
	for _, commit := range now {
		if commit == r.commit {
			return r.note, true
		}
	}
	return pullNote{}, false
}

// named sets the repository's name as notes carry it, which changes as
// repositories are served and not.
func (p *puller) named(repo string) {
	p.mu.Lock()
	p.repo = repo
	p.mu.Unlock()
}

// due says whether the last pull is older than every.
func (p *puller) due(every time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.last) >= every
}

// stands answers what the last pull left standing that wants names: every
// failure, and the refusals of the issues it wants. A refused issue is as
// this clone holds it, the pull having left it alone.
func (p *puller) stands(wants func(xidr string) bool) []pullNote {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []pullNote
	for _, n := range p.standing {
		if n.ID == "" || wants(n.ID) {
			n.Repo = p.repo
			out = append(out, n)
		}
	}
	return out
}

// alarmsSaid is what one watch has said stands. news answers what stands now
// that it has not said, and clear when all it said has gone.
type alarmsSaid map[string]bool

func (s *alarmsSaid) news(standing []pullNote, remote string) []pullNote {
	var out []pullNote
	now := alarmsSaid{}
	for _, n := range standing {
		now[n.key()] = true
		if !(*s)[n.key()] {
			out = append(out, n)
		}
	}
	if len(standing) == 0 && len(*s) > 0 {
		out = append(out, pullNote{Remote: remote, What: clear})
	}
	*s = now
	return out
}

// alarmsHash is what stands, as a watch's cursor carries it: equal for the
// same notes in any order, and empty for none.
func alarmsHash(standing []pullNote) string {
	if len(standing) == 0 {
		return ""
	}
	keys := make([]string, 0, len(standing))
	for _, n := range standing {
		keys = append(keys, n.key())
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:6])
}

// withChanges answers the notes of what a pull did to the issues among
// changes: a pull's note goes with the change it brought, and one whose
// change is not said is not either.
func withChanges(did []pullNote, changes []watchChange) []pullNote {
	said := map[string]bool{}
	for _, ch := range changes {
		said[ch.ID] = true
	}
	var out []pullNote
	for _, n := range did {
		if said[n.ID] {
			out = append(out, n)
		}
	}
	return out
}
