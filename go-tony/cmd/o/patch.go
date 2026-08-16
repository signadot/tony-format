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
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Patch, cc, cfg.Help) {
		return nil
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
	if len(args) == 0 {
		return usageErr(cfg.Patch, cc, "patch requires 1 argument, a patch object, and reads the documents to patch from its file arguments or from standard input")
	}
	patch, err := getPatch(cfg, cc, args[0])
	if err != nil {
		return fault(cc, err)
	}
	if cfg.Reverse {
		rev, err := libdiff.Reverse(patch)
		if err != nil {
			return fault(cc, fmt.Errorf("error reversing patch: %w", err))
		}
		patch = rev
	}
	// The documents to patch are read the way get, list and match read theirs: from
	// the files named, or from standard input when none is, and every document of
	// each. It used to take exactly one file and no standard input at all, while its
	// own synopsis said `[files]`, so `o get ... | o patch P` -- the obvious thing to
	// write -- answered with a usage error.
	written := 0
	for _, arg := range inputsOrStdin(args[1:]) {
		docs, err := readDocs(cc, arg, cfg.parseOpts()...)
		if err != nil {
			return fault(cc, err)
		}
		for _, doc := range docs {
			// A patch answers with data unless asked otherwise, so without -c the
			// result carries no comments -- not the patch's, and not the ones the
			// document being patched already had. Reading a document and writing it
			// back is how a comment-blind tool erases them.
			res, err := tony.Patch(doc, patch, mergeop.Comments(cfg.Comments))
			if err != nil {
				return fault(cc, fmt.Errorf("error patching %s: %w", arg, err))
			}
			if res == nil {
				// The patch deleted the whole document. tony.Patch says so by
				// returning nil, which is a result and not a fault: what remains is
				// nothing, so nothing is written. Encoding it used to segfault.
				continue
			}
			if written > 0 {
				if err := writeSep(cc.Out); err != nil {
					return fault(cc, err)
				}
			}
			if err := encode.Encode(res, cc.Out, cfg.encOpts(cc.Out)...); err != nil {
				return fault(cc, fmt.Errorf("error encoding result: %w", err))
			}
			written++
		}
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
