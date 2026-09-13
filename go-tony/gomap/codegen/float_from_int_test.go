package codegen

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap"
	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/floats"
	"github.com/signadot/tony-format/go-tony/parse"
)

// An integer literal is a number, and a float field takes it: the reflection
// decoder did and the generated one refused it with "expected number, got
// Number", so the two codecs disagreed on one document (p478tacqh12krg32msn0
// item 24).
func TestGeneratedFloatFieldTakesAnInteger(t *testing.T) {
	node, err := parse.Parse([]byte("{f: 1, xs: [2, 2.5], m: {k: 3}}"))
	if err != nil {
		t.Fatal(err)
	}
	var generated floats.Host
	if err := generated.FromTonyIR(node); err != nil {
		t.Fatalf("generated: %v", err)
	}
	var reflected struct {
		F  float64            `tony:"field=f"`
		Xs []float64          `tony:"field=xs"`
		M  map[string]float64 `tony:"field=m"`
	}
	if err := gomap.FromTonyIR(node, &reflected); err != nil {
		t.Fatalf("reflection: %v", err)
	}
	if generated.F != 1 || generated.Xs[0] != 2 || generated.Xs[1] != 2.5 || generated.M["k"] != 3 {
		t.Errorf("generated read %+v", generated)
	}
	if reflected.F != generated.F || reflected.Xs[1] != generated.Xs[1] || reflected.M["k"] != generated.M["k"] {
		t.Errorf("the codecs disagree: reflection %+v, generated %+v", reflected, generated)
	}
}
