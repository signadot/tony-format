package main

import (
	"bytes"
	"fmt"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/parse"
	"io"
	"os"
	"path/filepath"

	"github.com/scott-cotton/cli"
)

func view(cfg *ViewConfig, cc *cli.Context, args []string) error {
	args, err := cfg.View.Parse(cc, args)
	if err != nil {
		cfg.View.Usage(cc, err)
		return cli.ExitCodeErr(2)
	}
	if helpAsked(cfg.View, cc, cfg.Help) {
		return nil
	}
	if err := cfg.oneFormat(cfg.View, cc); err != nil {
		return err
	}
	// A fault exits 2, as it does for get, list and match: 1 is reserved for
	// "nothing", which is an answer, and a caller that cannot tell an unreadable
	// file from an empty one reads a mistake as a result.
	if cfg.Write {
		if msg := whyNotWritable(cfg, args); msg != "" {
			return usageErr(cfg.View, cc, msg)
		}
		// Writing a file back keeps its comments whether or not -c was given.
		// Dropping them is a display choice for a command which PRINTS; for one
		// which overwrites the source it is data loss, and a file holding nothing
		// but a comment came back zero bytes.
		keep := *cfg
		keep.Comments = true
		if err := writeFiles(&keep, args); err != nil {
			return fault(cc, err)
		}
		return nil
	}
	if len(args) == 0 {
		if _, err := viewReader(cfg, cc.Out, cc.In); err != nil {
			return fault(cc, err)
		}
		return nil
	}
	if err := viewFiles(cfg, cc.Out, cc.In, args); err != nil {
		return fault(cc, err)
	}
	return nil
}

// whyNotWritable answers why -w cannot be honoured, or "" when it can: the two
// ways it has nothing to write to, and the one way it would write something nobody
// wants into a file.
func whyNotWritable(cfg *ViewConfig, args []string) string {
	if len(args) == 0 {
		return "-w writes each file back and no file was named: name one, or drop -w " +
			"to write the result to standard output"
	}
	for _, file := range args {
		if file == "-" {
			return `-w writes each file back and "-" is standard input: name the files, or drop -w`
		}
	}
	if cfg.Color {
		return "-w with -color would write the colouring into the file: drop one of them"
	}
	return ""
}

// writeFiles rewrites each file with its normal form.
//
// Normalising a document is reading it and writing it out. What a reader accepts is
// wider than what a writer produces -- whitespace running to the end of a line, a
// blank line, quotes a value does not need -- and none of it survives the round trip
// (docs/tony.md, "Normalization").
func writeFiles(cfg *ViewConfig, files []string) error {
	for _, file := range files {
		if err := writeFile(cfg, file); err != nil {
			return err
		}
	}
	return nil
}

// writeFile writes one file's normal form back over it.
//
// A file already in normal form is left ALONE rather than rewritten with identical
// bytes, so formatting a tree does not touch the modification time of every file in
// it -- which is what a build, a watch and a `git status` all read.
//
// The replacement is written beside the file and renamed over it, so an interrupted
// run leaves the original whole. Writing in place would leave a truncated file, and
// the file being truncated is the only copy of what it held.
func writeFile(cfg *ViewConfig, file string) error {
	// Through a symlink, format what it NAMES. Renaming over the link would
	// replace it with a regular file and leave the target unformatted, which
	// silently detaches a symlinked config from the thing it points at.
	if resolved, err := filepath.EvalSymlinks(file); err == nil {
		file = resolved
	}
	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("could not stat %q: %w", file, err)
	}
	in, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("could not read %q: %w", file, err)
	}
	var out bytes.Buffer
	if _, err := viewReader(cfg, &out, bytes.NewReader(in)); err != nil {
		return fmt.Errorf("error processing %s: %w", file, err)
	}
	if bytes.Equal(in, out.Bytes()) {
		return nil
	}
	// Normalising answered NOTHING from a file which held something. That is not a
	// formatting: a file holding only a comment has no document to write, and the
	// comment is not the formatter's to delete. Leave it as it is rather than
	// replace it with nothing -- whatever the reason the answer came back empty.
	if out.Len() == 0 && len(in) > 0 {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".o")
	if err != nil {
		return fmt.Errorf("could not write beside %q: %w", file, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("could not write %q: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not close %q: %w", tmp.Name(), err)
	}
	// the replacement takes the file's mode, not the temporary file's 0600
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return fmt.Errorf("could not set the mode of %q: %w", file, err)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return fmt.Errorf("could not replace %q: %w", file, err)
	}
	return nil
}

