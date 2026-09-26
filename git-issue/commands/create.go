package commands

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/git-issue/issuelib"
	"github.com/signadot/tony-format/git-issue/ops"
)

type createConfig struct {
	*cli.Command
	store issuelib.Store
	Body  string `cli:"name=body aliases=b desc='Issue body/description'"`
}

// CreateCommand returns the create subcommand.
func CreateCommand(store issuelib.Store) *cli.Command {
	cfg := &createConfig{store: store}
	opts, _ := cli.StructOpts(cfg)
	return cli.NewCommandAt(&cfg.Command, "create").
		WithSynopsis("create <title> - Create new issue").
		WithOpts(opts...).
		WithRun(cfg.run)
}

func (cfg *createConfig) run(cc *cli.Context, args []string) error {
	args, err := cfg.Parse(cc, args)
	if err != nil {
		return err
	}

	if len(args) < 1 {
		return fmt.Errorf("%w: usage: git issue create <title>", cli.ErrUsage)
	}

	title := strings.Join(args, " ")

	// The body: -body, or stdin when it is not a terminal, or the editor.
	var descBody string
	if cfg.Body != "" {
		descBody = cfg.Body
	} else if stat, _ := os.Stdin.Stat(); (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		descBody = string(data)
	} else {
		initialContent := fmt.Sprintf(`# %s

# Enter description above.
# Lines starting with # will be ignored.
# Save and close the editor to submit, or leave empty to cancel.
`, title)
		var err error
		descBody, err = issuelib.EditInEditor(initialContent)
		if err != nil {
			return fmt.Errorf("editor failed: %w", err)
		}
	}

	issue, err := ops.Create(cfg.store, title, descBody)
	if err != nil {
		return err
	}

	fmt.Fprintf(cc.Out, "Created issue %s\n", issuelib.FormatID(issue.ID))
	fmt.Fprintf(cc.Out, "Ref: %s\n", issue.Ref)

	return nil
}
