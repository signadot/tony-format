package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/signadot/tony-format/tony"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/mergeop"
	"github.com/signadot/tony-format/tony/parse"

	"github.com/scott-cotton/cli"
)

func match(cfg *MatchConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Command.Parse(cc, args)
	if err != nil {
		return err
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
		return fmt.Errorf("%w: match requires 1 argument, a match object", cli.ErrUsage)
	}
	match, err := getMatch(cfg, cc, args[0])
	if err != nil {
		return err
	}
	for _, arg := range args[1:] {
		res, err := matchFile(nil, cfg, cc, match, arg)
		if err != nil {
			return fmt.Errorf("error matching %s: %w", arg, err)
		}
		for i, oy := range res {
			if i > 0 {
				_, err := cc.Out.Write([]byte("---\n"))
				if err != nil {
					return err
				}
			}
			if err := encode.Encode(oy, cc.Out, cfg.MainConfig.encOpts(cc.Out)...); err != nil {

				return fmt.Errorf("error encoding output: %w", err)
			}
		}
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
			matchReader = os.Stdin
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
		m, err := tony.Match(y, match)
		if err != nil {
			return nil, fmt.Errorf("error matching document %d: %w", i, err)
		}
		if m {
			if cfg.Trim {
				y = trim(match, y)
			}
			dst = append(dst, y)
		}
	}
	return dst, nil
}

func trim(match, doc *ir.Node) *ir.Node {
	switch match.Type {
	case ir.ObjectType:
		docMap := ir.ToMap(doc)
		matchMap := ir.ToMap(match)
		for i, field := range doc.Fields {
			matchVal := matchMap[field.String]
			if matchVal == nil {
				delete(docMap, field.String)
				continue
			}
			docVal := doc.Values[i]
			docMap[field.String] = trim(matchVal, docVal)
		}
		return ir.FromMap(docMap).WithTag(doc.Tag)
	case ir.ArrayType:
		n := len(match.Values)
		res := make([]*ir.Node, n)
		for i := range n {
			res[i] = trim(match.Values[i], doc.Values[i])
		}
		return ir.FromSlice(res).WithTag(doc.Tag)
	default:
		return doc.Clone()
	}
}
