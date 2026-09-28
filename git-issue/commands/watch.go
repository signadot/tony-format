package commands

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
)

// A watch is what changed in a repository's issues between two looks at its
// refs. There are two watchers over the same looks: the MCP server, which looks
// on a tick and after each of its own tools and hands what it finds to
// issue_watch and to resource subscribers (mcp_resources.go), and `git issue
// watch`, which looks on the same tick and prints what it finds -- for an agent
// whose host wakes it on a command's output rather than on a tool's answer
// (vegmw7bmh12ks11mq1n0). Both describe a change the same way (describe).

// watchInterval is how often a watcher looks at the refs unless -poll says
// otherwise: a change made beside it -- a comment from a shell, a pull in
// another clone -- is found within it.
const watchInterval = 5 * time.Second

// pollOpt is -poll: how often a watcher looks, as a duration. With zero, the
// MCP server takes no look but after its own tools; `git issue watch` would
// never answer, so it refuses zero.
func pollOpt(dst *time.Duration, zeroMeans string) *cli.Opt {
	return durationOpt("poll", "how often to look for changes (default 5s", dst, zeroMeans)
}

// fetchOpt is -fetch: how often a watch that pulls pulls.
func fetchOpt(dst *time.Duration) *cli.Opt {
	return durationOpt("fetch", "how often to pull the remote (default 30s", dst, "")
}

// durationOpt is a duration option, more than zero unless zeroMeans says what
// zero is. desc is open for the default and zero's meaning to close.
func durationOpt(name, desc string, dst *time.Duration, zeroMeans string) *cli.Opt {
	if zeroMeans != "" {
		desc += "; 0 " + zeroMeans
	}
	return &cli.Opt{
		Name:        name,
		Description: desc + ")",
		Type: cli.NamedFuncOpt(cli.FuncOpt(func(cc *cli.Context, a string) (any, error) {
			d, err := time.ParseDuration(a)
			if err != nil && a == "0" {
				d, err = 0, nil
			}
			if err != nil {
				return nil, fmt.Errorf("-%s %q: %w", name, a, err)
			}
			if d < 0 || (d == 0 && zeroMeans == "") {
				return nil, fmt.Errorf("-%s %q: must be more than zero", name, a)
			}
			*dst = d
			return 0, nil
		}), "(duration)"),
	}
}

// issueRefs is one look at a repository's issues: for each id, the refs that
// hold it and their commits. An issue is one ref, open or closed; its id can
// also be held by a mirror of it, so an id can have two. The tracking refs
// that copy them, a remote's as the last fetch or push left it, are among
// them: what a change says of whether the remote holds it is read from those
// (own and trackedBy tell them apart).
type issueRefs map[string]map[string]string

// lookAt takes one look at a repository's issue refs.
func lookAt(st issuelib.Store) (issueRefs, error) {
	tips, err := st.Tips()
	if err != nil {
		return nil, err
	}
	look := issueRefs{}
	for ref, commit := range tips {
		name := ref
		if _, copied, ok := issuelib.Tracked(ref); ok {
			name = copied
		}
		xidr, err := issuelib.XIDRFromRef(name)
		if err != nil {
			continue
		}
		if look[xidr] == nil {
			look[xidr] = map[string]string{}
		}
		look[xidr][ref] = commit
	}
	return look, nil
}

// moved answers the ids whose refs differ between two looks -- a new commit, a
// move between open and closed, an issue that came or went -- sorted.
func moved(was, now issueRefs) []string {
	var ids []string
	for xidr, refs := range now {
		if !sameRefs(was[xidr], refs) {
			ids = append(ids, xidr)
		}
	}
	for xidr := range was {
		if _, ok := now[xidr]; !ok {
			ids = append(ids, xidr)
		}
	}
	sort.Strings(ids)
	return ids
}

// own is an issue's refs in this clone, without a remote's copies of them.
func own(refs map[string]string) map[string]string {
	out := map[string]string{}
	for ref, commit := range refs {
		if _, _, ok := issuelib.Tracked(ref); !ok {
			out[ref] = commit
		}
	}
	return out
}

