package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

func list(cfg *ListConfig, cc *cli.Context, args []string) error {
	args, err := cfg.List.Parse(cc, args)
	if err != nil {
		cfg.List.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if len(args) == 0 {
		return usageErr(cfg.List, cc, "list requires one argument, an object path")
	}
	path := args[0]
	if path == "" {
		return usageErr(cfg.List, cc, "invalid query \"\"")
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
		return usageErr(cfg.List, cc, "list requires something to query: a file, or - for stdin")
	}
	found := 0
	for _, arg := range args {
		n, err := queryArg(cfg.MainConfig, cc.Out, arg, path, true, false, pred, trim)
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

// queryArg writes what query names in arg, keeping only what pred matches when
// one was given, and answers how many nodes it wrote -- which is what decides
// between "found" and "found nothing" for the caller's exit code.
func queryArg(cfg *MainConfig, w io.Writer, arg, query string, list, sep bool, pred, trim *ir.Node) (int, error) {
	var targetReader io.Reader
	if arg == "-" {
		targetReader = os.Stdin
	} else {
		targetFile, err := os.Open(arg)
		if err != nil {
			return 0, fmt.Errorf("error opening %s: %w", arg, err)
		}
		defer targetFile.Close()
		targetReader = targetFile
	}
	rd, err := io.ReadAll(targetReader)
	if err != nil {
		return 0, err
	}
	target, err := parse.Parse(rd, cfg.parseOpts()...)
	if err != nil {
		return 0, fmt.Errorf("error decoding %s: %w", arg, err)
	}
	if target == nil {
		// An empty document, which parse reports as a nil node. It names nothing,
		// which is an answer -- the caller's exit code says so -- and asking a
		// nil node for a path is a segfault.
		return 0, nil
	}
	if list {
		res, err := target.ListPath(nil, query)
		if err != nil {
			return 0, fmt.Errorf("error executing list on %s: %w", arg, err)
		}
		res, err = keepMatching(res, pred)
		if err != nil {
			return 0, fmt.Errorf("error matching results of %s: %w", query, err)
		}
		for i, n := range res {
			res[i] = trimTo(n, trim)
		}
		// The empty list is still written: a query for a collection answers with
		// a collection, and [] is the honest one. The exit code is what says it
		// was empty.
		arr := ir.FromSlice(res)
		if err := encode.Encode(arr, w, cfg.encOpts(w)...); err != nil {
			return 0, fmt.Errorf("error encoding result: %w", err)
		}
		return len(res), nil
	}
	res, err := target.GetPath(query)
	if err != nil {
		return 0, fmt.Errorf("error executing get on %s: %w", arg, err)
	}
	if res == nil {
		// don't encode anything and don't yell either
		return 0, nil
	}
	if pred != nil {
		ok, err := tony.Match(res, pred)
		if err != nil {
			return 0, fmt.Errorf("error matching result of %s: %w", query, err)
		}
		if !ok {
			return 0, nil
		}
	}
	if sep {
		if err := writeSep(w); err != nil {
			return 0, err
		}
		argLines := strings.Split(strings.TrimSpace(arg), "\n")
		for i, argLine := range argLines {
			msg := "# from " + argLine + "\n"
			if i != 0 {
				msg = "#     " + argLine + "\n"
			}
			_, err := w.Write([]byte(msg))
			if err != nil {
				return 0, err
			}
		}

	}
	if err := encode.Encode(trimTo(res, trim), w, cfg.encOpts(w)...); err != nil {
		return 0, fmt.Errorf("error encoding result: %w", err)
	}
	return 1, nil
}

// keepMatching keeps the nodes pred matches, in the order the path named them.
// A nil pred keeps everything: no -if was given, so the path is the whole
// question.
func keepMatching(nodes []*ir.Node, pred *ir.Node) ([]*ir.Node, error) {
	if pred == nil {
		return nodes, nil
	}
	kept := make([]*ir.Node, 0, len(nodes))
	for _, n := range nodes {
		ok, err := tony.Match(n, pred)
		if err != nil {
			return nil, err
		}
		if ok {
			kept = append(kept, n)
		}
	}
	return kept, nil
}
