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
	if err := cfg.oneFormat(cfg.Command, cc); err != nil {
		return err
	}
	if cfg.Tags {
		var pairs [][2]string
		for _, s := range mergeop.Symbols() {
			if !s.IsMatch() {
				continue
			}
			pairs = append(pairs, [2]string{s.String(), mergeop.Summary(s.String())})
		}
		return writeTagDoc(cc, cfg.encOpts(cc.Out), pairs)
	}
	if len(args) == 0 {
		return usageErr(cfg.Command, cc, "match requires 1 argument, a match object")
	}
	match, err := getMatch(cfg, cc, args[0])
	if err != nil {
		return fault(cc, err)
	}
	inputs := inputsOrStdin(args[1:])
	if cfg.Each {
		return matchEach(cfg, cc, match, inputs)
	}
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

// matchEach writes the elements that match as ONE document: a list, gathered from every
// document of every input. -each reads each document as a list of candidates, so what
// it writes has to be one too, or a second -each has nothing to read: it wrote each
// element as a document of its own, and `o m -each a | o m -each b` then asked b of
// each element's fields. As one list it is what `o m -each '!and [a, b]'` writes
// (xg534ta8h12ksegqmxn0). Nothing matched writes nothing and answers 1, as ever.
//
// The list carries no note of where each element came from. A comment is what -c reads
// and !comment asks about, so a note written by one stage would be read by the next as
// the element's own, and change what its pattern answers.
func matchEach(cfg *MatchConfig, cc *cli.Context, match *ir.Node, inputs []string) error {
	var kept []*ir.Node
	for _, arg := range inputs {
		res, err := matchFile(nil, cfg, cc, match, arg)
		if err != nil {
			return fault(cc, fmt.Errorf("error matching %s: %w", arg, err))
		}
		kept = append(kept, res...)
	}
	if len(kept) == 0 {
		return notFound()
	}
	if err := encode.Encode(ir.FromSlice(kept), cc.Out, cfg.encOpts(cc.Out)...); err != nil {
		return fault(cc, fmt.Errorf("error encoding output: %w", err))
	}
	return nil
}

func getMatch(cfg *MatchConfig, cc *cli.Context, arg string) (*ir.Node, error) {
	res, err := getish(cfg.File, cc, arg, cfg.parseOpts())
	if err != nil {
		return nil, err
	}
	return res, nil
}

// getish reads the argument a command takes as a document: the argument ITSELF by
// default, or the contents of the file it names under -f.
//
// There is no -s for "take it as a string", because that is what taking it means. One
// existed, and did nothing: its branch and the default were the same statement, so it
// only ever documented the default under a name that implied there was something else.
// The argument is never guessed at -- a document that happens to look like a path is a
// document -- so -f is the whole of the choice.
func getish(f bool, cc *cli.Context, arg string, opts []parse.ParseOption) (*ir.Node, error) {
	var matchReader io.Reader
	if f {
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
		// -each asks about a list's elements rather than the list, and a document
		// that is not a list holds no elements to ask about, so it matches nothing.
		// The elements that match are written together, as one list (matchEach).
		candidates := []*ir.Node{y}
		if cfg.Each {
			if ir.Uncomment(y).Type != ir.ArrayType {
				continue
			}
			candidates = ir.Uncomment(y).Values
		}
		for j, c := range candidates {
			m, err := tony.Match(c, match)
			if err != nil {
				if cfg.Each {
					return nil, fmt.Errorf("error matching document %d element %d: %w", i, j, err)
				}
				return nil, fmt.Errorf("error matching document %d: %w", i, err)
			}
			if m {
				if cfg.Trim {
					c = tony.Trim(match, c)
				}
				dst = append(dst, c)
			}
		}
	}
	return dst, nil
}
