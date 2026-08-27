package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/mergeop"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

// Exit codes follow grep, which is what a filter in a pipe is read against:
//
//	0  something matched and was written
//	1  nothing matched -- an answer, not a fault
//	2  a fault: bad usage, unreadable input, an unparseable pattern
//
// The distinction is the point. Answering "nothing matched" with the same code
// as "your pattern was nonsense" is how a mistake comes to look like an empty
// world, and a match against a list -- which cannot match, since the unit is the
// document -- looks exactly like a list with nothing in it.
func match(cfg *MatchConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Command.Parse(cc, args)
	if err != nil {
		cfg.Command.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Command, cc, cfg.Help) {
		return nil
	}
	if cfg.Tags {
		fmt.Fprintf(cc.Out, "available match tags:\n")
		for _, s := range mergeop.Symbols() {
			if !s.IsMatch() {
				continue
			}
			fmt.Fprintf(cc.Out, "\t- %s\n", s)
		}
		return nil
	}
	if len(args) == 0 {
		return usageErr(cfg.Command, cc, "match requires 1 argument, a match object")
	}
	match, err := getMatch(cfg, cc, args[0])
	if err != nil {
		return fault(cc, err)
	}
	inputs := inputsOrStdin(args[1:])
	written := 0
	for _, arg := range inputs {
		res, err := matchFile(nil, cfg, cc, match, arg)
		if err != nil {
			return fault(cc, fmt.Errorf("error matching %s: %w", arg, err))
		}
		for _, oy := range res {
			// The separator counts documents WRITTEN, not documents within one
			// file: two files each answering once are still two documents, and
			// the second used to be run onto the end of the first.
			if written > 0 {
				if _, err := cc.Out.Write([]byte("---\n")); err != nil {
					return fault(cc, err)
				}
			}
			if err := encode.Encode(oy, cc.Out, cfg.encOpts(cc.Out)...); err != nil {
				return fault(cc, fmt.Errorf("error encoding output: %w", err))
			}
			written++
		}
	}
	if written == 0 {
		return notFound()
	}
	return nil
}

func getMatch(cfg *MatchConfig, cc *cli.Context, arg string) (*ir.Node, error) {
	res, err := getish(cfg.String, cfg.File, cc, arg, cfg.parseOpts())
	if err != nil {
		return nil, err
	}
	return res, nil
}

func getish(s, f bool, cc *cli.Context, arg string, opts []parse.ParseOption) (*ir.Node, error) {
	if s == f && s {
		return nil, fmt.Errorf("%w: only one of -s, -f may be specified", cli.ErrUsage)
	}

	var matchReader io.Reader
	if s {
		matchReader = strings.NewReader(arg)
	} else if f {
		switch arg {
		case "-":
			// cc.In, not os.Stdin: what a caller redirects is the context's
			matchReader = cc.In
		default:
			f, err := os.Open(arg)
			if err != nil {
				return nil, fmt.Errorf("error opening %s: %w", arg, err)
			}
			defer f.Close()
			matchReader = f
		}
	} else {
		matchReader = strings.NewReader(arg)
	}
	d, err := io.ReadAll(matchReader)
	if err != nil {
		return nil, fmt.Errorf("error reading match: %w", err)
	}
	res, err := parse.Parse(d, opts...)
	if err != nil {
		return nil, fmt.Errorf("error decoding match: %w", err)
	}
	return res, nil
}

func matchFile(dst []*ir.Node, cfg *MatchConfig, cc *cli.Context, match *ir.Node, file string) ([]*ir.Node, error) {
	var fileReader io.Reader
	if file == "-" {
		fileReader = cc.In
	} else {
		targetFile, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("error opening %s: %w", file, err)
		}
		defer targetFile.Close()
		fileReader = targetFile
	}
	return matchReader(dst, cfg, cc, match, fileReader)
}

func matchReader(dst []*ir.Node, cfg *MatchConfig, cc *cli.Context, match *ir.Node, r io.Reader) ([]*ir.Node, error) {
	in, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading: %w", err)
	}
	docs := bytes.Split(in, []byte("\n---\n"))
	for i, doc := range docs {
		y, err := parse.Parse(doc, cfg.parseOpts()...)
		if err != nil {
			return nil, fmt.Errorf("error decoding document %d: %w", i, err)
		}
		if y == nil {
			// skip empty documents
			continue
		}
		m, err := tony.Match(y, match)
		if err != nil {
			return nil, fmt.Errorf("error matching document %d: %w", i, err)
		}
		if m {
			if cfg.Trim {
				y = tony.Trim(match, y)
			}
			dst = append(dst, y)
		}
	}
	return dst, nil
}
