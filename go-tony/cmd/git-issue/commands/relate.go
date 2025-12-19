package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type relateConfig struct {
	*cli.Command
	store        issuelib.Store
	relationType string
}

// RelateCommand returns the relate subcommand.
func RelateCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relationType: "related"}
	return cli.NewCommandAt(&cfg.Command, "relate").
		WithSynopsis("relate <id1> <id2> - Link two related issues").
		WithRun(cfg.run)
}

// BlocksCommand returns the blocks subcommand.
func BlocksCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relationType: "blocks"}
	return cli.NewCommandAt(&cfg.Command, "blocks").
		WithSynopsis("blocks <id1> <id2> - Issue id1 blocks id2").
		WithRun(cfg.run)
}

// DuplicateCommand returns the duplicate subcommand.
func DuplicateCommand(store issuelib.Store) *cli.Command {
	cfg := &relateConfig{store: store, relationType: "duplicate"}
	return cli.NewCommandAt(&cfg.Command, "duplicate").
		WithSynopsis("duplicate <id1> <id2> - Issue id1 duplicates id2").
		WithRun(cfg.run)
}

func (cfg *relateConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("%w: usage: git issue %s <id1> <id2>", cli.ErrUsage, cfg.relationType)
	}

	id1, err := issuelib.ParseID(args[0])
	if err != nil {
		return err
	}

	id2, err := issuelib.ParseID(args[1])
	if err != nil {
		return err
	}

	id1Str := issuelib.FormatID(id1)
	id2Str := issuelib.FormatID(id2)

	// Find and verify both issues exist
	ref1, err := cfg.store.FindRef(id1)
	if err != nil {
		return err
	}

	ref2, err := cfg.store.FindRef(id2)
	if err != nil {
		return err
	}

	// Read first issue
	issue1, _, err := cfg.store.GetByRef(ref1)
	if err != nil {
		return err
	}

	// Add relationship
	var added bool
	switch cfg.relationType {
	case "related":
		if !issuelib.Contains(issue1.RelatedIssues, id2Str) {
			issue1.RelatedIssues = append(issue1.RelatedIssues, id2Str)
			added = true
		}
	case "blocks":
		if !issuelib.Contains(issue1.Blocks, id2Str) {
			issue1.Blocks = append(issue1.Blocks, id2Str)
			added = true
		}
	case "duplicate":
		if !issuelib.Contains(issue1.Duplicates, id2Str) {
			issue1.Duplicates = append(issue1.Duplicates, id2Str)
			added = true
		}
	}

	if !added {
		fmt.Fprintf(cc.Out, "Issue #%s already has this relationship with #%s\n", id1Str, id2Str)
		return nil
	}

	// Save updated issue1
	var commitMsg string
	switch cfg.relationType {
	case "related":
		commitMsg = fmt.Sprintf("relate: link to #%s", id2Str)
	case "blocks":
		commitMsg = fmt.Sprintf("blocks: #%s", id2Str)
	case "duplicate":
		commitMsg = fmt.Sprintf("duplicate: of #%s", id2Str)
	}

	if err := cfg.store.Update(issue1, commitMsg, nil); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	// For blocks relationship, add reciprocal blocked_by to second issue
	if cfg.relationType == "blocks" {
		issue2, _, err := cfg.store.GetByRef(ref2)
		if err == nil && !issuelib.Contains(issue2.BlockedBy, id1Str) {
			issue2.BlockedBy = append(issue2.BlockedBy, id1Str)
			commitMsg2 := fmt.Sprintf("blocked-by: #%s", id1Str)
			_ = cfg.store.Update(issue2, commitMsg2, nil)
		}
	}

	// Print result
	switch cfg.relationType {
	case "related":
		fmt.Fprintf(cc.Out, "Linked issue #%s to #%s\n", id1Str, id2Str)
	case "blocks":
		fmt.Fprintf(cc.Out, "Issue #%s now blocks #%s\n", id1Str, id2Str)
	case "duplicate":
		fmt.Fprintf(cc.Out, "Issue #%s marked as duplicate of #%s\n", id1Str, id2Str)
	}

	return nil
}
