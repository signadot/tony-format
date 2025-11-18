package eval

import (
	"os"

	"github.com/signadot/tony-format/go-tony/ir"

	"github.com/expr-lang/expr"
)

func exprOpts(doc *ir.Node) []expr.Option {
	return []expr.Option{
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
	}
}
