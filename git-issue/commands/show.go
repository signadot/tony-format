package commands

import (
	"fmt"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type showConfig struct {
	*cli.Command
	store issuelib.Store
}

// ShowCommand returns the show subcommand.
func ShowCommand(store issuelib.Store) *cli.Command {
	cfg := &showConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "show").
		WithSynopsis("show <id> - Show issue details").
		WithRun(cfg.run)
}

func (cfg *showConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue show <xidr>", cli.ErrUsage)
	}

	sh, err := ops.Show(cfg.store, args[0])
	if err != nil {
		return err
	}
	writeShown(cc, sh)
	return nil
}

// writeShown is the issue as a person reads it: header, description, links,
// relations, then the discussion in order and the attachments by name.
func writeShown(cc *cli.Context, sh *ops.Shown) {
	issue := sh.Issue
	fmt.Fprintf(cc.Out, "Issue %s [%s]\n", issuelib.FormatID(issue.ID), sh.Status)
	fmt.Fprintf(cc.Out, "Ref: %s\n", sh.Ref)
	if sh.Source != "" {
		fmt.Fprintf(cc.Out, "Mirror of %s's issue, read-only here; `git issue ext refresh %s` follows it\n", sh.Source, sh.Source)
	}
	if len(issue.Labels) > 0 {
		fmt.Fprintf(cc.Out, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	fmt.Fprintln(cc.Out)

	fmt.Fprintln(cc.Out, sh.Description)
	fmt.Fprintln(cc.Out)

	if len(sh.Commits) > 0 {
		fmt.Fprintln(cc.Out, "Linked commits:")
		for _, info := range sh.Commits {
			fmt.Fprintf(cc.Out, "  %s\n", info)
		}
		fmt.Fprintln(cc.Out)
	}

	if len(issue.Branches) > 0 {
		fmt.Fprintln(cc.Out, "Linked branches:")
		for _, branch := range issue.Branches {
			fmt.Fprintf(cc.Out, "  %s\n", branch)
		}
		fmt.Fprintln(cc.Out)
	}

	writeRelated(cc, "Related issues:", sh.Related)
	writeRelated(cc, "Blocks:", sh.Blocks)
	writeRelated(cc, "Blocked by:", sh.BlockedBy)
	writeRelated(cc, "Duplicates:", sh.Duplicates)

	if len(sh.Comments) > 0 {
		fmt.Fprintln(cc.Out, "Discussion:")
		fmt.Fprintln(cc.Out)
		for _, c := range sh.Comments {
			fmt.Fprintf(cc.Out, "--- %s ---\n", c.Path)
			fmt.Fprint(cc.Out, c.Content)
			fmt.Fprintln(cc.Out)
		}
	}

	if len(sh.Attachments) > 0 {
		fmt.Fprintln(cc.Out, "Attachments:")
		for _, file := range sh.Attachments {
			fmt.Fprintf(cc.Out, "  %s\n", file)
		}
		fmt.Fprintln(cc.Out)
	}
}

func writeRelated(cc *cli.Context, title string, linked []ops.Linked) {
	if len(linked) == 0 {
		return
	}
	fmt.Fprintln(cc.Out, title)
	for _, l := range linked {
		if l.Err != "" {
			fmt.Fprintf(cc.Out, "  %s (%s)\n", l.ID, l.Err)
			continue
		}
		fmt.Fprintf(cc.Out, "  %s %s[%s]%s %s\n",
			l.ID, issuelib.StatusColor(l.Status), l.Status, issuelib.ColorReset, l.Title)
	}
	fmt.Fprintln(cc.Out)
}
