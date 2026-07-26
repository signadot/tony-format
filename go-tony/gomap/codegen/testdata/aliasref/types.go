package aliasref

import "github.com/signadot/tony-format/go-tony/gomap/codegen/testdata/aliastarget"

//tony:schemagen=aliasref-host,notag
type Host struct {
	Default *aliastarget.Format `tony:"field=default,omitzero"`
}
