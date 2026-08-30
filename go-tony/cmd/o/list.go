package main

import (
	"fmt"
	"io"
	"strings"

	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/scott-cotton/cli"
)

func list(cfg *ListConfig, cc *cli.Context, args []string) error {
	args, err := cfg.List.Parse(cc, args)
	if err != nil {
		cfg.List.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.List, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.List, cc); err != nil {
		return err
	}
	if len(args) == 0 {
		return usageErr(cfg.List, cc, "list requires one argument, an object path")
	}
	path, err := queryPath(args[0])
	if err != nil {
		return usageErr(cfg.List, cc, err.Error())
	}
	pred, err := ifPredicate(cfg.If, cfg.IfFile, cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	trim, err := ifPredicate(cfg.Trim, "", cfg.parseOpts())
	if err != nil {
		return fault(cc, err)
	}
	// One question over every document of every input, answered by one list.
	// Writing a list per input concatenated two arrays, which is not a document:
	// `o list .a empty.tony f2.tony` wrote "[]\n- 9" and o could not read it back.
	var found []*ir.Node
	for _, arg := range inputsOrStdin(args[1:]) {
		docs, err := readDocs(cc, arg, cfg.parseOpts()...)
		if err != nil {
			return fault(cc, err)
		}
		for _, doc := range docs {
			// -paths answers where each node IS, and trimming answers how much of
			// what it is to write. So a trim is not applied here: it would clone the
			// node away from the document, and a node with no document above it has
			// no path to report.
			nodeTrim := trim
			if cfg.Paths {
				nodeTrim = nil
			}
			res, err := listDoc(doc, path, cfg.Comments, pred, nodeTrim)
			if err != nil {
				return fault(cc, fmt.Errorf("error querying %s with %s: %w", arg, path, err))
			}
			if cfg.Paths {
				for _, n := range res {
					found = append(found, ir.FromString(n.KPath()))
				}
				continue
			}
			found = append(found, res...)
		}
	}
	// The empty list is still written: a query for a collection answers with a
	// collection, and [] is the honest one. The exit code is what says it was empty.
	if err := encode.Encode(ir.FromSlice(found), cc.Out, cfg.encOpts(cc.Out)...); err != nil {
		return fault(cc, fmt.Errorf("error encoding result: %w", err))
	}
	if len(found) == 0 {
		return notFound()
	}
	return nil
}

// listDoc answers what query names in one document, keeping only what pred matches
// when one was given.
func listDoc(doc *ir.Node, query string, comments bool, pred, trim *ir.Node) ([]*ir.Node, error) {
	// WithComments when comments were asked for: a path ANSWERS with the value it
	// names, dropping what was said above it, which is right for a reader asking
	// what is there and wrong for one asking to be shown the document.
	res, err := doc.ListKPathWith(nil, query, ir.WithComments(comments))
	if err != nil {
		return nil, fmt.Errorf("error executing list: %w", err)
	}
	res, err = keepMatching(res, pred)
	if err != nil {
		return nil, fmt.Errorf("error matching results of %s: %w", query, err)
	}
	for i, n := range res {
		res[i] = trimTo(n, trim)
	}
	return res, nil
}

// getDoc writes what query names in one document, and answers how many nodes it
// wrote -- which is what decides between "found" and "found nothing" for the
// caller's exit code.
//
// written is the count so far across every document of every input, because that is
// what a separator depends on: two inputs answering once each are two documents, and
// the second was run onto the end of the first when the count was per file.
func getDoc(eOpts []encode.EncodeOption, comments bool, w io.Writer, doc *ir.Node, arg, query string, written int, pred, trim *ir.Node) (int, error) {
	res, err := doc.GetKPathWith(query, ir.WithComments(comments))
	if err != nil {
		return 0, fmt.Errorf("error executing get: %w", err)
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
	if written > 0 {
		if err := writeSep(w); err != nil {
			return 0, err
		}
		argLines := strings.Split(strings.TrimSpace(arg), "\n")
		for i, argLine := range argLines {
			msg := "# from " + argLine + "\n"
			if i != 0 {
				msg = "#     " + argLine + "\n"
			}
			if _, err := w.Write([]byte(msg)); err != nil {
				return 0, err
			}
		}
	}
	if err := encode.Encode(trimTo(res, trim), w, eOpts...); err != nil {
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
