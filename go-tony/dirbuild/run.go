package dirbuild

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/go-tony"
	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
)

// Run executes the build pipeline: fetches documents from all sources, applies
// patches to matching documents, evaluates tool expressions, and writes the
// results. When Output.DestDir is set, each document is written to a file of its own
// there and w is not used; otherwise the documents are written to w, and a nil w is
// an error. Returns the processed documents and any error encountered.
//
// The writer belongs to the caller and is not closed here.
//
// Run changes the process working directory to the build root and changes it
// back, which is why the returns are named: the restore is deferred, and a
// failure to restore leaves the process somewhere the caller did not put it, so
// it has to be reported rather than dropped.
func (d *Dir) Run(w io.WriteCloser, opts ...encode.EncodeOption) (docs []*ir.Node, err error) {
	// Refused before anything runs: a nil w went on to the write path, which
	// panicked on it, and only after the sources had been fetched and the tool
	// nodes -- !exec among them -- evaluated (addsgv1yh12kszdxmdn0).
	if w == nil && d.Output.DestDir == "" {
		return nil, errors.New("no writer and no output.destDir: nowhere to write the documents")
	}
	var wd string
	wd, err = os.Getwd()
	if err != nil {
		err = fmt.Errorf("error getting working dir: %w", err)
		return nil, err
	}
	err = os.Chdir(d.Root)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := os.Chdir(wd); e != nil {
			err = errors.Join(err, fmt.Errorf("could not return to %s: %w", wd, e))
		}
	}()
	//fmt.Fprintf(os.Stderr, "running with env:\n%v\n", d.Env)

	docs, err = d.fetch()
	if err != nil {
		return nil, err
	}
	err = d.patch(docs)
	if err != nil {
		err = fmt.Errorf("error patching: %w", err)
		return nil, err
	}
	err = d.runTool(docs)
	if err != nil {
		err = fmt.Errorf("error evaluating tool nodes: %w", err)
		return nil, err
	}
	var bw *bufio.Writer
	if w != nil {
		bw = bufio.NewWriter(w)
	}
	err = d.writeFlush(bw, docs, opts...)
	if err != nil {
		err = fmt.Errorf("error writing docs: %w", err)
		return nil, err
	}
	return docs, nil
}

func (d *Dir) runTool(dst []*ir.Node) error {
	tool := &tony.Tool{
		Env: eval.EnvToMapAny(d.Env),
	}
	defer clear(d.nameCache)
	for i, doc := range dst {
		if doc == nil {
			continue
		}
		outDoc, err := tool.Run(doc)
		if err != nil {
			return err
		}
		//fmt.Printf("run\n%s\nran\n%s\n", doc.MustString(), outDoc.MustString())
		dst[i] = outDoc
		if debug.Eval() {
			debug.Logf("# tool node in doc\n---\n%s\n# out\n---\n%s\n", encode.MustString(doc), encode.MustString(outDoc))
		}
	}
	return nil
}
