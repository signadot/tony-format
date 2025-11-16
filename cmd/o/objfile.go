package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tony-format/tony/ir"
	"github.com/tony-format/tony/parse"

	"github.com/scott-cotton/cli"
)

func getObjFile(cc *cli.Context, path string, opts ...parse.ParseOption) (*ir.Node, error) {
	var (
		r io.Reader
	)
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	} else {
		r = cc.In
	}

	d, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading %q: %w", path, err)
	}
	return parse.Parse(d, opts...)
}
