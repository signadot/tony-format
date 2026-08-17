package schema

import "testing"

// !all constrains the elements, and its payload was built at the container's own
// position: `!and [!irtype [], !all.irtype 1]` -- a list of numbers -- read as
// impossible, while `!all .[number]` was accepted.  The scalar reading is a real
// constraint and stays: !all over a scalar matches the scalar itself.
func TestAllConstrainsElementsNotTheContainer(t *testing.T) {
	for _, src := range []string{
		"define:\n  nums: !and\n  - !irtype []\n  - !all.irtype 1\naccept: .[nums]\n",
		"define:\n  strs: !and\n  - !irtype []\n  - !all.irtype \"\"\naccept: .[strs]\n",
		"define:\n  nums: !and\n  - !irtype []\n  - !all .[number]\naccept: .[nums]\n",
		"define:\n  objs: !and\n  - !irtype {}\n  - !all.irtype 1\naccept: .[objs]\n",
		// a contradictory element type is satisfiable by the empty container
		"define:\n  never: !and\n  - !irtype []\n  - !all.and [.[string], .[number]]\naccept: .[never]\n",
	} {
		if err := parseWithBase(t, src); err != nil {
			t.Errorf("%s\n  rejected: %s", src, err)
		}
	}
	// a scalar's every element is the scalar, so this asks a number to be a string
	if err := parseWithBase(t, "define:\n  n: !and\n  - !irtype 1\n  - !all.irtype \"\"\naccept: .[n]\n"); err == nil {
		t.Error("a number whose elements must be strings was accepted")
	}
}
