package commands

import (
	"fmt"
	"sort"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// What a push and a pull have in common is decided in ops (ops.Push, ops.Pull):
// they decide from the same plans, report in the same shape, and refuse the
// same thing. What is here is how that report reads to a person.
//
// A sync that refused or failed anything exits non-zero. An operator reads the
// lines; a script reads the status, and must not have to parse prose to learn
// that an issue was left behind.

// writeReport reports the run and answers whether it was whole. What was refused
// or failed is named, because the point of refusing is that a person decides.
func writeReport(cc *cli.Context, r *ops.Report) error {
	for _, c := range r.Changed {
		fmt.Fprintf(cc.Out, "  %s  %s\n", issuelib.FormatID(c.ID), c.What)
	}

	verb := "changed"
	if r.DryRun {
		verb = "to change"
	}
	fmt.Fprintf(cc.Out, "%d issue(s) %s, %d unchanged", len(r.Changed), verb, r.Unchanged)
	if len(r.Refused) > 0 {
		fmt.Fprintf(cc.Out, ", %d refused", len(r.Refused))
	}
	if len(r.Failed) > 0 {
		fmt.Fprintf(cc.Out, ", %d failed", len(r.Failed))
	}
	fmt.Fprintln(cc.Out, ".")

	for _, ref := range r.Refused {
		writeRefusal(cc, r, ref)
	}
	for _, err := range r.Failed {
		fmt.Fprintf(cc.Err, "  %v\n", err)
	}
	if len(r.OldClient) > 0 {
		writeOldClient(cc, r)
	}
	for _, src := range sortedKeys(r.Refreshed) {
		fmt.Fprintf(cc.Out, "  %s: %d mirror(s) refreshed\n", src, r.Refreshed[src])
	}
	for _, src := range sortedKeys(r.Unreached) {
		fmt.Fprintf(cc.Out, "  %s could not be reached; its mirrors are as they were: %s\n", src, r.Unreached[src])
	}
	return r.Err()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// writeRefusal names one issue nobody can settle automatically, and says what
// saying so again would do.
func writeRefusal(cc *cli.Context, r *ops.Report, ref ops.Refusal) {
	title := ""
	if ref.Title != "" {
		title = "  " + ref.Title
	}
	fmt.Fprintf(cc.Out, "  %s%s\n", issuelib.FormatID(ref.ID), title)
	fmt.Fprintf(cc.Out, "      %s.\n", ref.Reason)

	if ref.Split {
		fmt.Fprintf(cc.Out, "      %s holds this issue at two tips that disagree, which a "+
			"git-issue older than this one can cause.\n", r.Remote)
	} else {
		fmt.Fprintf(cc.Out, "      here %s, %s %s.\n", ref.Here, r.Remote, ref.There)
	}
	takes, other := "the remote's", "this clone's"
	command := "pull"
	if !r.Pulling {
		takes, other = "this clone's", "the remote's"
		command = "push"
	}
	fmt.Fprintf(cc.Out, "      `git issue %s --force` takes %s; %s stays in the ref's reflog.\n",
		command, takes, other)
}

// writeOldClient says that someone is running a git-issue too old to see the
// current refs. Nothing of theirs is lost -- their work is adopted and the next
// push clears where they put it -- but it will keep happening until they
// upgrade, and the people who can tell them are the ones reading this.
func writeOldClient(cc *cli.Context, r *ops.Report) {
	var ids []string
	for _, id := range r.OldClient {
		ids = append(ids, issuelib.FormatID(id))
	}
	fmt.Fprintf(cc.Out, "\nA git-issue older than this one has pushed %d issue(s) to %s: %s.\n",
		len(ids), r.Remote, join(ids))
	fmt.Fprintf(cc.Out, "Nothing was lost. Whoever pushed them should upgrade.\n")
}

// join lists ids for a person, and stops before a line of them.
func join(ids []string) string {
	const most = 6
	out := ""
	for i, id := range ids {
		if i == most {
			return fmt.Sprintf("%s and %d more", out, len(ids)-most)
		}
		if i > 0 {
			out += ", "
		}
		out += id
	}
	return out
}

// shortSHA abbreviates a commit for a message to a person.
func shortSHA(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}
	return commit
}
