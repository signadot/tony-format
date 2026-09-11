package schema

import (
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/mergeop"
)

// TestMatchContextNamesOperatorsThatExist: the match context declares what a
// pattern may contain, so every tag in it has to be a tag something implements.
//
// It declared "type", which nothing registers -- the operator is !irtype
// (mergeop/type.go names the Go symbol Type but registers "irtype") -- so the
// vocabulary allowed a tag that matches nothing and omitted the one that works.
// An unregistered tag is not an error anywhere: SplitChild folds it into the
// node's data, so a pattern using it constrains nothing and says so to no one.
func TestMatchContextNamesOperatorsThatExist(t *testing.T) {
	reg := NewContextRegistry()
	ctx, ok := reg.GetContext("tony-format/context/match")
	if !ok {
		t.Fatal("no match context registered")
	}

	for name := range ctx.Tags {
		if mergeop.Lookup(name) == nil {
			t.Errorf("match context declares %q, which no mergeop implements", name)
		}
	}

	if _, ok := ctx.Tags["irtype"]; !ok {
		t.Error("match context does not declare irtype, the operator that matches by node kind")
	}
	if _, ok := ctx.Tags["type"]; ok {
		t.Error("match context declares type, which is not an operator")
	}
}

// TestOpContextsAgreeWithRegistry: the match and patch contexts say which
// operators a pattern and a patch may hold, and mergeop's registry already
// knows -- each symbol says whether it IsMatch and IsPatch. The patch context
// named json-patch "jsonpatch", and the match context declared if, quote and
// unquote, which only patch (addsgv1yh12kszdxmdn0).
func TestOpContextsAgreeWithRegistry(t *testing.T) {
	const (
		matchURI = "tony-format/context/match"
		patchURI = "tony-format/context/patch"
	)
	reg := NewContextRegistry()
	matchCtx, ok := reg.GetContext(matchURI)
	if !ok {
		t.Fatal("no match context registered")
	}
	patchCtx, ok := reg.GetContext(patchURI)
	if !ok {
		t.Fatal("no patch context registered")
	}

	for _, sym := range mergeop.Symbols() {
		name := sym.String()
		var want []string
		if sym.IsMatch() {
			want = append(want, matchURI)
		}
		if sym.IsPatch() {
			want = append(want, patchURI)
		}
		for _, c := range []struct {
			ctx *Context
			in  bool
		}{{matchCtx, sym.IsMatch()}, {patchCtx, sym.IsPatch()}} {
			def, listed := c.ctx.Tags[name]
			if listed != c.in {
				t.Errorf("%s: listed in %s is %v, registry says %v", name, c.ctx.ShortName, listed, c.in)
				continue
			}
			if listed && strings.Join(def.Contexts, ",") != strings.Join(want, ",") {
				t.Errorf("%s in %s: contexts %v, want %v", name, c.ctx.ShortName, def.Contexts, want)
			}
		}
	}
	for _, ctx := range []*Context{matchCtx, patchCtx} {
		for name := range ctx.Tags {
			if mergeop.Lookup(name) == nil {
				t.Errorf("%s context declares %q, which no mergeop implements", ctx.ShortName, name)
			}
		}
	}
}
