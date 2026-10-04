package mergeop

import (
	"fmt"
	"regexp"

	"github.com/signadot/tony-format/go-tony/debug"
	"github.com/signadot/tony-format/go-tony/ir"
)

var regexpSym = &regexpSymbol{matchName: regexpName}

// Regexp matches a string node against a regular expression in Go's syntax
// (RE2, so a match costs time linear in the input). Anchoring is the
// pattern's own, as with regexp.MatchString: !regexp "proceed" finds the word
// anywhere, !regexp "^proceed$" only the whole string. A glob cannot say
// where a word must stand -- its * spans any run of characters -- and a match
// over free text often has to.
func Regexp() Symbol {
	return regexpSym
}

const (
	regexpName matchName = "regexp"
)

type regexpSymbol struct {
	matchName
}

func (s regexpSymbol) Instance(child *ir.Node, args []string) (Op, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("regexp op has no args, got %v", args)
	}
	if child.Type != ir.StringType {
		return nil, fmt.Errorf("regexp op needs a string pattern, got %s", child.Type)
	}
	re, err := regexp.Compile(child.String)
	if err != nil {
		return nil, fmt.Errorf("regexp op: %w", err)
	}
	return &regexpOp{matchOp: matchOp{op: op{name: s.matchName, child: child}}, re: re}, nil
}

type regexpOp struct {
	matchOp
	re *regexp.Regexp
}

func (r regexpOp) Match(doc *ir.Node, ctx *OpContext, f MatchFunc) (bool, error) {
	if debug.Op() {
		debug.Logf("regexp op called on %s\n", doc.Path())
	}
	if doc.Type != ir.StringType {
		return false, nil
	}
	return r.re.MatchString(doc.String), nil
}
