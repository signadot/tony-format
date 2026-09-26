package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type relateConfig struct {
	*cli.Command
	store    issuelib.Store
	relation ops.Relation
}

// RelateCommand returns the relate subcommand.
func RelateCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relation: ops.Related}
	return cli.NewCommandAt(&cfg.Command, "relate").
		WithSynopsis("relate <id1> <id2> - Link two related issues").
		WithRun(cfg.run)
}

// BlocksCommand returns the blocks subcommand.
func BlocksCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relation: ops.Blocks}
	return cli.NewCommandAt(&cfg.Command, "blocks").
		WithSynopsis("blocks <id1> <id2> - Issue id1 blocks id2").
		WithRun(cfg.run)
}

// DuplicateCommand returns the duplicate subcommand.
func DuplicateCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relation: ops.Duplicate}
	return cli.NewCommandAt(&cfg.Command, "duplicate").
		WithSynopsis("duplicate <id1> <id2> - Issue id1 duplicates id2").
		WithRun(cfg.run)
}

func (cfg *relateConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: usage: git issue %s <xidr1> <xidr2>", cli.ErrUsage, cfg.relation)
	}

	from, to, changed, err := ops.Relate(cfg.store, args[0], args[1], cfg.relation)
	if err != nil {
		return err
	}
	id1, id2 := issuelib.FormatID(from.ID), issuelib.FormatID(to.ID)
	if !changed {
		fmt.Fprintf(cc.Out, "Issue %s already has this relationship with %s\n", id1, id2)
		return nil
	}
	switch cfg.relation {
	case ops.Related:
		fmt.Fprintf(cc.Out, "Linked issue %s to %s\n", id1, id2)
	case ops.Blocks:
		fmt.Fprintf(cc.Out, "Issue %s now blocks %s\n", id1, id2)
	case ops.Duplicate:
		fmt.Fprintf(cc.Out, "Issue %s marked as duplicate of %s\n", id1, id2)
	}
	return nil
}
