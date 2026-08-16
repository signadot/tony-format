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
	if helpAsked(cfg.Get, cc, cfg.Help) {
		return nil
	}
	if len(args) == 0 {
		return usageErr(cfg.Get, cc, "get requires one argument, an object path")
	}
	path, err := queryPath(args[0])
	if err != nil {
		return usageErr(cfg.Get, cc, err.Error())
	}
	pred, err := ifPredicate(cfg.If, cfg.IfFile, cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	trim, err := ifPredicate(cfg.Trim, "", cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	found := 0
	for _, arg := range inputsOrStdin(args[1:]) {
		docs, err := readDocs(cc, arg, cfg.parseOpts()...)
		if err != nil {
			return fault(cc, err)
		}
		for _, doc := range docs {
			n, err := getDoc(cfg.encOpts(cc.Out), cfg.Comments, cc.Out, doc, arg, path, found, pred, trim)
			if err != nil {
				return fault(cc, fmt.Errorf("error querying %s with %s: %w", arg, path, err))
			}
			found += n
		}
	}
	if found == 0 {
		return notFound()
	}
	return nil
}