// onRemote says whether remote's copies of an issue, among refs, hold commit:
// whether the remote had it at the last fetch or push.
func onRemote(st issuelib.Store, refs map[string]string, remote, commit string) bool {
	for ref, tip := range refs {
		if r, _, ok := issuelib.Tracked(ref); ok && r == remote && st.Holds(tip, commit) {
			return true
		}
	}
	return false
}

func sameRefs(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for ref, commit := range a {
		if b[ref] != commit {
			return false
		}
	}
	return true
}

// watchChange is one issue that changed, as a watcher reports it.
type watchChange struct {
	ID      string    `json:"id"`
	Repo    string    `json:"repo,omitempty" jsonschema:"the repository, when more than one is served"`
	Status  string    `json:"status" jsonschema:"open or closed as it is now, or gone when no ref holds it any more"`
	Title   string    `json:"title,omitempty"`
	Labels  []string  `json:"labels"`
	Updated time.Time `json:"updated,omitzero"`
	What    []string  `json:"what" jsonschema:"what was done to it, oldest first: one line per commit it gained (comment: ..., edit: ..., label: ...), then closed or reopened if it moved with no commit saying so; arrived for an issue that came with no commit since the watch began; pushed to <remote> when the remote came to hold a change that was local"`
	On      string    `json:"on,omitempty" jsonschema:"the remote, when it holds the issue as the change left it -- as this clone knows the remote, from its last fetch or push -- or local when it does not, and the change is not pushed; empty when there is no remote to ask. The remote is the one the watch pulls, or origin. It is the remote as this clone last saw it: a remote rewritten since by another clone (a forced push, a deleted ref) can make it wrong until the next fetch"`

	labelsBefore []string // its labels before the change, for a label filter
}

// describe says what changed about one issue between two looks at its refs:
// the issue as the later look saw it, and the subjects of the commits it
// gained. An issue new to the watcher lists only commits made since the watch
// began, so one pulled in does not bring its whole history; with none, it
// arrived. An issue is read from its own ref, open or closed, before a mirror
// of it.
//
// A look can land inside a move -- a close or reopen writes the new ref, then
// deletes the old -- and find the issue at both. The ref read is then the one
// the move is going to, and the look that finds the old one gone has nothing
// to say: ok is false when the change adds nothing but a ref's going.
//
// With a remote, the change says whether the remote holds the issue as the
// change left it (On), from the remote's copies among the refs. A change to
// those copies alone is said when the remote comes to hold what it did not
// -- a push of a change that was local -- and not otherwise: a fetch that
// finds the remote ahead is news once a pull brings it here.
func describe(st issuelib.Store, repo, remote, xidr string, allWas, allNow map[string]string, began time.Time) (watchChange, bool) {
	ch := watchChange{ID: xidr, Repo: repo, Labels: []string{}, What: []string{}}
	was, now := own(allWas), own(allNow)
	if old := readAt(st, was, nil); old != nil {
		ch.labelsBefore = old.Labels
	}
	ref := issueRef(now, was)
	if ref == "" {
		ch.Status = "gone"
		return ch, len(was) > 0
	}
	if commit, ok := was[ref]; ok && commit == now[ref] && len(now) < len(was) {
		return ch, false
	}
	if issue := readAt(st, now, was); issue != nil {
		ch.Status, ch.Title, ch.Updated = issuelib.StatusOf(issue), issue.Title, issue.Updated
		if issue.Labels != nil {
			ch.Labels = issue.Labels
		}
	}
	if remote != "" {
		ch.On = "local"
		if onRemote(st, allNow, remote, now[ref]) {
			ch.On = remote
		}
	}
	if sameRefs(was, now) {
		if remote == "" || ch.On != remote || onRemote(st, allWas, remote, now[ref]) {
			return ch, false
		}
		ch.What = append(ch.What, "pushed to "+remote)
		return ch, true
	}
	var not []string
	for _, commit := range was {
		not = append(not, commit)
	}
	since := time.Time{}
	if len(was) == 0 {
		since = began
	}
	if subjects, err := st.Subjects(now[ref], not, since); err == nil {
		ch.What = append(ch.What, subjects...)
	}
	switch {
	case len(was) == 0 && len(ch.What) == 0:
		ch.What = append(ch.What, "arrived")
	case issuelib.IsClosedRef(ref) && hasPrefixRef(was, issuelib.OpenPrefix) && !recorded(ch.What, "close"):
		ch.What = append(ch.What, "closed")
	case strings.HasPrefix(ref, issuelib.OpenPrefix) && hasPrefixRef(was, issuelib.ClosedPrefix) && !recorded(ch.What, "reopen"):
		ch.What = append(ch.What, "reopened")
	}
	return ch, true
}

