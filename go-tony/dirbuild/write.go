package dirbuild

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
)

func (d *Dir) writeFlush(bw *bufio.Writer, dst []*ir.Node, opts ...encode.EncodeOption) error {
	if err := d.write(bw, dst, opts...); err != nil {
		return err
	}
	if bw != nil {
		return bw.Flush()
	}
	return nil
}

func (d *Dir) write(bw *bufio.Writer, dst []*ir.Node, opts ...encode.EncodeOption) error {
	if d.DestDir != "" {
		st, err := os.Stat(d.DestDir)
		if err != nil {
			if os.IsNotExist(err) {
				err = os.MkdirAll(d.DestDir, 0755)
				if err != nil {
					return err
				}
			} else {
				return err
			}
		} else if !st.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", filepath.Join(d.Root, d.DestDir))
		}
	}
	j := 0
	for _, doc := range dst {
		if doc == nil {
			continue
		}
		dst[j] = doc
		j++
	}
	dst = dst[:j]
	n := len(dst)
	for i, doc := range dst {
		if err := d.writeOut(bw, doc, i, n, opts...); err != nil {
			return err
		}
	}
	return nil
}

func (d *Dir) writeOut(w io.Writer, y *ir.Node, j, n int, opts ...encode.EncodeOption) error {
	wc, err := d.writeCloser(w, y)
	if err != nil {
		return err
	}
	defer wc.Close()
	if err := encode.Encode(y, wc, opts...); err != nil {
		return err
	}
	if d.DestDir == "" && j != n-1 {
		// doc separator
		_, err = wc.Write([]byte{'-', '-', '-', '\n'})
		return err
	}
	return nil
}

type nopWriterCloser struct {
	io.Writer
}

func (_ nopWriterCloser) Close() error {
	return nil
}

func (d *Dir) writeCloser(w io.Writer, node *ir.Node) (io.WriteCloser, error) {
	if d.DestDir == "" {
		return nopWriterCloser{Writer: w}, nil
	}
	fn := fileName(node)
	n := d.nameCache[fn]
	d.nameCache[fn] = n + 1
	if n != 0 {
		fn += "-" + strconv.Itoa(n)
	}
	fn += d.Suffix
	fp := filepath.Join(d.DestDir, fn)
	f, err := os.OpenFile(fp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}
	return &wc{f: f, w: bufio.NewWriter(f)}, nil
}

type wc struct {
	f *os.File
	w *bufio.Writer
}

func (w *wc) Write(d []byte) (int, error) {
	return w.w.Write(d)
}

func (w *wc) Close() error {
	if err := w.w.Flush(); err != nil {
		return err
	}
	return w.f.Close()
}
