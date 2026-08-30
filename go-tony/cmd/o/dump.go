package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"io"
	"os"

	"github.com/scott-cotton/cli"
)

func dump(cfg *DumpConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Dump.Parse(cc, args)
	if err != nil {
		cfg.Dump.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Dump, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.Dump, cc); err != nil {
		return err
	}
	// A fault exits 2, as it does for get, list and match: 1 is reserved for
	// "nothing", which is an answer, and a caller that cannot tell an unreadable
	// file from an empty one reads a mistake as a result.
	if len(args) == 0 {
		if err := dumpReader(cfg, cc.Out, cc.In); err != nil {
			return fault(cc, err)
		}
		return nil
	}
	if err := dumpFiles(cfg, cc.Out, cc.In, args); err != nil {
		return fault(cc, err)
	}
	return nil
}

func dumpFiles(cfg *DumpConfig, w io.Writer, in io.Reader, files []string) error {
	for i, file := range files {
		if err := dumpFile(cfg, w, in, file); err != nil {
			return err
		}
		if i < len(files)-1 {
			w.Write([]byte("\n---\n"))
		}
	}
	return nil
}

func dumpFile(cfg *DumpConfig, w io.Writer, in io.Reader, file string) error {
	// cc.In, not os.Stdin: the context is what a caller can redirect, and reading
	// the process's input for "-" left that one path unreachable in process --
	// untestable, and wrong for anything embedding the command tree.
	r := in
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return fmt.Errorf("could not open %q: %w", file, err)
		}
		defer f.Close()
		r = f
	}
	if err := dumpReader(cfg, w, r); err != nil {
		return fmt.Errorf("error processing %s: %w", file, err)
	}
	return nil
}

func dumpReader(cfg *DumpConfig, w io.Writer, r io.Reader) error {
	in, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("error reading: %w", err)
	}
	docs := bytes.Split(in, []byte("\n---\n"))
	n := len(docs)
	mCfg := cfg.MainConfig
	opts := mCfg.encOpts(w)
	if cfg.Comments {
		opts = append(opts, encode.EncodeComments(cfg.Comments))
	}
	for i, doc := range docs {
		y, err := parse.Parse(doc, cfg.parseOpts()...)
		if err != nil {
			return fmt.Errorf("error decoding document %d: %w", i, err)
		}
		j, err := json.Marshal(y)
		if err != nil {
			return fmt.Errorf("internal error: %w", err)
		}
		yy, err := parse.Parse(j)
		if err != nil {
			return fmt.Errorf("error parsing IR: %w", err)
		}
		if err := encode.Encode(yy, w, opts...); err != nil {
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
