package main

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/buildinfo"
)

// VersionCommand prints which build of o this is.
//
// It writes to cc.Out rather than through MainConfig's output file: -o names
// where a command's object output goes, and a version is not one.
func VersionCommand() *cli.Command {
	return cli.NewCommand("version").
		WithSynopsis("version").
		WithDescription("Print the version of o").
		WithRun(func(cc *cli.Context, args []string) error {
			fmt.Fprintln(cc.Out, buildinfo.Line("o"))
			return nil
		})
}
