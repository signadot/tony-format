package commands

import (
	"sort"
	"strings"
	"time"

	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// A watch that pulls. Issues often travel by push and pull, so a watch that
// only read this clone would hear nothing a teammate pushed, and would look
// like a sync that does not work. So a watch can pull a remote every -fetch,
// and says what each pull did beside the changes it brought: an issue
// created, moved on or merged, one refused for a person to decide, a remote
// that could not be reached. What stays true from one pull to the next -- an issue still
// refused, a remote still down -- is said once, and a remote reached again
// after failing says so.

// defaultFetch is how often a watch pulls unless -fetch says otherwise.
const defaultFetch = 30 * time.Second

// pullNote is one thing a watcher's pull did.
type pullNote struct {
	Repo   string `json:"repo,omitempty" jsonschema:"the repository, when more than one is served"`
	Remote string `json:"remote"`
	ID     string `json:"id,omitempty" jsonschema:"the issue, when the note is about one"`
	What   string `json:"what" jsonschema:"what the pull did, as issue_pull says it: created at <sha>, <old>..<new>, merged <a> and <b>; refused: why, for a person to decide; failed: why, for one issue; could not pull: why; reachable again"`

	alarm bool // news on its own: a refusal or a failure
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

// puller pulls one repository from one remote for a watch, and remembers the
// refusals and failures its last pull reported, so a standing one is said
// once.
type puller struct {
	st           issuelib.Store
	repo, remote string
	last         time.Time
	said         map[string]bool // refusals and failures the last pull reported
}

func newPuller(st issuelib.Store, repo, remote string) *puller {
	return &puller{st: st, repo: repo, remote: remote, said: map[string]bool{}}
}

// pull pulls, and answers what it did to the issues wants names, and every
// refusal or failure not said by the pull before it.
func (p *puller) pull(wants func(xidr string) bool) []pullNote {
	p.last = time.Now()
	note := func(id, what string, alarm bool) pullNote {
		return pullNote{Repo: p.repo, Remote: p.remote, ID: id, What: what, alarm: alarm}
	}
	var notes, alarms []pullNote
	r, err := ops.Pull(p.st, p.remote, false, false)
	if err != nil {
		alarms = append(alarms, note("", couldNot+err.Error(), true))
	} else {
		for _, c := range r.Changed {
			if wants(c.ID) {
				notes = append(notes, note(c.ID, c.What, false))
			}
		}
		for _, f := range r.Refused {
			if wants(f.ID) {
				alarms = append(alarms, note(f.ID, "refused: "+f.Reason+" (here "+f.Here+", "+p.remote+" "+f.There+")", true))
			}
		}
		for _, e := range r.Failed {
			alarms = append(alarms, note("", "failed: "+e.Error(), true))
		}
		srcs := make([]string, 0, len(r.Unreached))
		for src := range r.Unreached {
			srcs = append(srcs, src)
		}
		sort.Strings(srcs)
		for _, src := range srcs {
			alarms = append(alarms, note("", "source "+src+" not reached, its mirrors are as they were: "+r.Unreached[src], true))
		}
	}
	said := map[string]bool{}
	for _, a := range alarms {
		key := a.ID + " " + a.What
		said[key] = true
		if !p.said[key] {
			notes = append(notes, a)
		}
	}
	if err == nil && p.failed() {
		notes = append(notes, note("", "reachable again", false))
	}
	p.said = said
	return notes
}

// couldNot begins the note of a pull that could not be made at all: the
// remote not reached, or not there.
const couldNot = "could not pull: "

// failed says whether the last pull could not be made at all.
func (p *puller) failed() bool {
	for key := range p.said {
		if strings.HasPrefix(key, " "+couldNot) {
			return true
		}
	}
	return false
}
