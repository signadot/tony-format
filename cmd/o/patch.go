package main

import (
	"fmt"

	"github.com/signadot/tony-format/tony"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/libdiff"
	"github.com/signadot/tony-format/tony/mergeop"

	"github.com/scott-cotton/cli"
)

func patch(cfg *PatchConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Patch.Parse(cc, args)
	if err != nil {
		cfg.Patch.Usage(cc, err)
		return cli.ExitCodeErr(1)
	}
	if cfg.Tags {
		fmt.Fprintf(cc.Out, "available patch tags:\n")
		for _, s := range mergeop.Symbols() {
			if !s.IsPatch() {
				continue
			}
			fmt.Fprintf(cc.Out, "\t- %s\n", s)

		}
		return nil

	}
	if len(args) != 2 {
		return fmt.Errorf("%w: patch requires 2 arguments, a patch object, and a file to which to apply it", cli.ErrUsage)
	}
	patch, err := getPatch(cfg, cc, args[0])
	if err != nil {
		return err
	}
	if cfg.Reverse {
		rev, err := libdiff.Reverse(patch)
		if err != nil {
			return fmt.Errorf("error reversing patch: %w", err)
		}
		patch = rev
	}
	target, err := getObjFile(cc, args[1], cfg.parseOpts()...)
	if err != nil {
		return fmt.Errorf("error decoding %s: %w", args[0], err)
	}
	res, err := tony.Patch(target, patch)
	if err != nil {
		return fmt.Errorf("error patching %s: %w", args[0], err)
	}
	mCfg := cfg.MainConfig
	if err := encode.Encode(res, cc.Out, mCfg.encOpts(cc.Out)...); err != nil {
		return fmt.Errorf("error encoding result: %w", err)
	}
	return nil
}

func getPatch(cfg *PatchConfig, cc *cli.Context, arg string) (*ir.Node, error) {
	res, err := getish(cfg.String, cfg.File, cc, arg, cfg.parseOpts())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", cli.ErrUsage, err)
	}
	return res, nil
}
