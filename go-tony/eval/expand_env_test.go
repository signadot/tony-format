package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
)

type envTest struct {
	in, out string
}

func TestEnv(t *testing.T) {
	tests := []envTest{
		{
			in:  "abc",
			out: "abc",
		},
		{
			in:  "$[",
			out: "$[",
		},
		{
			in:  "$[x]",
			out: `X`,
		},
		{
			in:  " $[x]",
			out: ` X`,
		},
		{
			in:  ".[x]",
			out: `X`,
		},
		{
			in:  "$[x",
			out: "$[x",
		},
		{
			in:  "some $[stuff] $[here]",
			out: `some STUFF HERE`,
		},
		{
			in:  "some $[stuff] $[here] trailing",
			out: `some STUFF HERE trailing`,
		},
		{
			in:  "some $[ stuff ] $[here] trailing",
			out: `some STUFF HERE trailing`,
		},
		{
			in:  "$abc",
			out: "$abc",
		},
		{
			in:  " $abc",
			out: " $abc",
		},
	}
	f := map[string]*ir.Node{
		"x":     ir.FromString("X"),
		"stuff": ir.FromString("STUFF"),
		"here":  ir.FromString("HERE"),
		"true":  ir.FromBool(false),
	}
	for i := range tests {
		tc := &tests[i]
		got, err := ExpandString(tc.in, f)
		if err != nil {
			t.Error(err)
			continue
		}
		if got == tc.out {
			continue
		}
		t.Errorf("got %q want %q", got, tc.out)
	}
}
