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
// results. If w is non-nil, output is written to it; otherwise output goes to
// DestDir if set. Returns the processed documents and any error encountered.
//
// The writer belongs to the caller and is not closed here. Run used to close it,
// so the usage this package's own documentation shows -- dir.Run(os.Stdout) --
// closed the caller's standard output.
//
// Run changes the process working directory to the build root and changes it
// back, which is why the returns are named: the restore is deferred, and a
// failure to restore leaves the process somewhere the caller did not put it, so
// it has to be reported rather than dropped.
func (d *Dir) Run(w io.WriteCloser, opts ...encode.EncodeOption) (docs []*ir.Node, err error) {
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
