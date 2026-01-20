package tony

import (
	"fmt"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
)

type Tool struct {
	Env map[string]any
}

func DefaultTool() *Tool {
	return &Tool{
		Env: map[string]any{},
	}
}

func (t *Tool) Run(y *ir.Node) (*ir.Node, error) {
	return t.run(y.Clone(), nil)
}

func (t *Tool) run(node, parent *ir.Node) (*ir.Node, error) {
	tag, args, child, err := eval.SplitChild(node)
	if err != nil {
		return nil, err
	}
	if debug.Eval() {
		if tag != "" {
			debug.Logf("on %s split child: tag=%q, child.tag=%q\n", node.Path(), tag, child.Tag)
		} else {
			debug.Logf("on %s (no tag from orig %q)\n", node.Path(), node.Tag)
		}
	}
	if tag != "" {
		node = child.Clone()
	}
	res := &ir.Node{}
	*res = *node
	res.Parent = parent
	res.ParentIndex = node.ParentIndex
	res.ParentField = node.ParentField

	switch res.Type {
	case ir.ObjectType:
		fields := make([]*ir.Node, len(node.Fields))
		values := make([]*ir.Node, len(node.Values))
		for i, field := range node.Fields {
			value := node.Values[i]
			yy, err := t.run(value, res)
			if err != nil {
				return nil, err
			}
			yy.Parent = res
			yy.ParentField = field.String
			values[i] = yy
			resField := field.Clone()
			resField.Parent = res
			resField.ParentField = field.String
			fields[i] = resField
		}
		res.Fields = fields
		res.Values = values

	case ir.ArrayType:
		values := make([]*ir.Node, len(res.Values))
		for i, yy := range node.Values {
			resY, err := t.run(yy, res)
			if err != nil {
				return nil, fmt.Errorf("error processing list item %d (%s): %w", i, encode.MustString(yy), err)
			}
			values[i] = resY
			resY.Parent = res
			resY.ParentIndex = i
		}
		res.Values = values
	case ir.NumberType:
		res = res.Clone() // this has pointers
	case ir.BoolType, ir.StringType, ir.NullType:
	case ir.CommentType:
		if len(res.Values) > 0 {
			yy, err := t.run(res.Values[0], res)
			if err != nil {
				return nil, err
			}
			res.Values[0] = yy
		}
	default:
		panic("type")
	}
	if tag != "" {
		op := eval.Lookup(tag)
		if op == nil {
			return nil, fmt.Errorf("no evalop for tag %q", tag)
		}
		if debug.Op() {
			debug.Logf("looked up %s\n", op)
		}
		runRes, err := t.run(res, parent)
		if err != nil {
			return nil, err
		}
		opInst, err := op.Instance(runRes, args)
		if err != nil {
			return nil, err
		}
		res, err = opInst.Eval(runRes, t.Env, evalFunc(t.Env, tag))
		if err != nil {
			return nil, err
		}
	}
	return res, nil
}

func evalFunc(_ eval.Env, _ string) eval.EvalFunc {
	return func(doc *ir.Node, env eval.Env) (*ir.Node, error) {
		t := &Tool{
			Env: env,
		}
		return t.Run(doc)
	}
}
