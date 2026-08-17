package mergeop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
)

var letSym = &letSymbol{matchName: letName}

func Let() Symbol {
	return letSym
}

const (
	letName matchName = "let"
)

type letSymbol struct {
	matchName
}

func (s letSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("let op has no args, got %v", args)
	}
	if child.Type != ir.ObjectType {
		return nil, errors.New("let must be an object")
	}

	// Extract let: array
	letNode := ir.Get(child, "let")
	if letNode == nil {
		return nil, errors.New("let must have 'let' field")
	}
	if letNode.Type != ir.ArrayType {
		return nil, errors.New("let field must be an array")
	}

	// Extract in: node
	inNode := ir.Get(child, "in")
	if inNode == nil {
		return nil, errors.New("let must have 'in' field")
	}

	// Parse bindings from let array.
	//
	// Every field of every item binds. Reading Fields[0] alone took the first
	// binding of a multi-field item and dropped the rest without saying so, and
	// read off the end of an item with no fields at all -- `let: [{}]` was a panic
	// out of a match.
	bindings := make(map[string]*ir.Node)
	for i, bindingItem := range letNode.Values {
		item := ir.Uncomment(bindingItem)
		if item == nil || item.Type != ir.ObjectType {
			return nil, fmt.Errorf("let binding %d is %s, and a binding is an object naming what it binds",
				i, bindingItemType(item))
		}
		if len(item.Fields) == 0 {
			return nil, fmt.Errorf("let binding %d binds nothing", i)
		}
		for j, f := range item.Fields {
			if j >= len(item.Values) {
				return nil, fmt.Errorf("let binding %d is malformed: %d names and %d values",
					i, len(item.Fields), len(item.Values))
			}
			bindings[f.String] = item.Values[j]
		}
	}

	op := &letOp{
		matchOp:  matchOp{op: op{name: s.matchName, child: child}},
		bindings: bindings,
		in:       inNode,
	}
	return op, nil
}

type letOp struct {
	matchOp
	bindings map[string]*ir.Node
	in       *ir.Node
}

func (l letOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("let op match on %s\n", doc.Path())
	}

	// Build environment from bindings
	env := l.buildEnv()

	// Substitute the names THIS let binds, and leave the rest for an inner one.
	expandedIn, err := l.expandScoped(l.in, env)
	if err != nil {
		return false, fmt.Errorf("error expanding let in body: %w", err)
	}

	// What is left over is a name nobody can bind: an enclosing let has already
	// run, and an inner one binds only inside its own body, which this skips.
	// Saying so is the point -- an unbound .[name] used to expand to NULL, and a
	// null pattern matches anything, so a misspelt name matched every document
	// there is rather than failing.
	if unbound := unresolvedRefs(expandedIn); len(unbound) > 0 {
		return false, fmt.Errorf("let does not bind %s", strings.Join(unbound, ", "))
	}

	// Match using the expanded 'in' node
	return f(doc, expandedIn, ctx)
}

