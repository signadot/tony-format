package host

import "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/pkgnamed/lib-go"

//tony:schemagen=pkgnamed-host,notag
type Host struct {
	T lib.Thing  `tony:"field=t"`
	P *lib.Thing `tony:"field=p,omitzero"`
}
