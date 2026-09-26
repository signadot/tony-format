package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

// ExtCommand returns the ext subcommand: another repository's issues, mirrored
// here so that a relation to one resolves from this repository alone.
//
//	git issue ext add <source> <url|path>    record where a repository is
//	git issue ext fetch <source> <id>        mirror one of its issues here
//	git issue ext refresh [<source>]         bring mirrors up to their sources
//	git issue ext remove <id>                drop a mirror and the relations naming it
//	git issue ext list                       the sources, and when each was last fetched
func ExtCommand(store issuelib.Store) *cli.Command {
	return cli.NewCommand("ext").
		WithSynopsis("ext <add|fetch|refresh|remove|list> - Another repository's issues, mirrored here").
		WithSubs(
			cli.NewCommand("add").
				WithSynopsis("add <source> <url> - Record where a repository is, under a name").
				WithRun(func(cc *cli.Context, args []string) error {
					if len(args) != 2 {
						return fmt.Errorf("%w: usage: git issue ext add <source> <url>", cli.ErrUsage)
					}
					if err := ops.SourceAdd(store, args[0], args[1]); err != nil {
						return err
					}
					fmt.Fprintf(cc.Out, "Source %s is %s\n", args[0], args[1])
					return nil
				}),
			cli.NewCommand("fetch").
				WithSynopsis("fetch <source> <id> - Mirror one of a source's issues here, read-only").
				WithRun(func(cc *cli.Context, args []string) error {
					if len(args) != 2 {
						return fmt.Errorf("%w: usage: git issue ext fetch <source> <full id>", cli.ErrUsage)
					}
					issue, err := ops.Mirror(store, args[0], args[1])
					if err != nil {
						return err
					}
					fmt.Fprintf(cc.Out, "Mirrored %s from %s: %s\n", issue.ID, args[0], issue.Title)
					return nil
				}),
			cli.NewCommand("refresh").
				WithSynopsis("refresh [<source>] - Bring mirrors up to what their sources hold").
				WithRun(func(cc *cli.Context, args []string) error {
					sources, err := ops.Sources(store)
					if err != nil {
						return err
					}
					for _, src := range sources {
						if len(args) > 0 && src.Name != args[0] {
							continue
						}
						n, err := ops.Refresh(store, src.Name)
						if err != nil {
							return err
						}
						fmt.Fprintf(cc.Out, "%s: %d mirror(s) refreshed\n", src.Name, n)
					}
					return nil
				}),
			cli.NewCommand("remove").
				WithSynopsis("remove <id> - Drop a mirror, and the relations that named it").
				WithRun(func(cc *cli.Context, args []string) error {
					if len(args) != 1 {
						return fmt.Errorf("%w: usage: git issue ext remove <id>", cli.ErrUsage)
					}
					touched, err := ops.Unmirror(store, args[0])
					if err != nil {
						return err
					}
					fmt.Fprintf(cc.Out, "Removed the mirror; %d issue(s) no longer name it\n", touched)
					return nil
				}),
			cli.NewCommand("list").
				WithSynopsis("list - The sources this repository knows").
				WithRun(func(cc *cli.Context, args []string) error {
					sources, err := ops.Sources(store)
					if err != nil {
						return err
					}
					if len(sources) == 0 {
						fmt.Fprintln(cc.Out, "No sources")
						return nil
					}
					for _, src := range sources {
						when := "never fetched"
						if !src.Fetched.IsZero() {
							when = "fetched " + src.Fetched.Local().Format("2006-01-02 15:04")
						}
						fmt.Fprintf(cc.Out, "%s  %s  (%s)\n", src.Name, src.URL, when)
					}
					return nil
				}),
		)
}
