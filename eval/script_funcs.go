package eval

import (
	"os"

	"github.com/tony-format/tony/ir"

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
			return ToJSONAny(res), nil
		},
			new(func(string) any)),
		expr.Function("listpath", func(params ...any) (any, error) {
			path := params[0].(string)
			yRes, err := doc.Root().ListPath(nil, path)
			if err != nil {
				return nil, err
			}
			res := make([]any, len(yRes))
			for i, item := range yRes {
				res[i] = ToJSONAny(item)
			}
			return res, nil
		},
			new(func(string) []any)),
		expr.Function("getenv", func(params ...any) (any, error) {
			return os.Getenv(params[0].(string)), nil
		},
			new(func(string) string)),
	}
}
