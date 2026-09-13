package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

// A text that holds no value -- empty, or comments only -- is null as a value, and a
// path the document does not have is null through getpath. Both came back as a nil
// node and crashed the walk that installs the value (4ynqp7wqh12krg32msn0 item 21).
func TestNothingEvaluatesToNull(t *testing.T) {
	op, err := ToValue().Instance(ir.FromString(""), nil)
	if err != nil {
		t.Fatalf("Instance: %v", err)
	}
	for _, text := range []string{"", "# only a comment\n", "\n\n"} {
		got, err := op.Eval(ir.FromString(text), Env{}, nil)
		if err != nil {
			t.Fatalf("tovalue %q: %v", text, err)
		}
		if got == nil || got.Type != ir.NullType {
			t.Errorf("tovalue %q gave %v, want null", text, got)
		}
	}

	node, err := parse.Parse([]byte(`a: '.[getpath("$.absent")]'` + "\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := ExpandIR(node, map[string]any{})
	if err != nil {
		t.Fatalf("ExpandIR: %v", err)
	}
	if got, _ := out.GetPath("$.a"); got == nil || got.Type != ir.NullType {
		t.Errorf("getpath of an absent path gave %v, want null", got)
	}
}
