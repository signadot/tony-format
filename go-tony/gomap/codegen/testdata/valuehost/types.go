package valuehost

import "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/valueleaf"

//tony:schemagen=valuehost-host,notag
type Host struct {
	// struct VALUE, and the only reference to the package
	Val valueleaf.Leaf `tony:"field=val"`
}
