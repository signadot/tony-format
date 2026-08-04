package ir

import (
	"testing"

	"github.com/signadot/tony-format/go-tony/ir/kpath"
)

// KPathQuoteField is built on token.NeedsQuote, so relaxing which digit-leading scalars
// need quoting changes how paths are written too: "100m" is now a bare literal, and a
// field named that is written .100m rather than ."100m". This checks the path still parses
// back to the same field.
//
// Fields that would be ambiguous in a path are covered by KPathQuoteField's own '.', '['
// and '{' test rather than by NeedsQuote — "1.2.3" is a bare literal to the tokenizer but
// still quoted here, because the dots are path syntax.
func TestKPathDigitLeadingFields(t *testing.T) {
	for _, field := range []string{
		"100m", "30s", "1Gi", "1h30m", "0m", "007m",
		"1.2.3", "192.168.1.1", // quoted for the dots, not for the digits
		"0x1f", "007", "1_000", "100", "1.5", // still quoted: not bare literals
	} {
		node := FromMap(map[string]*Node{field: FromString("value")}).Values[0]
		path := node.KPath()
		parsed, err := kpath.Parse(path)
		if err != nil {
			t.Errorf("field %q: KPath() = %q does not parse: %v", field, path, err)
			continue
		}
		if parsed.Field == nil {
			t.Errorf("field %q: KPath() = %q parsed with no field", field, path)
			continue
		}
		if *parsed.Field != field {
			t.Errorf("field %q: KPath() = %q parsed back as %q", field, path, *parsed.Field)
		}
	}
}
