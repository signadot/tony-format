package commands

import (
	"fmt"
	"io"
	"os"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type editConfig struct {
	*cli.Command
	store issuelib.Store
	Title string `cli:"name=title aliases=t desc='the new title'"`
	Body  string `cli:"name=body aliases=b desc='the new body'"`
}

// EditCommand returns the edit subcommand: what an issue says, changed in place.
// --title and --body each replace their half and leave the other; with neither,
// a piped stdin is the new body, and a terminal opens $EDITOR on the whole
// description -- title line and body -- as it is stored.
func EditCommand(store issuelib.Store) *cli.Command {
	cfg := &editConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "edit").
		WithSynopsis("edit <id> [--title <t>] [--body <b>] - Change an issue's title or body ($EDITOR or stdin)").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *editConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}
	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue edit <xidr> [--title <t>] [--body <b>]", cli.ErrUsage)
	}
	id := args[0]

	var title, body *string
	switch {
	case cfg.Title != "" || cfg.Body != "":
		if cfg.Title != "" {
			title = &cfg.Title
		}
		if cfg.Body != "" {
			body = &cfg.Body
		}
	default:
		if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			s := string(data)
			body = &s
			break
		}
		// The editor opens on the description as stored, so headings in the body
		// survive: nothing here strips a line for starting with "#", as the
		// prompts for create and comment do.
		sh, err := ops.Show(cfg.store, id)
		if err != nil {
			return err
		}
		edited, err := issuelib.EditTextInEditor(sh.Description)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
		t, b := ops.SplitDescription(edited)
		title, body = &t, &b
	}

	issue, err := ops.Edit(cfg.store, id, title, body)
	if err != nil {
		return err
	}
	fmt.Fprintf(cc.Out, "Edited issue %s\n", issuelib.FormatID(issue.ID))
	return nil
}