// expandScoped substitutes the names in env and leaves every other reference
// exactly as it stands.
//
// Leaving them is what makes a nested let work. eval.ExpandIR resolves an unknown
// name to null, so an outer let reached into an inner one's body and blanked the
// references it meant to bind, before the inner op had run at all -- and since a
// null pattern matches anything, the inner match then passed on every document.
// The inner let is expanded later, by its own op, and the names it binds have to
// still be there when it runs.
//
// A nested let's BINDINGS are expanded here, and that is not an oversight: a
// binding list is evaluated in the enclosing scope, which is what makes
// `let: [{inner: .[outer]}]` mean what it reads as. Only the body is the inner
// scope's.
//
// Containers are rebuilt the way ExpandIR rebuilds them -- same keys, same tag --
// so the only difference between this and ExpandIR is what happens to a name
// neither of them knows.
func (l letOp) expandScoped(n *ir.Node, env map[string]any) (*ir.Node, error) {
	if n == nil {
		return nil, nil
	}
	if name := refName(n); name != "" {
		if _, bound := env[name]; !bound {
			return n.Clone(), nil // an inner let's to bind, or nobody's
		}
		return eval.ExpandIR(n.Clone(), env)
	}
	// A nested let: its bindings are evaluated here, in the enclosing scope, and its
	// body is evaluated with the names it rebinds taken OUT of scope. Without that
	// an inner binding could not shadow an outer one -- the outer substituted first
	// and the inner never saw its own name.
	if ir.TagHas(n.Tag, "!"+string(letName)) && n.Type == ir.ObjectType {
		inner := env
		if shadowed := letBoundNames(n); len(shadowed) > 0 {
			inner = make(map[string]any, len(env))
			for k, v := range env {
				if !shadowed[k] {
					inner[k] = v
				}
			}
		}
		kvs := make([]ir.KeyVal, len(n.Values))
		for i, elt := range n.Values {
			use := env
			if n.Fields[i].String == "in" {
				use = inner
			}
			xc, err := l.expandScoped(elt, use)
			if err != nil {
				return nil, err
			}
			kvs[i] = ir.KeyVal{Key: n.Fields[i], Val: xc}
		}
		return ir.FromKeyVals(kvs).WithTag(n.Tag), nil
	}

	switch n.Type {
	case ir.ObjectType:
		kvs := make([]ir.KeyVal, len(n.Values))
		for i, elt := range n.Values {
			xc, err := l.expandScoped(elt, env)
			if err != nil {
				return nil, err
			}
			f := n.Fields[i]
			// Preserve merge keys (null-typed keys) by using the original field node
			if f.Type == ir.NullType {
				kvs[i] = ir.KeyVal{Key: nil, Val: xc}
			} else {
				kvs[i] = ir.KeyVal{Key: f, Val: xc}
			}
		}
		return ir.FromKeyVals(kvs).WithTag(n.Tag), nil
	case ir.ArrayType:
		res := make([]*ir.Node, len(n.Values))
		for i, elt := range n.Values {
			xc, err := l.expandScoped(elt, env)
			if err != nil {
				return nil, err
			}
			res[i] = xc
		}
		return ir.FromSlice(res).WithTag(n.Tag), nil
	default:
		return eval.ExpandIR(n.Clone(), env)
	}
}

// refName is the name a whole-node .[name] reference names, or "" for anything
// else.
func refName(n *ir.Node) string {
	n = ir.Uncomment(n)
	if n == nil || n.Type != ir.StringType {
		return ""
	}
	return eval.GetRaw(n.String)
}

// unresolvedRefs answers the references left in n which no let will bind, in the
// order they are met and without repeats.
//
// A nested let's BODY is skipped: those names are its to bind, and it will say so
// itself when it runs. Its bindings are not skipped -- they were evaluated in this
// scope, so an unknown name there is unbound here.
func unresolvedRefs(n *ir.Node) []string {
	seen := map[string]bool{}
	var res []string
	var walk func(*ir.Node)
	walk = func(n *ir.Node) {
		if n == nil {
			return
		}
		if name := refName(n); name != "" {
			if !seen[name] {
				seen[name] = true
				res = append(res, ".["+name+"]")
			}
			return
		}
		u := ir.Uncomment(n)
		if u == nil {
			return
		}
		// A let anywhere in this body is a NESTED one -- this walk is over the body,
		// never over the let it belongs to -- so its bindings are ours to have
		// resolved and its body is its own. The body of an outer let is very often a
		// let itself, which is the shape being made to work here.
		if ir.TagHas(u.Tag, "!"+string(letName)) && u.Type == ir.ObjectType {
			walk(ir.Get(u, string(letName)))
			return
		}
		for _, v := range u.Values {
			walk(v)
		}
	}
	walk(n)
	return res
}

// buildEnv creates an eval.Env from the let bindings
func (l letOp) buildEnv() eval.Env {
	env := make(eval.Env)
	for varName, varValue := range l.bindings {
		env[varName] = varValue
	}
	return env
}

// bindingItemType names what a malformed binding is, for the error.
func bindingItemType(n *ir.Node) string {
	if n == nil {
		return "nothing"
	}
	return n.Type.String()
}

// letBoundNames answers the names a let node binds, read from its binding list
// without evaluating anything. They are what an inner let takes out of scope for
// its own body.
func letBoundNames(letNode *ir.Node) map[string]bool {
	bindings := ir.Get(letNode, string(letName))
	if bindings == nil || bindings.Type != ir.ArrayType {
		return nil
	}
	res := map[string]bool{}
	for _, item := range bindings.Values {
		item = ir.Uncomment(item)
		if item == nil || item.Type != ir.ObjectType {
			continue
		}
		for _, f := range item.Fields {
			res[f.String] = true
		}
	}
	return res
}
