package commands

import (
	"fmt"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type labelConfig struct {
	*cli.Command
	store  issuelib.Store
	remove bool
}

// LabelCommand returns the label subcommand. A label key=value sets the key,
// replacing any value it had; labels beginning git-issue- are reserved for
// conventions git-issue or a program driving it defines.
func LabelCommand(store issuelib.Store) *cli.Command {
	cfg := &labelConfig{store: store}
	return cli.NewCommandAt(&cfg.Command, "label").
		WithSynopsis("label <id> <label|key=value> [...] - Add labels to issue; key=value replaces the key's value").
		WithRun(cfg.run)
}

// UnlabelCommand returns the unlabel subcommand. A key given without a value
// removes it whatever its value.
func UnlabelCommand(store issuelib.Store) *cli.Command {
	cfg := &labelConfig{store: store, remove: true}
	return cli.NewCommandAt(&cfg.Command, "unlabel").
		WithSynopsis("unlabel <id> <label|key|key=value> [...] - Remove labels from issue; a bare key removes any value").
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
	for i := range labels {
		labels[i] = issuelib.NormalizeLabel(labels[i])
	}

	var add, remove []string
	action := "Added"
	if cfg.remove {
		remove, action = labels, "Removed"
	} else {
		add = labels
	}
	issue, err := ops.Label(cfg.store, xidrOrPrefix, add, remove)
	if err != nil {
		if strings.HasSuffix(err.Error(), "has no key") {
			return fmt.Errorf("%w: %v", cli.ErrUsage, err)
		}
		return fmt.Errorf("failed to update issue: %w", err)
	}

	fmt.Fprintf(cc.Out, "%s label(s) %s on issue %s\n", action, strings.Join(labels, ", "), issue.ID)
	if len(issue.Labels) > 0 {
		fmt.Fprintf(cc.Out, "Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	return nil
}
