package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/scott-cotton/cli"
)

func oMain(cfg *MainConfig, cc *cli.Context, args []string) error {
	defer func() {
		if cfg.CloseOut != nil {
			cfg.CloseOut()
		}
	}()
	args, err := cfg.Main.Parse(cc, args)
	if err != nil {
		return err
	}
	if count(cfg.T, cfg.J, cfg.Y) > 1 {
		return fmt.Errorf("%w: must specify at most one of -j[son] -t[ony] -y[aml]", cli.ErrUsage)
	}
	// Asking what a tool does is not a misuse of it. `o -h`, `o --help` and `o` with
	// nothing at all all print the same thing, on stdout, and exit 0 -- they used to
	// print it on stderr followed by `usage error: unknown option: "help"`, which
	// tells a first-time reader they have already got it wrong.
	if cfg.Help || len(args) == 0 {
		if cfg.Help && len(args) > 0 {
			if sub := cfg.Main.FindSub(cc, args[0]); sub != nil {
				sub.Usage(cc, nil)
				return nil
			}
		}
		cfg.Main.Usage(cc, nil)
		if !cfg.Help {
			fmt.Fprintf(cc.Out, "\nplease choose a command, or run `o help <command>`.\n")
		}
		return nil
	}
	sub := cfg.Main.FindSub(cc, args[0])
	if sub == nil {
		return noSuchCommand(cfg.Main, cc, args[0])
	}
	err = sub.Run(cc, args[1:])
	if errors.Is(err, cli.ErrUsage) {
		sub.Usage(cc, err)
		os.Exit(sub.Exit(cc, err))
	}
	return err
}

func count(vs ...bool) int {
	ttl := 0
	for _, v := range vs {
		if v {
			ttl++
		}
	}
	return ttl
}

func (cfg *MainConfig) outOpt(cc *cli.Context, a string) (any, error) {
	cfg.Out = a
	if a == "-" {
		return nil, nil
	}
	f, err := os.OpenFile(cfg.Out, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	cc.Out = f
	cfg.CloseOut = f.Close
	return nil, nil
}
