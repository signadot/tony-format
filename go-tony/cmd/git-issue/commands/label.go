package commands

import (
	"fmt"
	"slices"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

type labelConfig struct {
	*cli.Command
	store  issuelib.Store
	remove bool
}

// LabelCommand returns the label subcommand.
func LabelCommand(store issuelib.Store) *cli.Command {
	cfg := &labelConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "label").
		WithSynopsis("label <id> <label> [label...] - Add labels to issue").
		WithRun(cfg.run)
}

// UnlabelCommand returns the unlabel subcommand.
func UnlabelCommand(store issuelib.Store) *cli.Command {
	cfg := &labelConfig{store: store, remove: true}
	return cli.NewCommandAt(&cfg.Command, "unlabel").
		WithSynopsis("unlabel <id> <label> [label...] - Remove labels from issue").
		WithRun(cfg.run)
}

func (cfg *labelConfig) run(cc *cli.Context, args []string) error {
	if len(args) < 2 {
		if cfg.remove {
			return fmt.Errorf("%w: usage: git issue unlabel <xidr> <label> [label...]", cli.ErrUsage)
		}
		return fmt.Errorf("%w: usage: git issue label <xidr> <label> [label...]", cli.ErrUsage)
	}

	xidrOrPrefix := args[0]
	labels := args[1:]

	// Find issue
	ref, err := cfg.store.FindRef(xidrOrPrefix)
	if err != nil {
		return err
	}

	issue, _, err := cfg.store.GetByRef(ref)
	if err != nil {
		return err
	}

	// Normalize labels (lowercase, trimmed)
	for i := range labels {
		labels[i] = strings.ToLower(strings.TrimSpace(labels[i]))
	}

	var action string
	if cfg.remove {
		// Remove labels
		newLabels := make([]string, 0, len(issue.Labels))
		for _, l := range issue.Labels {
			if !slices.Contains(labels, l) {
				newLabels = append(newLabels, l)
			}
		}
		issue.Labels = newLabels
		action = "Removed"
	} else {
		// Add labels (avoid duplicates)
		for _, l := range labels {
			if !slices.Contains(issue.Labels, l) {
				issue.Labels = append(issue.Labels, l)
			}
		}
		slices.Sort(issue.Labels)
		action = "Added"
	}

	// Update issue
	commitMsg := fmt.Sprintf("label: %s %s", strings.ToLower(action), strings.Join(labels, ", "))
	if err := cfg.store.Update(issue, commitMsg, nil); err != nil {
		return fmt.Errorf("failed to update issue: %w", err)
	}

	fmt.Fprintf(cc.Out, "%s label(s) %s on issue %s\n", action, strings.Join(labels, ", "), issue.ID)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(cc.Out, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	return nil
}
