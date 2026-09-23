package eval

import (
	"errors"
	"os"

	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/expr-lang/expr"
)

// baseOpts are the functions an expression has wherever it is evaluated, needing
// nothing of the document. The rest (getpath, listpath, whereami) resolve through a
// node and are exprOpts', so an expansion given no node has these and not those.
func baseOpts() []expr.Option {
	return []expr.Option{
		// fail is how an expression says it has no answer: it raises the author's
		// message as the expression's error, so `cond ? value : fail("...")` refuses
		// rather than defaulting. It answers no value -- a caller reads the error --
		// and being the else branch of a ternary it is evaluated only when taken.
		expr.Function("fail", func(params ...any) (any, error) {
			msg, _ := params[0].(string)
			return nil, errors.New(msg)
		},
			new(func(string) any)),
	}
}

func exprOpts(doc *ir.Node) []expr.Option {
	return append(baseOpts(), []expr.Option{
		expr.Function("whereami", func(params ...any) (any, error) {
			return doc.Path(), nil
		},
			new(func() string)),
		expr.Function("getpath", func(params ...any) (any, error) {
			path := params[0].(string)
			res, err := doc.Root().GetPath(path)
			if err != nil {
				return nil, err
			}
			return res, nil
		},
			new(func(string) *ir.Node)),
		expr.Function("listpath", func(params ...any) (any, error) {
			path := params[0].(string)
			yRes, err := doc.Root().ListPath(nil, path)
			if err != nil {
				return nil, err
			}
			return yRes, nil
		},
			new(func(string) []*ir.Node)),
		expr.Function("getenv", func(params ...any) (any, error) {
			return os.Getenv(params[0].(string)), nil
		},
			new(func(string) string)),
	}...)
}
