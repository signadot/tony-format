package eval

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"

	"github.com/expr-lang/expr"
)

var scriptSym = &scriptSymbol{name: scriptName}

func Script() Symbol {
	return scriptSym
}

const (
	scriptName name = "script"
)

type scriptSymbol struct {
	name
}

func (s scriptSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("%s op expects no args, got %v", s, args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("script only applies to strings, got %s", child.Type)
	}
	as, err := parseScriptAs(args[0])
	if err != nil {
		return nil, err
	}

	return &scriptOp{as: as, op: op{name: s.name, child: child}}, nil
}

type scriptAs string

const (
	scriptAsValue   scriptAs = "value"
	scriptAsJSONAny scriptAs = "any"
	scriptAsString  scriptAs = "string"
)

func parseScriptAs(v string) (scriptAs, error) {
	as, ok := map[string]scriptAs{
		"value":  scriptAsValue,
		"any":    scriptAsJSONAny,
		"string": scriptAsString,
	}[v]
	if ok {
		return as, nil
	}
	return "", fmt.Errorf("invalid script as: %q", v)
}

type scriptOp struct {
	as scriptAs
	op
}

func (p scriptOp) Eval(doc *ir.Node, env Env, ef EvalFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("script on %s\n", doc.Path())
	}
	if err := ExpandEnv(doc, env); err != nil {
		return nil, err
	}
	prg, err := expr.Compile(doc.String, exprOpts(doc)...)
	if err != nil {
		return nil, err
	}
	res, err := expr.Run(prg, env)
	if err != nil {
		return nil, err
	}
	switch p.as {
	case scriptAsValue:
		v, ok := res.(string)
		if !ok {
			return nil, fmt.Errorf("script(yaml) but returned type %T", res)
		}
		return parse.Parse([]byte(v))
	case scriptAsString:
		switch v := res.(type) {
		case string:
			return ir.FromString(v), nil
		case *ir.Node:
			if v == nil {
				return ir.Null(), nil
			}
			// Convert node to string representation
			// If it's a string node, use its value; otherwise use Path()
			if v.Type == ir.StringType {
				return ir.FromString(v.String), nil
			}
			return ir.FromString(v.Path()), nil
		case nil:
			return ir.Null(), nil
		default:
			return nil, fmt.Errorf("script(string) but returned type %T", res)
		}
	case scriptAsJSONAny:
		return FromAny(res)
	}
	return doc, nil
}
