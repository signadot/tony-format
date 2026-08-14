package main

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/scott-cotton/cli"
	tony "github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// The commands that SEARCH -- match, get, list -- answer a pipe the way grep
// does, and mean the same thing by it:
//
//	0  something was found and written
//	1  nothing was found -- an answer, not a fault, and written on no stream
//	2  a fault: bad usage, unreadable input, an unparseable match document
//
// Keeping 1 apart from 2 is the point. A search that answers "nothing" with the
// same code as "your pattern was nonsense" turns a mistake into an empty world,
// and the empty world is the more believable of the two.
//
// 1 is reported as a cli.ExitCodeErr, which the framework prints nothing for --
// the silence a pipe wants. A fault prints first, because a fault is worth a
// sentence.

// fault reports a fault: the message, then exit 2.
func fault(cc *cli.Context, err error) error {
	fmt.Fprintln(cc.Err, err)
	return cli.ExitCodeErr(2)
}

// usageErr reports a misuse: the command's usage, then exit 2. A misuse is a
// fault and not an empty answer, which is the distinction that makes `x | o m
// PAT` with the - forgotten say something.
func usageErr(cmd *cli.Command, cc *cli.Context, msg string) error {
	cmd.Usage(cc, fmt.Errorf("%w: %s", cli.ErrUsage, msg))
	return cli.ExitCodeErr(2)
}

// notFound is the answer when a search found nothing.
func notFound() error {
	return cli.ExitCodeErr(1)
}

// ifPredicate parses the -if / -if-file option into a match document, or answers
// nil when neither was given, which means "keep everything the path named".
//
// A path says WHERE to look and a match says WHICH of what is there to keep;
// the two are separate questions and the flags keep them separate, so
// `o list -if '{state: open}' '$.items[*]'` reads as the sentence it is.
func ifPredicate(text, file string, opts []parse.ParseOption) (*ir.Node, error) {
	if text != "" && file != "" {
		return nil, fmt.Errorf("%w: only one of -if, -if-file may be given", cli.ErrUsage)
	}
	src := []byte(text)
	if file != "" {
		d, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("error reading match document %s: %w", file, err)
		}
		src = d
	}
	if len(src) == 0 {
		return nil, nil
	}
	pred, err := parse.Parse(src, opts...)
	if err != nil {
		return nil, fmt.Errorf("error decoding match document: %w", err)
	}
	return pred, nil
}

// inputsOrStdin answers what to read when the command was given no file.
//
// A pipe means stdin: `x | o m PAT` is the shape everyone writes, and requiring
// the `-` is a toll on the common case. A TERMINAL means the file was forgotten,
// and reading it would hang waiting for a person to type a document -- so that
// stays a usage error, which is the case the trailing `-` was protecting.
func inputsOrStdin(args []string) ([]string, bool) {
	if len(args) > 0 {
		return args, true
	}
	if isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		return nil, false
	}
	return []string{"-"}, true
}

// trimTo projects node to the shape pat names, which is what -trim means on
// match: the fields asked for and nothing else. A nil pat leaves the node whole.
func trimTo(node, pat *ir.Node) *ir.Node {
	if pat == nil || node == nil {
		return node
	}
	return tony.Trim(pat, node)
}
