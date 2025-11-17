package eval

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/signadot/tony-format/tony/debug"
	"github.com/signadot/tony-format/tony/ir"
)

var fileSym = &fileSymbol{name: fileName}

func File() Symbol {
	return fileSym
}

const (
	fileName name = "file"
)

type fileSymbol struct {
	name
}

func (s fileSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("file only applies to strings, got %s", child.Type)
	}
	return &fileOp{op: op{name: s.name, child: child}}, nil
}

type fileOp struct {
	op
}

func (p fileOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("file on %s\n", doc.Path())
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	if doc.Type != ir.StringType {
		return nil, fmt.Errorf("file only applies to strings, got %s after expanding env", doc.Type)
	}
	u, err := url.Parse(doc.String)
	if err != nil {
		return nil, fmt.Errorf("%q is not a file or url: %w", doc.String, err)
	}
	switch u.Scheme {
	case "":
		f, err := os.Open(doc.String)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		d, err := io.ReadAll(f)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %w", doc.String, err)
		}
		return ir.FromString(string(d)), nil
	case "http", "https":
		resp, err := http.Get(doc.String)
		if err != nil {
			return nil, fmt.Errorf("error fetching %s: %w", u, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("error fetching %s: %s", u, http.StatusText(resp.StatusCode))
		}
		d, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading %s: %w", doc.String, err)
		}
		return ir.FromString(string(d)), nil
	default:
		return nil, fmt.Errorf("unsupported url scheme %q", u.Scheme)

	}
}
