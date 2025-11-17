package dirbuild

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/signadot/tony-format/tony"
	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/encode"
	"github.com/signadot/tony-format/tony/ir"
)

func (d *Dir) Run(w io.WriteCloser, opts ...encode.EncodeOption) ([]*ir.Node, error) {
	var (
		docs []*ir.Node
		err  error
		wd   string
	)
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
		e := os.Chdir(wd)
		if err != nil {
			err = errors.Join(err, e)
		} else {
			err = e
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
		defer w.Close()
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
		Env: d.Env,
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