// recorded says whether a commit among subjects is the operation op ("close",
// or "close: closed by ..."; "reopen"), as ops writes it. A move its own commit
// records is not said twice; one without -- a pull bringing the ref across --
// is said as closed or reopened.
func recorded(subjects []string, op string) bool {
	for _, s := range subjects {
		if s == op || strings.HasPrefix(s, op+":") {
			return true
		}
	}
	return false
}

// issueRef is the ref to read an issue from among those holding its id: its
// own, open or closed, before a mirror of it; and of its own, one that was not
// there before -- mid-move, the one the move is going to.
func issueRef(refs, was map[string]string) string {
	rank := func(r string) int {
		switch {
		case issuelib.IsExtRef(r):
			return 0
		case was[r] == "":
			return 2
		}
		return 1
	}
	ref := ""
	for r := range refs {
		if ref == "" || rank(r) > rank(ref) || (rank(r) == rank(ref) && r < ref) {
			ref = r
		}
	}
	return ref
}

// readAt reads an issue at the commit a look saw, not at its ref, which may
// have moved on since, or gone -- a close commits on the open ref, then moves
// it. Nil when no ref held it.
func readAt(st issuelib.Store, refs, was map[string]string) *issuelib.Issue {
	ref := issueRef(refs, was)
	if ref == "" {
		return nil
	}
	issue, _, err := st.GetByRef(refs[ref])
	if err != nil {
		return nil
	}
	issue.Ref = ref
	return issue
}

func hasPrefixRef(refs map[string]string, prefix string) bool {
	for ref := range refs {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

// watchFilter is which issues a watcher reports: those in the repositories
// named, the ids named, and those carrying a label before the change or after
// it -- so the label's removal is heard. Each scope given must match; with
// none, every issue.
type watchFilter struct {
	dirs  map[string]bool // repository directories; the MCP server's scope
	ids   map[string]bool
	label string
}

// wants says whether f can match an issue by its repository and id alone:
// what a watcher asks before reading the issue to describe it.
func (f watchFilter) wants(dir, xidr string) bool {
	return (len(f.dirs) == 0 || f.dirs[dir]) && (len(f.ids) == 0 || f.ids[xidr])
}

// wantsIssue says whether f matches an issue as this clone holds it -- its
// repository, id and labels -- for what is said of an issue with no change to
// describe: a refusal, which leaves the issue as it was.
func (f watchFilter) wantsIssue(st issuelib.Store, dir, xidr string) bool {
	if !f.wants(dir, xidr) {
		return false
	}
	if f.label == "" {
		return true
	}
	issue, _, err := st.Get(xidr)
	return err == nil && issuelib.Contains(issue.Labels, f.label)
}

// matches says whether f matches a described change, whose repository wants
// has already answered for.
func (f watchFilter) matches(ch watchChange) bool {
	if len(f.ids) > 0 && !f.ids[ch.ID] {
		return false
	}
	return f.label == "" || issuelib.Contains(ch.Labels, f.label) || issuelib.Contains(ch.labelsBefore, f.label)
}

// oneLine is a change as `git issue watch` prints it.
func (ch watchChange) oneLine() string {
	var b strings.Builder
	if ch.Repo != "" {
		b.WriteString(ch.Repo + "  ")
	}
	b.WriteString(ch.ID + "  " + ch.Status)
	switch ch.On {
	case "":
	case "local":
		b.WriteString(" (local)")
	default:
		b.WriteString(" (on " + ch.On + ")")
	}
	if ch.Title != "" {
		b.WriteString("  " + ch.Title)
	}
	if len(ch.What) > 0 {
		b.WriteString("  -- " + strings.Join(ch.What, "; "))
	}
	return b.String()
}
