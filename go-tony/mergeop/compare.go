package mergeop

import (
	"fmt"
	"math/big"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var (
	ltSym = &compareSymbol{matchName: ltName, holds: func(c int) bool { return c < 0 }}
	leSym = &compareSymbol{matchName: leName, holds: func(c int) bool { return c <= 0 }}
	gtSym = &compareSymbol{matchName: gtName, holds: func(c int) bool { return c > 0 }}
	geSym = &compareSymbol{matchName: geName, holds: func(c int) bool { return c >= 0 }}
)

// Lt, Le, Gt and Ge are the ordered comparisons of a number: the node the match
// meets is the left side, and the operand is the right.
//
//	used: !ge 400                          # a literal
//	used: !ge.get-path(root) limit         # another field in the document
//	used: !ge(0.8).get-path(root) limit    # that field, scaled: used >= 0.8 * limit
//
// Equality could already be asked, and !get-path made it a question about a
// relation; nothing ordered two numbers, so "within 80% of a limit" could not be
// written as a pattern at all, and had to be computed by whatever produced the
// document.
//
// The operand is a number literal, or a !get-path naming one node.  The tag
// argument is an optional scale on it, so the question is doc OP scale*operand.
// A charter cannot do arithmetic, and "within X% of" is the common question.
//
// A doc node which is not a number does not match: it is a document failing the
// pattern, the reading !irtype 0 gives it.  So !lt 400 does not match a string,
// and !not.ge 400 does.
//
// An operand which cannot be compared against is an ERROR, wherever that is
// found: a literal or scale which is not a number when the pattern is built, and
// a path which names nothing, or names something other than a number, when the
// match runs -- the rule !get-path keeps, and for its reason.  The comparison
// would otherwise answer false about a document for something wrong with the
// pattern, and say nothing about why.  A wild path is refused where it is built,
// by !get-path itself: a set has no order.
//
// Numbers compare by VALUE and exactly.  Each side is a big.Rat: an integer as
// itself, a float as the exact value it holds, and the scale from its decimal
// text, so 0.8 is 4/5 and 440 against 0.8 * 550 lands on the boundary rather than
// beside it.  3 < 3.5, and 3.0 <= 3 -- where equality, {n: 3}, is type-exact and
// does not match 3.0.  An order is about values; equality of data is about what
// was written.
func Lt() Symbol { return ltSym }
func Le() Symbol { return leSym }
func Gt() Symbol { return gtSym }
func Ge() Symbol { return geSym }

const (
	ltName matchName = "lt"
	leName matchName = "le"
	gtName matchName = "gt"
	geName matchName = "ge"
)

type compareSymbol struct {
	matchName
	// holds says whether the comparison holds, given the sign of doc - operand.
	holds func(int) bool
}

func (s compareSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	res := &compareOp{
		matchOp: matchOp{op: op{name: s.matchName, child: child}},
		holds:   s.holds,
	}
	switch len(args) {
	case 0:
	case 1:
		scale, ok := new(big.Rat).SetString(args[0])
		if !ok {
			return nil, fmt.Errorf("%s(%s): the scale is not a number", s, args[0])
		}
		res.scale = scale
	default:
		return nil, fmt.Errorf("%s op takes at most 1 arg (a scale), got %v", s, args)
	}

	child = ir.Uncomment(child)
	if child == nil {
		return nil, fmt.Errorf("%s op expects a number or a %s as its operand, got nothing", s, getPathName)
	}
	_, tag, opArgs, opChild, err := SplitChild(child)
	if err != nil {
		return nil, err
	}
	switch tag {
	case "":
		lit, ok := rat(child)
		if !ok {
			return nil, fmt.Errorf("%s op expects a number or a %s as its operand, got %s",
				s, getPathName, operandType(child))
		}
		res.literal = lit
	case string(getPathName):
		// The operand is a VALUE, so it is fetched here rather than handed to the
		// match function, which would read it as a pattern.
		get, err := GetPath().Instance(opChild, opArgs)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s, err)
		}
		res.get = get.(*getOp)
	default:
		return nil, fmt.Errorf("%s op expects a number or a %s as its operand, got !%s",
			s, getPathName, tag)
	}
	return res, nil
}

type compareOp struct {
	matchOp
	holds   func(int) bool
	scale   *big.Rat // nil: 1
	literal *big.Rat // the operand, when it is a literal
	get     *getOp   // the operand, when it is a path
}

func (c compareOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("%s op match on %s\n", c.name, doc.Path())
	}
	operand := c.literal
	if c.get != nil {
		val, err := c.get.fetch(doc)
		if err != nil {
			return false, fmt.Errorf("%s: %w", c.name, err)
		}
		r, ok := rat(ir.Uncomment(val))
		if !ok {
			return false, fmt.Errorf("%s: %s %s names %s, not a number",
				c.name, c.get.name, c.get.path, operandType(ir.Uncomment(val)))
		}
		operand = r
	}
	if c.scale != nil {
		operand = new(big.Rat).Mul(operand, c.scale)
	}
	left, ok := rat(doc)
	if !ok {
		return false, nil
	}
	return c.holds(left.Cmp(operand)), nil
}

// rat answers a number node's exact value.  A float which holds no value -- NaN
// or an infinity -- answers false, as a node which is not a number does.
func rat(n *ir.Node) (*big.Rat, bool) {
	if n == nil || n.Type != ir.NumberType {
		return nil, false
	}
	switch {
	case n.Int64 != nil:
		return new(big.Rat).SetInt64(*n.Int64), true
	case n.Float64 != nil:
		r := new(big.Rat).SetFloat64(*n.Float64)
		return r, r != nil
	}
	return new(big.Rat).SetString(n.Number)
}
