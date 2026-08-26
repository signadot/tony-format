package mergeop

import (
	"errors"
	"fmt"
	"strings"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/eval"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/libdiff"
)

var letSym = &letSymbol{name: letName}

func Let() Symbol {
	return letSym
}

const (
	letName name = "let"
)

type letSymbol struct {
	name
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
	var order []string
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
			if _, dup := bindings[f.String]; !dup {
				order = append(order, f.String)
			}
			bindings[f.String] = item.Values[j]
		}
	}
	if err := checkBindings(order, bindings); err != nil {
		return nil, err
	}

	op := &letOp{
		op:       op{name: s.name, child: child},
		bindings: bindings,
		in:       inNode,
	}
	return op, nil
}

type letOp struct {
	op
	bindings map[string]*ir.Node
	in       *ir.Node
}

func (l letOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("let op match on %s\n", doc.Path())
	}
	expandedIn, err := l.expandBody()
	if err != nil {
		return false, err
	}
	return f(doc, expandedIn, ctx)
}

// Patch applies the body with the names bound, which is the same act as matching
// with them: a binding says what a name stands for, and it stands for it wherever
// the body is read from. Refusing to patch made a name unusable in exactly the
// place a name is worth most -- writing the same value into several fields, or
// naming the value an !if installs -- and the body had to be written out with the
// value copied into each spot instead (nxybjwvch12ksbj8hxn0).
func (l letOp) Patch(doc *ir.Node, ctx *OpContext, _ MatchFunc, pf PatchFunc, _ libdiff.DiffFunc) (*ir.Node, error) {
	if debug.Op() {
		debug.Logf("let op patch on %s\n", doc.Path())
	}
	expandedIn, err := l.expandBody()
	if err != nil {
		return nil, err
	}
	return pf(doc, expandedIn, ctx)
}

// expandBody answers the body with the names this let binds substituted through
// it, and refuses one it cannot bind.
//
// The names left over are nobody's: an enclosing let has already run, and an
// inner one binds only inside its own body, which this skips. Saying so is the
// point -- an unbound .[name] used to expand to NULL, and a null pattern matches
// anything, so a misspelt name matched every document there is rather than
// failing.
func (l letOp) expandBody() (*ir.Node, error) {
	// Substitute the names THIS let binds, and leave the rest for an inner one.
	expandedIn, err := l.expandScoped(l.in, l.buildEnv())
	if err != nil {
		return nil, fmt.Errorf("error expanding let in body: %w", err)
	}
	if unbound := unresolvedRefs(expandedIn); len(unbound) > 0 {
		return nil, fmt.Errorf("let does not bind %s", strings.Join(unbound, ", "))
	}
	return expandedIn, nil
}

// checkBindings refuses a binding list which cannot be evaluated: a name nobody
// binds, and a cycle among the names it does.
//
// Both used to get through. eval.ExpandIR resolves a name it does not know to
// NULL, so `let: [{v: .[nope]}]` bound v to null and said nothing -- the same
// silent expansion the BODY has refused since it learned to, and worse here, since
// a null pattern merely matched everything while a null patch is WRITTEN. A cycle
// was not silent, it was fatal: `let: [{a: .[a]}]` expanded itself until the stack
// ran out and took the process with it.
//
// This is where !at says a pattern which cannot be read belongs -- at the place it
// is built, rather than reported as a mismatch at every node a match visits.
//
// A binding may name another of its own let's bindings, and order does not come
// into it: `[{b: .[a]}, {a: 1}]` reads as the reverse does, which is why the cycle
// is looked for in a graph rather than prevented by a left fold. A name an
// ENCLOSING let binds is already substituted by the time this let is instantiated,
// so a name still standing here is this let's to bind or nobody's.
func checkBindings(order []string, bindings map[string]*ir.Node) error {
	deps := make(map[string][]string, len(bindings))
	for _, nm := range order {
		for _, ref := range refNames(bindings[nm]) {
			if _, bound := bindings[ref]; !bound {
				return fmt.Errorf("let does not bind .[%s], named by binding %q", ref, nm)
			}
			deps[nm] = append(deps[nm], ref)
		}
	}

	const (
		open = 1 // on the path being walked: meeting it again is the cycle
		done = 2
	)
	state := make(map[string]int, len(order))
	var path []string
	var walk func(string) error
	walk = func(nm string) error {
		switch state[nm] {
		case done:
			return nil
		case open:
			at := 0
			for ; at < len(path); at++ {
				if path[at] == nm {
					break
				}
			}
			return fmt.Errorf("let bindings form a cycle: %s",
				strings.Join(append(append([]string{}, path[at:]...), nm), " -> "))
		}
		state[nm] = open
		path = append(path, nm)
		for _, dep := range deps[nm] {
			if err := walk(dep); err != nil {
				return err
			}
		}
		path = path[:len(path)-1]
		state[nm] = done
		return nil
	}
	for _, nm := range order {
		if err := walk(nm); err != nil {
			return err
		}
	}
	return nil
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

// unresolvedRefs is refNames spelled the way a pattern writes them, for an error.
func unresolvedRefs(n *ir.Node) []string {
	names := refNames(n)
	res := make([]string, len(names))
	for i, nm := range names {
		res[i] = ".[" + nm + "]"
	}
	return res
}

// refNames answers the names referred to in n which nothing in n binds, in the
// order they are met and without repeats.
//
// A nested let's BODY is skipped: those names are its to bind, and it will say so
// itself when it runs. Its bindings are not skipped -- they were evaluated in this
// scope, so an unknown name there is unbound here -- EXCEPT for the names that let
// binds itself, which one of its own bindings may name exactly as a top-level
// binding may. Without that exception the shape was refused where the identical
// one at top level was accepted: `!let {let: [{o: 1}], in: !let {let: [{a: 5}, {b:
// .[a]}], in: ...}}` answered "let does not bind .[a]" about a let that binds it.
func refNames(n *ir.Node) []string {
	seen := map[string]bool{}
	var res []string
	var walk func(*ir.Node, map[string]bool)
	walk = func(n *ir.Node, bound map[string]bool) {
		if n == nil {
			return
		}
		if name := refName(n); name != "" {
			if !bound[name] && !seen[name] {
				seen[name] = true
				res = append(res, name)
			}
			return
		}
		u := ir.Uncomment(n)
		if u == nil {
			return
		}
		// A let anywhere in this walk is a NESTED one -- the walk is over a body or a
		// binding, never over the let it belongs to -- so its bindings are ours to have
		// resolved and its body is its own. The body of an outer let is very often a
		// let itself, which is the shape being made to work here.
		if ir.TagHas(u.Tag, "!"+string(letName)) && u.Type == ir.ObjectType {
			walk(ir.Get(u, string(letName)), alsoBound(bound, letBoundNames(u)))
			return
		}
		for _, v := range u.Values {
			walk(v, bound)
		}
	}
	walk(n, nil)
	return res
}

// alsoBound answers the names of both, without writing to either.
func alsoBound(bound, more map[string]bool) map[string]bool {
	if len(more) == 0 {
		return bound
	}
	res := make(map[string]bool, len(bound)+len(more))
	for nm := range bound {
		res[nm] = true
	}
	for nm := range more {
		res[nm] = true
	}
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
