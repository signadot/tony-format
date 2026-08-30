package main

import (
	"fmt"

	"github.com/scott-cotton/cli"
	"github.com/signadot/tony-format/go-tony/buildinfo"
)

// VersionConfig configures `o version`. It exists for -h: every other command
// answers it, and a command which took no config at all answered it by printing
// the version, which is not what was asked.
type VersionConfig struct {
	*MainConfig
	Version *cli.Command
}

// VersionCommand prints which build of o this is.
//
// It writes to cc.Out rather than through MainConfig's output file: -o names
// where a command's object output goes, and a version is not one.
func VersionCommand(mainCfg *MainConfig) *cli.Command {
	cfg := &VersionConfig{MainConfig: mainCfg}
	opts, err := cli.StructOpts(cfg)
	if err != nil {
		panic(err)
	}
	return cli.NewCommandAt(&cfg.Version, "version").
		WithSynopsis("version").
		WithDescription("Print the version of o.").
		WithOpts(opts...).
		WithRun(func(cc *cli.Context, args []string) error {
			args, err := cfg.Version.Parse(cc, args)
			if err != nil {
				cfg.Version.Usage(cc, err)
				return cli.ExitCodeErr(2)
			}
			if helpAsked(cfg.Version, cc, cfg.Help) {
				return nil
			}
			if len(args) != 0 {
				return usageErr(cfg.Version, cc, "version takes no arguments")
			}
			fmt.Fprintln(cc.Out, buildinfo.Line("o"))
			return nil
		})
}
