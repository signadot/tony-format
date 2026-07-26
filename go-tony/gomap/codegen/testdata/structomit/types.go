package structomit

//tony:schemagen=structomit-inner,notag
type Inner struct {
	Kind string `tony:"field=kind"`
}

// Config has a value-struct field with omitzero (issue f69agjyeh12ks item 12).
//
//tony:schemagen=structomit-config,notag
type Config struct {
	Addr   string `tony:"field=addr"`
	Format Inner  `tony:"field=format,omitzero"`
}
