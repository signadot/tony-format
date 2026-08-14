package commands

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/buildinfo"
	"github.com/signadot/tony-format/go-tony/cmd/git-issue/issuelib"
)

// VersionCommand returns the version subcommand. It takes the store it does not
// use, so that Root can build it beside every other subcommand.
func VersionCommand(store issuelib.Store) *cli.Command {
	return cli.NewCommand("version").
		WithSynopsis("version - Print the version of git-issue").
		WithRun(func(cc *cli.Context, args []string) error {
			fmt.Fprintln(cc.Out, buildinfo.Line("git-issue"))
			return nil
		})
}
