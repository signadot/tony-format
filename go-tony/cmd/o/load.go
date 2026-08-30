package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/format"
	y "github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/scott-cotton/cli"
)

func load(cfg *LoadConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Load.Parse(cc, args)
	if err != nil {
		cfg.Load.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.Load, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.Load, cc); err != nil {
		return err
	}
	// A fault exits 2, as it does for get, list and match: 1 is reserved for
	// "nothing", which is an answer, and a caller that cannot tell an unreadable
	// file from an empty one reads a mistake as a result.
	if len(args) == 0 {
		if err := loadReader(cfg, cc.Out, cc.In); err != nil {
			return fault(cc, err)
		}
		return nil
	}
	if err := loadFiles(cfg, cc.Out, cc.In, args); err != nil {
		return fault(cc, err)
	}
	return nil
}

func loadFiles(cfg *LoadConfig, w io.Writer, in io.Reader, files []string) error {
	for i, file := range files {
		if err := loadFile(cfg, w, in, file); err != nil {
			return err
		}
		if i < len(files)-1 {
			w.Write([]byte("\n---\n"))
		}
	}
	return nil
}

func loadFile(cfg *LoadConfig, w io.Writer, in io.Reader, file string) error {
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
	if err := loadReader(cfg, w, r); err != nil {
		return fmt.Errorf("error processing %s: %w", file, err)
	}
	return nil
}

func loadReader(cfg *LoadConfig, w io.Writer, r io.Reader) error {
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
	encOpts := cfg.encOpts(w)
	if cfg.Comments {
		encOpts = append(encOpts, encode.EncodeComments(cfg.Comments))
	}
	pOpts := append(cfg.parseOpts(), parse.ParseComments(false))
	for i, doc := range docs {
		ir, err := parse.Parse(doc, pOpts...)
		if err != nil {
			return fmt.Errorf("error decoding document %d: %w", i, err)
		}
		bw := bytes.NewBuffer(nil)
		if err := encode.Encode(ir, bw, encode.EncodeFormat(format.JSONFormat)); err != nil {
			return err
		}
		org := &y.Node{}
		if err := json.Unmarshal(bw.Bytes(), org); err != nil {
			return err
		}
		if err := encode.Encode(org, w, encOpts...); err != nil {
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
