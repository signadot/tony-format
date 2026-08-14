package main

import (
	"fmt"

	"github.com/scott-cotton/cli"
)

func get(cfg *GetConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Get.Parse(cc, args)
	if err != nil {
		cfg.Get.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if len(args) == 0 {
		return usageErr(cfg.Get, cc, "get requires one argument, an object path")
	}
	path := args[0]
	if path == "" {
		return usageErr(cfg.Get, cc, "invalid query \"\"")
	}
	if path[0] != '$' {
		path = "$" + path
	}
	pred, err := ifPredicate(cfg.If, cfg.IfFile, cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	trim, err := ifPredicate(cfg.Trim, "", cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	args, ok := inputsOrStdin(args[1:])
	if !ok {
		return usageErr(cfg.Get, cc, "get requires something to query: a file, or - for stdin")
	}
	found := 0
	for i, arg := range args {
		n, err := queryArg(cfg.MainConfig, cc.Out, arg, path, false, i > 0, pred, trim)
		if err != nil {
			return fault(cc, fmt.Errorf("error querying %s with %s: %w", arg, path, err))
		}
		found += n
	}
	if found == 0 {
		return notFound()
	}
	return nil
}
