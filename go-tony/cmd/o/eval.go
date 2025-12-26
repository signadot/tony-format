package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

func tonyEval(cfg *EvalConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Eval.Parse(cc, args)
	if err != nil {
		return err
	}
	if cfg.Tags {
		fmt.Fprintf(cc.Out, "available eval tags:\n")
		for _, s := range eval.Symbols() {
			fmt.Fprintf(cc.Out, "\t- %s\n", s)
		}
		return nil
	}
	tool := tony.DefaultTool()
	tool.Env = eval.EnvToMapAny(cfg.Env)
	if len(args) == 0 {
		if err := evalReader(cfg, cc.Out, os.Stdin, tool, &cfg.Color); err != nil {
			return err
		}
		return nil
	}
	if err := evalFiles(cfg, cc.Out, args, tool, &cfg.Color); err != nil {
		return err
	}

	return nil
}

func evalFiles(cfg *EvalConfig, w io.Writer, files []string, tool *tony.Tool, color *bool) error {
	for i, file := range files {
		if err := evalFile(cfg, w, file, tool, color); err != nil {
			return err
		}
		if i < len(files)-1 {
			w.Write([]byte("\n---\n"))
		}
	}
	return nil
}

func evalFile(cfg *EvalConfig, w io.Writer, file string, tool *tony.Tool, color *bool) error {
	var (
		f   *os.File
		err error
	)
	if file != "-" {
		f, err = os.Open(file)
		if err != nil {
			return fmt.Errorf("could not open %q: %w", file, err)
		}
		defer f.Close()
	} else {
		f = os.Stdin
	}
	if err := evalReader(cfg, w, f, tool, color); err != nil {
		return fmt.Errorf("error processing %s: %w", file, err)
	}
	return nil
}

func evalReader(cfg *EvalConfig, w io.Writer, r io.Reader, tool *tony.Tool, color *bool) error {
	in, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("error reading: %w", err)
	}
	docs := bytes.Split(in, []byte("\n---\n"))
	n := len(docs)
	for i, doc := range docs {
		y, err := parse.Parse(doc, cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("error decoding document %d: %w", i, err)
		}
		if y == nil {
			continue
		}
		y, err = tool.Run(y)
		if err != nil {
			return fmt.Errorf("error evaluating document %d: %w", i, err)
		}
		if err := encode.Encode(y, w, cfg.MainConfig.encOpts(w)...); err != nil {
			return fmt.Errorf("error encoding result %d: %w", i, err)
		}
		if i < n-1 {
			_, err = w.Write([]byte("\n---\n"))
			if err != nil {
				return fmt.Errorf("error writing document %d: %w", i, err)
			}
		}
	}
	return nil
}

func envFunc(env map[string]*ir.Node, a string) error {
	key, val, ok := strings.Cut(a, "=")
	if !ok {
		return fmt.Errorf("%w: argument %q expected key=val", cli.ErrUsage, a)
	}
	var v any
	node, err := parse.Parse([]byte(val))
	if err != nil {
		return err
	}
	if err := gomap.FromTonyIR(node, &v); err != nil {
		return err
	}
	parts := strings.Split(key, ".")
	n := len(parts)

	// Build the nested value from the leaf up
	current := node
	for i := n - 1; i > 0; i-- {
		current = ir.FromMap(map[string]*ir.Node{parts[i]: current})
	}

	// Merge into existing structure at the top level
	topKey := parts[0]
	if n == 1 {
		env[topKey] = node
	} else {
		existing := env[topKey]
		if existing == nil {
			env[topKey] = current
		} else if existing.Type == ir.ObjectType {
			// Merge current into existing
			merged, err := tony.Patch(existing, current)
			if err != nil {
				return err
			}
			env[topKey] = merged
		} else {
			return fmt.Errorf("cannot access %s, list or scalar", topKey)
		}
	}
	return nil
}
