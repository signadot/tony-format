package schema

import (
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
