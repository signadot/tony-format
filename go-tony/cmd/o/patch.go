package main

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
	"github.com/signadot/tony-format/go-tony/mergeop"

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
	// A patch answers with data unless asked otherwise, so without -c the result
	// carries no comments -- not the patch's, and not the ones the document being
	// patched already had. Reading a document and writing it back is how a
	// comment-blind tool erases them.
	res, err := tony.Patch(target, patch, mergeop.Comments(cfg.Comments))
	if err != nil {
		return fmt.Errorf("error patching %s: %w", args[0], err)
	}
	if res == nil {
		// The patch deleted the whole document. tony.Patch says so by returning
		// nil, which is a result and not a fault: what remains is nothing, so
		// nothing is written. Encoding it used to segfault.
		return nil
	}
	if err := encode.Encode(res, cc.Out, cfg.encOpts(cc.Out)...); err != nil {
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
