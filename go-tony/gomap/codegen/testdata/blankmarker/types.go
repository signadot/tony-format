package blankmarker

// Config uses the blank-field schema marker form, which codegen does not read
// (issue f69agjyeh12ks item 13). It must be diagnosed, not silently ignored.
type Config struct {
	_    struct{} `tony:"schemagen=blankmarker-config,notag"`
	Addr string   `tony:"field=addr,omitzero"`
}
