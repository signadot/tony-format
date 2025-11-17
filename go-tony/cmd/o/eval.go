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
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/goccy/go-yaml"
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
	tool.Env = cfg.Env
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

func envFunc(env map[string]any, a string) error {
	key, val, ok := strings.Cut(a, "=")
	if !ok {
		return fmt.Errorf("%w: argument %q expected key=val", cli.ErrUsage, a)
	}
	var v any
	err := yaml.Unmarshal([]byte(val), &v)
	if err != nil {
		return err
	}
	parts := strings.Split(key, ".")
	n := len(parts)
	tmpEnv := env
	for i, part := range parts {
		if i == n-1 {
			tmpEnv[part] = v
			break
		}
		next := tmpEnv[part]
		if next == nil {
			next = map[string]any{}
			tmpEnv[part] = next
		}
		nextEnv, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot access %s, list or scalar", strings.Join(parts[:i+1], "."))
		}
		tmpEnv = nextEnv
	}
	return nil
}
