package main

import (
	"fmt"

	"github.com/scott-cotton/cli"
)

func get(cfg *GetConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Get.Parse(cc, args)
	if err != nil {
		cfg.Get.Usage(cc, err)
		return cli.ExitCodeErr(1)
	}
	if len(args) == 0 {
		return fmt.Errorf("%w: get requires one argument, an object path", cli.ErrUsage)
	}
	path := args[0]
	if path == "" {
		return fmt.Errorf("%w: invalid query \"\"", cli.ErrUsage)
	}
	if path[0] != '$' {
		path = "$" + path
	}
	args = args[1:]
	for i, arg := range args {
		if err := queryArg(cfg.MainConfig, cc.Out, arg, path, false, i > 0); err != nil {
			return fmt.Errorf("error querying %s with %s: %w", arg, path, err)
		}
	}
	return nil
}
