package namedstrpkg

import "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/namedstr"

//tony:schemagen=namedstrpkg-holder,notag
type Holder struct {
	Op namedstr.Op `tony:"field=op"`
}
