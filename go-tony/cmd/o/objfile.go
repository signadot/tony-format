package main

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

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

// An input holds a STREAM of documents, not one document, and every command that
// reads one has to read it that way.
//
// This is what `o` writes: multiple files, multiple matches and multiple documents
// all come out `---` separated, which is what makes the output of one command the
// input of the next. Reading only the first document is therefore not a limitation,
// it is a tool that cannot consume its own output:
//
//	o get .a f1.tony f2.tony | o get .a
//	  -> imbalanced document: trailing material TDocSep
//
// grep is the shape to copy. Its unit is a line and it reads a stream of them from a
// file or from stdin without caring which, and without the caller saying how many to
// expect. Here the unit is a document.
//
// readDocs is where that lives, so a command reads a stream by asking for one rather
// than by remembering to split.

// docSep separates documents, on the way in and on the way out. A separator at the
// very start or end of the input is not a document boundary but an empty document
// beside it, which is why the split tolerates empties rather than counting them.
const docSep = "\n---\n"

// splitDocs cuts input into the documents it holds. It does not parse them.
func splitDocs(in []byte) [][]byte {
	return bytes.Split(in, []byte(docSep))
}

// readDocs reads every document in path, where "-" is standard input. Empty
// documents are dropped rather than answered for: a trailing separator, or a blank
// stretch between two, says nothing about what the caller asked.
func readDocs(cc *cli.Context, path string, opts ...parse.ParseOption) ([]*ir.Node, error) {
	var r io.Reader
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("error opening %s: %w", path, err)
		}
		defer f.Close()
		r = f
	} else {
		r = cc.In
	}
	in, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %w", path, err)
	}
	var docs []*ir.Node
	for i, raw := range splitDocs(in) {
		doc, err := parse.Parse(raw, opts...)
		if err != nil {
			return nil, fmt.Errorf("error decoding document %d of %s: %w", i, path, err)
		}
		if doc == nil {
			continue
		}
		docs = append(docs, doc)
	}
	return docs, nil
}