func viewFiles(cfg *ViewConfig, w io.Writer, in io.Reader, files []string) error {
	for i, file := range files {
		// The separator between FILES asks what the separator between documents
		// asks: has the line already been ended? Writing "\n---\n" after output
		// which ends in a newline puts a blank line before it, and separators
		// within one file stopped doing that.
		//
		// Answered rather than buffered: encOpts sniffs whether the writer is a
		// terminal to decide colour, so encoding into a buffer would turn colour
		// off for every multi-file view.
		endedLine, err := viewFile(cfg, w, in, file)
		if err != nil {
			return err
		}
		if i < len(files)-1 {
			sep := "\n---\n"
			if endedLine {
				sep = "---\n"
			}
			if _, err := w.Write([]byte(sep)); err != nil {
				return err
			}
		}
	}
	return nil
}

// viewFile answers whether what it wrote ended with a newline, which the caller
// needs to place a separator after it.
func viewFile(cfg *ViewConfig, w io.Writer, in io.Reader, file string) (bool, error) {
	// cc.In, not os.Stdin: the context is what a caller can redirect, and reading
	// the process's input for "-" left that one path unreachable in process --
	// untestable, and wrong for anything embedding the command tree.
	r := in
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return false, fmt.Errorf("could not open %q: %w", file, err)
		}
		defer f.Close()
		r = f
	}
	endedLine, err := viewReader(cfg, w, r)
	if err != nil {
		return false, fmt.Errorf("error processing %s: %w", file, err)
	}
	return endedLine, nil
}

// viewReader answers whether what it wrote ended with a newline.
func viewReader(cfg *ViewConfig, w io.Writer, r io.Reader) (bool, error) {
	in, err := io.ReadAll(r)
	if err != nil {
		return false, fmt.Errorf("error reading: %w", err)
	}
	docs := bytes.Split(in, []byte("\n---\n"))
	n := len(docs)
	mCfg := cfg.MainConfig
	opts := mCfg.encOpts(w)
	if cfg.Comments {
		opts = append(opts, encode.EncodeComments(cfg.Comments))
	}
	endedLine := false
	for i, doc := range docs {
		y, err := parse.Parse(doc, cfg.parseOpts()...)
		if err != nil {
			return false, fmt.Errorf("error decoding document %d: %w", i, err)
		}
		if y == nil {
			continue
		}
		// Encoded aside so the separator can tell whether the document has already
		// ended its line. Writing "\n---\n" after one that has put a blank line
		// before every separator, which is a line the author did not write and
		// which the writer is documented to drop (docs/tony.md, "Normalization").
		var one bytes.Buffer
		if err := encode.Encode(y, &one, opts...); err != nil {
			return false, fmt.Errorf("error encoding result %d: %w", i, err)
		}
		if _, err := w.Write(one.Bytes()); err != nil {
			return false, fmt.Errorf("error writing document %d: %w", i, err)
		}
		if b := one.Bytes(); len(b) > 0 {
			endedLine = b[len(b)-1] == '\n'
		}
		if i < n-1 {
			sep := "\n---\n"
			if endedLine {
				sep = "---\n"
			}
			if _, err := w.Write([]byte(sep)); err != nil {
				return false, fmt.Errorf("error writing document %d: %w", i, err)
			}
			endedLine = true
		}
	}
	return endedLine, nil
}
