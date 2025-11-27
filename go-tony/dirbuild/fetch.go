package dirbuild

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/format"
	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func (d *Dir) fetch() ([]*ir.Node, error) {
	res := []*ir.Node{}
	evalEnv := eval.EnvToMapAny(d.Env)
	for i := range d.Sources {
		src := &d.Sources[i]
		docs, err := src.Fetch(d.Root, evalEnv)
		if err != nil {
			err = fmt.Errorf("error fetching from source: %w", err)
			return nil, err
		}
		res = append(res, docs...)
	}
	return res, nil
}

// DirSource represents a data source for a dirbuild.
type DirSource struct {
	schema `tony:"schemagen=dirsource"`
	Format *format.Format `tony:"field=format"`
	Exec   *string        `tony:"field=exec"`
	Dir    *string        `tony:"field=dir"`
	URL    *string        `tony:"field=url"`
}

// formatFromExtension returns the format based on file extension.
// Returns nil if the extension is not recognized, indicating format should be auto-detected.
func formatFromExtension(path string) *format.Format {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		f := format.YAMLFormat
		return &f
	case ".tony":
		f := format.TonyFormat
		return &f
	case ".json":
		f := format.JSONFormat
		return &f
	default:
		return nil
	}
}

func (s *DirSource) Fetch(root string, env map[string]any) ([]*ir.Node, error) {
	var defaultForm *format.Format
	if s.Format != nil {
		defaultForm = s.Format
	}
	switch {
	case s.Dir != nil:
		path, err := eval.ExpandString(*s.Dir, env)
		if err != nil {
			return nil, fmt.Errorf("error expanding path %q: %w", *s.Dir, err)
		}
		walker := newSourceWalker(defaultForm)
		err = filepath.WalkDir(path, walker.walk)
		if err != nil {
			return nil, err
		}
		return walker.docs, nil
	case s.URL != nil:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		url, err := eval.ExpandString(*s.URL, env)
		if err != nil {
			return nil, fmt.Errorf("error expanding url %q: %w", *s.URL, err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("url %s gave %d/%s", *s.URL, resp.StatusCode, http.StatusText(resp.StatusCode))
		}
		// Try to detect format from URL extension
		form := defaultForm
		if form == nil {
			form = formatFromExtension(url)
		}
		// Fall back to TonyFormat if still not detected
		if form == nil {
			f := format.TonyFormat
			form = &f
		}
		opts := []parse.ParseOption{parse.ParseFormat(*form)}
		return fromReader(resp.Body, opts)
	case s.Exec != nil:
		cmdStr, err := eval.ExpandString(*s.Exec, env)
		if err != nil {
			return nil, fmt.Errorf("error expanding command %q: %w", *s.Exec, err)
		}
		cmdArgV := strings.Fields(cmdStr)
		if len(cmdArgV) == 0 {
			return nil, fmt.Errorf("invalid command %q (after env %q)", cmdStr, *s.Exec)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cmdArgV[0], cmdArgV[1:]...)
		out := bytes.NewBuffer(nil)
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		// Use default format or fall back to TonyFormat
		form := defaultForm
		if form == nil {
			f := format.TonyFormat
			form = &f
		}
		opts := []parse.ParseOption{parse.ParseFormat(*form)}
		return fromReader(out, opts)
	default:
		return nil, nil
	}
}

func fromReader(r io.Reader, opts []parse.ParseOption) ([]*ir.Node, error) {
	d, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	docs := bytes.Split(d, []byte{'\n', '-', '-', '-', '\n'})
	res := make([]*ir.Node, len(docs))
	for i, doc := range docs {
		y, err := parse.Parse(doc, opts...)
		if err != nil {
			return nil, err
		}
		res[i] = y
	}
	return res, nil
}

type sourceWalker struct {
	ignore      map[string]bool
	defaultForm *format.Format
	docs        []*ir.Node
}

func newSourceWalker(defaultForm *format.Format) *sourceWalker {
	return &sourceWalker{
		defaultForm: defaultForm,
		ignore:      map[string]bool{},
	}
}

func (w *sourceWalker) walk(path string, info fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	//fmt.Printf("walk %s ignore %v\n", path, w.ignore)
	if w.ignore[path] {
		if info.IsDir() {
			return fs.SkipDir
		}
		return nil
	}
	for ignore := range w.ignore {
		m, _ := filepath.Match(ignore, path)
		if m {
			if info.IsDir() {
				return fs.SkipDir
			}
		}
	}
	if info.IsDir() {
		ignorePath := filepath.Join(path, ".buildignore.tony")
		_, err := os.Stat(ignorePath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := w.readIgnore(path, ignorePath); err != nil {
			return err
		}
		w.ignore[ignorePath] = true
		return nil
	}
	if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".tony") && !strings.HasSuffix(path, ".json") {
		return nil
	}

	// Determine format: use explicit format if set, otherwise detect from extension
	form := w.defaultForm
	if form == nil {
		form = formatFromExtension(path)
	}
	// Fall back to TonyFormat if still not detected
	if form == nil {
		f := format.TonyFormat
		form = &f
	}
	opts := []parse.ParseOption{parse.ParseFormat(*form)}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fDocs, err := fromReader(f, opts)
	if err != nil {
		return fmt.Errorf("error fetching from %s: %w", path, err)
	}
	w.docs = append(w.docs, fDocs...)
	return nil
}

func (w *sourceWalker) readIgnore(path, ignorePath string) error {
	d, err := os.ReadFile(ignorePath)
	if err != nil {
		return err
	}
	ignores := []string{}
	//dec := yaml.NewDecoder(f)
	// if err := dec.Decode(&ignores); err != nil {
	// 	return fmt.Errorf("error decoding %s: %w", ignorePath, err)
	// }
	ir, err := parse.Parse(d)
	if err != nil {
		return err
	}
	if err := gomap.FromTonyIR(ir, &ignores); err != nil {
		return err
	}

	for _, ignore := range ignores {
		pat := filepath.Join(path, ignore)
		_, err := filepath.Match(pat, "")
		if err != nil {
			return fmt.Errorf("illegal ignore pattern %q in %s: %w", ignore, ignorePath, err)
		}
		w.ignore[pat] = true
	}
	return nil
}
