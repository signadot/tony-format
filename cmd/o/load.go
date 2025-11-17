package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/format"
	y "github.com/signadot/tony-format/tony/ir"
	"github.com/signadot/tony-format/tony/parse"

	"github.com/scott-cotton/cli"
)

func load(cfg *LoadConfig, cc *cli.Context, args []string) error {
	args, err := cfg.Load.Parse(cc, args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if err := loadReader(cfg, cc.Out, cc.In); err != nil {
			return err
		}
		return nil
	}
	if err := loadFiles(cfg, cc.Out, args); err != nil {
		return err
	}
	return nil
}

func loadFiles(cfg *LoadConfig, w io.Writer, files []string) error {
	for i, file := range files {
		if err := loadFile(cfg, w, file); err != nil {
			return err
		}
		if i < len(files)-1 {
			w.Write([]byte("\n---\n"))
		}
	}
	return nil
}

func loadFile(cfg *LoadConfig, w io.Writer, file string) error {
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
	if err := loadReader(cfg, w, f); err != nil {
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
