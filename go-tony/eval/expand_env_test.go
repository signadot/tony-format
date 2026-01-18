package eval

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
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
		// Escape tests: \] -> ] and \\ -> \
		{
			in:  `$["a\]b"]`, // expression is "a]b"
			out: "a]b",
		},
		{
			in:  `$["a\\\\b"]`, // expression is "a\\b", expr gives a\b
			out: `a\b`,
		},
		{
			in:  `$["[\]"]`, // expression is "[]", evaluates to []
			out: "[]",
		},
	}
	f := EnvToMapAny(map[string]*ir.Node{
		"x":     ir.FromString("X"),
		"stuff": ir.FromString("STUFF"),
		"here":  ir.FromString("HERE"),
		"true":  ir.FromBool(false),
	})
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

func TestEnvEscapeLiterals(t *testing.T) {
	// When an expression is not properly closed (no unescaped ]), treat as literal
	f := EnvToMapAny(map[string]*ir.Node{})

	tests := []envTest{
		{
			in:  `$[x\]`, // escapes ], no closing bracket - literal
			out: `$[x\]`,
		},
		{
			in:  `$[x\y`, // no closing bracket - literal
			out: `$[x\y`,
		},
		{
			in:  `$["a\]`, // escapes ], no closing bracket - literal
			out: `$["a\]`,
		},
	}

	for _, tc := range tests {
		got, err := ExpandString(tc.in, f)
		if err != nil {
			t.Errorf("unexpected error for %q: %v", tc.in, err)
			continue
		}
		if got != tc.out {
			t.Errorf("for %q: got %q, want %q", tc.in, got, tc.out)
		}
	}
}

// TestExpandIRScriptFuncsInEval tests that script functions (getpath, whereami,
// listpath, getenv) are available in .[...] expressions.
func TestExpandIRScriptFuncsInEval(t *testing.T) {
	t.Run("getpath returns string value", func(t *testing.T) {
		input := `config:
  name: my-app
result: '.[getpath("$.config.name").String]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.result")
		if resultNode.Type != ir.StringType || resultNode.String != "my-app" {
			t.Errorf("got type=%s string=%q, want type=String string=my-app", resultNode.Type, resultNode.String)
		}
	})

	t.Run("getpath returns number node", func(t *testing.T) {
		input := `config:
  port: 8080
result: '.[getpath("$.config.port")]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.result")
		if resultNode.Type != ir.NumberType {
			t.Errorf("got type=%s, want Number", resultNode.Type)
		}
		if resultNode.Int64 == nil || *resultNode.Int64 != 8080 {
			t.Errorf("got Int64=%v, want 8080", resultNode.Int64)
		}
	})

	t.Run("getpath string concatenation with number", func(t *testing.T) {
		input := `config:
  host: localhost
  port: 3000
result: '.[getpath("$.config.host").String + ":" + string(getpath("$.config.port").Int64)]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.result")
		if resultNode.Type != ir.StringType || resultNode.String != "localhost:3000" {
			t.Errorf("got type=%s string=%q, want localhost:3000", resultNode.Type, resultNode.String)
		}
	})

	t.Run("getpath nested access", func(t *testing.T) {
		input := `config:
  database:
    host: db.example.com
result: '.[getpath("$.config.database.host").String]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.result")
		if resultNode.Type != ir.StringType || resultNode.String != "db.example.com" {
			t.Errorf("got type=%s string=%q, want db.example.com", resultNode.Type, resultNode.String)
		}
	})

	t.Run("getpath chained GetPath", func(t *testing.T) {
		input := `config:
  database:
    host: db.example.com
result: '.[getpath("$.config").GetPath("$.database").GetPath("$.host").String]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.result")
		if resultNode.Type != ir.StringType || resultNode.String != "db.example.com" {
			t.Errorf("got type=%s string=%q, want db.example.com", resultNode.Type, resultNode.String)
		}
	})

	t.Run("whereami returns current path", func(t *testing.T) {
		input := `foo:
  bar: '.[whereami()]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.foo.bar")
		if resultNode.Type != ir.StringType || resultNode.String != "$.foo.bar" {
			t.Errorf("got type=%s string=%q, want $.foo.bar", resultNode.Type, resultNode.String)
		}
	})

	t.Run("listpath with array", func(t *testing.T) {
		input := `items:
- first
- second
count: '.[len(listpath("$.items[*]"))]'
`
		node, err := parse.Parse([]byte(input))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		result, err := ExpandIR(node, make(map[string]any))
		if err != nil {
			t.Fatalf("ExpandIR error: %v", err)
		}

		resultNode, _ := result.GetPath("$.count")
		if resultNode.Type != ir.NumberType {
			t.Errorf("got type=%s, want Number", resultNode.Type)
		}
		if resultNode.Int64 == nil || *resultNode.Int64 != 2 {
			t.Errorf("got Int64=%v, want 2", resultNode.Int64)
		}
	})
}

// TestExpandEnvScriptFuncs tests that script functions work in ExpandEnv.
func TestExpandEnvScriptFuncs(t *testing.T) {
	input := `source:
  value: hello
target: '.[getpath("$.source.value").String]'
`
	node, err := parse.Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	env := make(map[string]any)
	err = ExpandEnv(node, env)
	if err != nil {
		t.Fatalf("ExpandEnv error: %v", err)
	}

	// Check that target now has the value "hello"
	target, err := node.GetPath("$.target")
	if err != nil {
		t.Fatalf("GetPath error: %v", err)
	}
	if target.String != "hello" {
		t.Errorf("got %q, want %q", target.String, "hello")
	}
}

// TestExpandIRCommentTypeEmptyValues tests that ExpandIR handles CommentType nodes
// with empty Values arrays without panicking (issue #105).
func TestExpandIRCommentTypeEmptyValues(t *testing.T) {
	// This YAML has a trailing comment before the closing bracket of the array.
	// When parsed with comments, this creates a CommentType node with empty Values
	// as an element of the array.
	input := `items:
- value1
# trailing comment
`

	node, err := parse.Parse([]byte(input), parse.ParseComments(true))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// This should not panic
	env := make(map[string]any)
	_, err = ExpandIR(node, env)
	if err != nil {
		t.Fatalf("ExpandIR error: %v", err)
	}
}
