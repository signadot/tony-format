package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"github.com/tony-format/tony/encode"
	"github.com/tony-format/tony/parse"

	"github.com/scott-cotton/cli"
)

func view(cfg *ViewConfig, cc *cli.Context, args []string) error {
	args, err := cfg.View.Parse(cc, args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if err := viewReader(cfg, cc.Out, cc.In); err != nil {
			return err
		}
		return nil
	}
	if err := viewFiles(cfg, cc.Out, args); err != nil {
		return err
	}
	return nil
}

func viewFiles(cfg *ViewConfig, w io.Writer, files []string) error {
	for i, file := range files {
		if err := viewFile(cfg, w, file); err != nil {
			return err
		}
		if i < len(files)-1 {
			w.Write([]byte("\n---\n"))
		}
	}
	return nil
}

func viewFile(cfg *ViewConfig, w io.Writer, file string) error {
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
	if err := viewReader(cfg, w, f); err != nil {
		return fmt.Errorf("error processing %s: %w", file, err)
	}
	return nil
}

func viewReader(cfg *ViewConfig, w io.Writer, r io.Reader) error {
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
		if err := encode.Encode(y, w, opts...); err != nil {
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
