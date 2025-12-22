package schema

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/encode"
	"github.com/signadot/tony-format/go-tony/ir"
	"github.com/signadot/tony-format/go-tony/parse"
)

func TestParseDefSignature(t *testing.T) {
	tests := []struct {
		defName  string
		wantName string
		wantArgs []string
	}{
		{"array", "array", nil},
		{"array(t)", "array", []string{"t"}},
		{"nullable(t)", "nullable", []string{"t"}},
		{"map(k,v)", "map", []string{"k", "v"}},
		{"key(p)", "key", []string{"p"}},
	}

	for _, tt := range tests {
		name, args := ParseDefSignature(tt.defName)
		if name != tt.wantName {
			t.Errorf("ParseDefSignature(%q) name = %q, want %q", tt.defName, name, tt.wantName)
		}
		if len(args) != len(tt.wantArgs) {
			t.Errorf("ParseDefSignature(%q) args = %v, want %v", tt.defName, args, tt.wantArgs)
			continue
		}
		for i := range args {
			if args[i] != tt.wantArgs[i] {
				t.Errorf("ParseDefSignature(%q) args[%d] = %q, want %q", tt.defName, i, args[i], tt.wantArgs[i])
			}
		}
	}
}

func TestInstantiateDef(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		params []string
		args   []string
		want   string
	}{
		{
			name:   "simple tag substitution",
			body:   "!t null",
			params: []string{"t"},
			args:   []string{"int"},
			want:   "!int null",
		},
		{
			name:   "chained tag substitution",
			body:   "!all.t null",
			params: []string{"t"},
			args:   []string{"int"},
			want:   "!all.int null",
		},
		{
			name:   "nested arg substitution",
			body:   "!array(t)",
			params: []string{"t"},
			args:   []string{"int"},
			want:   "!array(int)",
		},
		{
			name:   "deeply nested substitution",
			body:   "!array(array(t))",
			params: []string{"t"},
			args:   []string{"int"},
			want:   "!array(array(int))",
		},
		{
			name:   "multiple params",
			body:   "!map(k,v)",
			params: []string{"k", "v"},
			args:   []string{"string", "int"},
			want:   "!map(string,int)",
		},
		{
			name:   "string value substitution",
			body:   "!hasPath p",
			params: []string{"p"},
			args:   []string{"name"},
			want:   "!hasPath name",
		},
		{
			name: "array body with tag substitution",
			body: `!and
- .[array]
- !all.t null`,
			params: []string{"t"},
			args:   []string{"int"},
			want: `!and
- .[array]
- !all.int null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := parse.Parse([]byte(tt.body))
			if err != nil {
				t.Fatalf("parse body: %v", err)
			}

			args := make([]*ir.Node, len(tt.args))
			for i, arg := range tt.args {
				args[i] = ir.FromString(arg)
			}

			result, err := InstantiateDef(body, tt.params, args)
			if err != nil {
				t.Fatalf("InstantiateDef: %v", err)
			}

			got := encode.MustString(result)
			want, err := parse.Parse([]byte(tt.want))
			if err != nil {
				t.Fatalf("parse want: %v", err)
			}
			wantStr := encode.MustString(want)

			if got != wantStr {
				t.Errorf("InstantiateDef:\ngot:\n%s\nwant:\n%s", got, wantStr)
			}
		})
	}
}

func TestInstantiateDef_NoParams(t *testing.T) {
	body, _ := parse.Parse([]byte("!foo bar"))
	result, err := InstantiateDef(body, nil, nil)
	if err != nil {
		t.Fatalf("InstantiateDef: %v", err)
	}

	// Result should be a clone
	if result == body {
		t.Error("result should be a clone, not the same pointer")
	}
	if !result.DeepEqual(body) {
		t.Error("result should be equal to body")
	}
}

func TestInstantiateDef_ParamCountMismatch(t *testing.T) {
	body, _ := parse.Parse([]byte("!t null"))
	_, err := InstantiateDef(body, []string{"t"}, nil)
	if err == nil {
		t.Error("expected error for param count mismatch")
	}
}

func TestInstantiateDef_DefRefScoping(t *testing.T) {
	// Test that .[...] expressions are NOT substituted (scoping rule).
	// With .[def](.[arg]) syntax, params only appear in tags, not inside .[...].
	tests := []struct {
		name   string
		body   string
		params []string
		args   []string
		want   string
	}{
		{
			name:   "def ref not substituted",
			body:   ".[t]",
			params: []string{"t"},
			args:   []string{"int"},
			want:   ".[t]", // NOT .[int]
		},
		{
			name:   "def ref with args not substituted",
			body:   ".[array(t)]",
			params: []string{"t"},
			args:   []string{"int"},
			want:   ".[array(t)]", // NOT .[array(int)]
		},
		{
			name:   "tag substituted but def ref preserved",
			body:   "!t .[t]",
			params: []string{"t"},
			args:   []string{"int"},
			want:   "!int .[t]", // tag changed, def ref unchanged
		},
		{
			name: "mixed: tags substituted, def refs preserved",
			body: `!and
- .[array]
- !all.t null`,
			params: []string{"t"},
			args:   []string{"int"},
			want: `!and
- .[array]
- !all.int null`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := parse.Parse([]byte(tt.body))
			if err != nil {
				t.Fatalf("parse body: %v", err)
			}

			args := make([]*ir.Node, len(tt.args))
			for i, arg := range tt.args {
				args[i] = ir.FromString(arg)
			}

			result, err := InstantiateDef(body, tt.params, args)
			if err != nil {
				t.Fatalf("InstantiateDef: %v", err)
			}

			got := encode.MustString(result)
			want, err := parse.Parse([]byte(tt.want))
			if err != nil {
				t.Fatalf("parse want: %v", err)
			}
			wantStr := encode.MustString(want)

			if got != wantStr {
				t.Errorf("InstantiateDef:\ngot:\n%s\nwant:\n%s", got, wantStr)
			}
		})
	}
}

func TestIsDefRef(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{".[array]", true},
		{".[array(int)]", true},
		{".[nullable(t)]", true},
		{"array", false},
		{"t", false},
		{"!array", false},
		{".array", false},
		{"[array]", false},
	}

	for _, tt := range tests {
		got := isDefRef(tt.s)
		if got != tt.want {
			t.Errorf("isDefRef(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}
