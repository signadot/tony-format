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
	desc := "how often to look for changes (default 5s)"
	if zeroMeans != "" {
		desc = "how often to look for changes (default 5s; 0 " + zeroMeans + ")"
	}
	return &cli.Opt{
		Name:        "poll",
		Description: desc,
		Type: cli.NamedFuncOpt(cli.FuncOpt(func(cc *cli.Context, a string) (any, error) {
			d, err := time.ParseDuration(a)
			if err != nil && a == "0" {
				d, err = 0, nil
			}
			if err != nil {
				return nil, fmt.Errorf("-poll %q: %w", a, err)
			}
			if d < 0 || (d == 0 && zeroMeans == "") {
				return nil, fmt.Errorf("-poll %q: must be more than zero", a)
			}
			*dst = d
			return 0, nil
		}), "(duration)"),
	}
}

// issueRefs is one look at a repository's issues: for each id, the refs that
// hold it and their commits. An issue is one ref, open or closed; its id can
// also be held by a mirror of it, so an id can have two.
type issueRefs map[string]map[string]string

// lookAt takes one look at a repository's issue refs.
func lookAt(st issuelib.Store) (issueRefs, error) {
	tips, err := st.Tips()
	if err != nil {
		return nil, err
	}
	look := issueRefs{}
	for ref, commit := range tips {
		xidr, err := issuelib.XIDRFromRef(ref)
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
	What    []string  `json:"what" jsonschema:"what was done to it, oldest first: one line per commit it gained (comment: ..., edit: ..., label: ...), then closed or reopened if it moved"`
}

// describe says what changed about one issue between two looks at its refs:
// the issue as that look saw it, and the subjects of the commits it gained. An issue
// is read from its own ref, open or closed, before a mirror of it.
func describe(st issuelib.Store, repo, xidr string, was, now map[string]string) watchChange {
	ch := watchChange{ID: xidr, Repo: repo, Labels: []string{}, What: []string{}}
	ref := ""
	for r := range now {
		if ref == "" || !issuelib.IsExtRef(r) {
			ref = r
		}
	}
	if ref == "" {
		ch.Status = "gone"
		return ch
	}
	// Read at the commit the look saw: the ref may have moved on since, or
	// gone -- a close commits on the open ref, then moves it.
	if issue, _, err := st.GetByRef(now[ref]); err == nil {
		issue.Ref = ref
		ch.Status, ch.Title, ch.Updated = issuelib.StatusOf(issue), issue.Title, issue.Updated
		if issue.Labels != nil {
			ch.Labels = issue.Labels
		}
	}
	var not []string
	for _, commit := range was {
		not = append(not, commit)
	}
	if subjects, err := st.Subjects(now[ref], not); err == nil {
		ch.What = append(ch.What, subjects...)
	}
	wasOpen, wasClosed := hasPrefixRef(was, issuelib.OpenPrefix), hasPrefixRef(was, issuelib.ClosedPrefix)
	switch {
	case issuelib.IsClosedRef(ref) && wasOpen:
		ch.What = append(ch.What, "closed")
	case strings.HasPrefix(ref, issuelib.OpenPrefix) && wasClosed:
		ch.What = append(ch.What, "reopened")
	}
	return ch
}

func hasPrefixRef(refs map[string]string, prefix string) bool {
	for ref := range refs {
		if strings.HasPrefix(ref, prefix) {
			return true
		}
	}
	return false
}

// watchFilter is which issues a watcher reports: the ids named, or those
// carrying a label, or -- with neither -- every one.
type watchFilter struct {
	ids   map[string]bool
	label string
}

func (f watchFilter) matches(ch watchChange) bool {
	if len(f.ids) > 0 && !f.ids[ch.ID] {
		return false
	}
	return f.label == "" || issuelib.Contains(ch.Labels, f.label)
}

// oneLine is a change as `git issue watch` prints it.
func (ch watchChange) oneLine() string {
	var b strings.Builder
	if ch.Repo != "" {
		b.WriteString(ch.Repo + "  ")
	}
	b.WriteString(ch.ID + "  " + ch.Status)
	if ch.Title != "" {
		b.WriteString("  " + ch.Title)
	}
	if len(ch.What) > 0 {
		b.WriteString("  -- " + strings.Join(ch.What, "; "))
	}
	return b.String()
}
