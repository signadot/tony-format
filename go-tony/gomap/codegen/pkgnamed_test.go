package codegen

import (
	"os"
	"strings"
	"testing"

	"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/pkgnamed/host"
	lib "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/pkgnamed/lib-go"
)

// A package whose declared name is not the last element of its import path --
// lib in lib-go, as gopkg.in/yaml.v3 or a /v2 path -- is referred to by its
// name: the parser resolved `lib.Thing` against an import map keyed by the
// path's base and found nothing, and the generator wrote `&lib-go.Thing{}`
// (p478tacqh12krg32msn0 item 25). The generated import carries the name, and the
// fixture building is most of the test.
func TestPackageNamedUnlikeItsPath(t *testing.T) {
	src, err := os.ReadFile("testdata/pkgnamed/host/host_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "\tlib \"github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/pkgnamed/lib-go\"") {
		t.Errorf("the generated import does not name the package: %s", src)
	}
	in := &host.Host{T: lib.Thing{V: 1}, P: &lib.Thing{V: 2}}
	node, err := in.ToTonyIR()
	if err != nil {
		t.Fatalf("ToTonyIR: %v", err)
	}
	var out host.Host
	if err := out.FromTonyIR(node); err != nil {
		t.Fatalf("FromTonyIR: %v", err)
	}
	if out.T.V != 1 || out.P == nil || out.P.V != 2 {
		t.Errorf("round trip: %+v", out)
	}
}
